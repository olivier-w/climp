package aacfile

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Info describes the decoded PCM stream exposed by Reader.
type Info struct {
	SampleRate   int
	ChannelCount int
	PCMBytes     int64
	Container    string
}

// Reader exposes a seekable PCM16LE stream decoded lazily from a local
// AAC-family input.
type Reader struct {
	source *containerSource

	decoder *synthDecoder
	trace   TraceSink

	pos    int64
	length int64
	nextAU int

	buf     []byte
	tmpAU   []byte
	tmpRaw  []byte
	discard int

	info Info
}

// Open indexes src and exposes a seekable native-rate PCM16LE stream. name is
// used only to select and validate the expected AAC-family container.
func Open(src io.ReaderAt, size int64, name string) (*Reader, error) {
	return openWithTrace(src, size, name, nil)
}

// OpenWithTrace indexes src and emits per-access-unit synthesis traces into
// sink while exposing the normal PCM16LE Reader interface.
func OpenWithTrace(src io.ReaderAt, size int64, name string, sink TraceSink) (*Reader, error) {
	return openWithTrace(src, size, name, sink)
}

func openWithTrace(src io.ReaderAt, size int64, name string, sink TraceSink) (*Reader, error) {
	if size < 0 {
		return nil, malformedf("invalid input size: %d", size)
	}

	container, err := sourceContainer(name)
	if err != nil {
		return nil, err
	}

	source, err := parseContainer(container, src, size)
	if err != nil {
		return nil, err
	}

	r := &Reader{
		source: source,
		trace:  sink,
		length: int64(source.totalPCM * source.cfg.channelConfig * 2),
		info: Info{
			SampleRate:   source.cfg.sampleRate,
			ChannelCount: source.cfg.channelConfig,
			PCMBytes:     int64(source.totalPCM * source.cfg.channelConfig * 2),
			Container:    container,
		},
	}
	if err := r.resetDecoder(); err != nil {
		return nil, err
	}
	r.discard = source.leading * source.cfg.channelConfig * 2
	return r, nil
}

// OpenFile opens a local AAC-family file and decodes it into a seekable PCM
// reader. The input file handle stays owned by the caller.
func OpenFile(f *os.File) (*Reader, error) {
	return OpenFileWithTrace(f, nil)
}

// OpenFileWithTrace opens a local AAC-family file and forwards synthesis traces
// into sink while exposing the normal PCM16LE Reader interface.
func OpenFileWithTrace(f *os.File, sink TraceSink) (*Reader, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat input: %w", err)
	}
	return openWithTrace(f, info.Size(), f.Name(), sink)
}

func sourceContainer(name string) (string, error) {
	switch ext := strings.ToLower(filepath.Ext(name)); ext {
	case ".aac", ".m4a", ".m4b":
		return ext, nil
	default:
		return "", unsupportedFeature("container", ext)
	}
}

func (r *Reader) Read(p []byte) (int, error) {
	if r == nil || r.pos >= r.length {
		return 0, io.EOF
	}

	total := 0
	remaining := r.length - r.pos

	if len(r.buf) > 0 {
		buf := r.buf
		if int64(len(buf)) > remaining {
			buf = buf[:int(remaining)]
		}
		n := copy(p, buf)
		r.buf = r.buf[n:]
		r.pos += int64(n)
		total += n
		p = p[n:]
		remaining -= int64(n)
		if len(p) == 0 {
			return total, nil
		}
	}

	for len(p) > 0 {
		raw, err := r.decodeAccessUnit()
		if err != nil {
			if total > 0 && err == io.EOF {
				return total, nil
			}
			if total > 0 {
				return total, nil
			}
			return total, err
		}

		remaining = r.length - r.pos
		if remaining <= 0 {
			if total > 0 {
				return total, nil
			}
			return 0, io.EOF
		}
		if int64(len(raw)) > remaining {
			raw = raw[:int(remaining)]
		}

		n := copy(p, raw)
		if n < len(raw) {
			r.buf = append(r.buf[:0], raw[n:]...)
		}
		r.pos += int64(n)
		total += n
		p = p[n:]
		if n < len(raw) {
			return total, nil
		}
	}

	return total, nil
}

func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	if r == nil {
		return 0, io.EOF
	}

	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = r.pos + offset
	case io.SeekEnd:
		next = r.length + offset
	default:
		return r.pos, malformedf("invalid seek whence: %d", whence)
	}

	if next < 0 {
		next = 0
	}
	if next > r.length {
		next = r.length
	}

	frameSize := int64(r.info.ChannelCount * 2)
	if frameSize > 0 {
		next -= next % frameSize
	} else {
		return r.pos, malformedf("invalid PCM frame size")
	}

	targetFrame := int(next / frameSize)
	rawFrameTarget := targetFrame + r.source.leading
	targetAU, discardFrames := r.source.locateRawFrame(rawFrameTarget)
	startAU := targetAU
	if rawFrameTarget < r.source.totalRaw && startAU > 0 {
		startAU--
		discardFrames += r.source.units[startAU].pcmFrames
	}

	if err := r.resetDecoder(); err != nil {
		return r.pos, err
	}
	r.buf = nil
	r.nextAU = startAU
	r.discard = discardFrames * r.info.ChannelCount * 2
	r.pos = next
	return next, nil
}

func (r *Reader) Info() Info {
	if r == nil {
		return Info{}
	}
	return r.info
}

func (r *Reader) Length() int64 {
	if r == nil {
		return 0
	}
	return r.length
}

func (r *Reader) SampleRate() int {
	if r == nil {
		return 0
	}
	return r.info.SampleRate
}

func (r *Reader) ChannelCount() int {
	if r == nil {
		return 0
	}
	return r.info.ChannelCount
}

func (r *Reader) Close() error {
	if r == nil {
		return nil
	}
	r.source = nil
	r.decoder = nil
	r.pos = 0
	r.length = 0
	r.nextAU = 0
	r.buf = nil
	r.tmpAU = nil
	r.tmpRaw = nil
	r.discard = 0
	r.trace = nil
	return nil
}

func (r *Reader) resetDecoder() error {
	if r.source == nil {
		return io.EOF
	}
	r.decoder = newSynthDecoder(r.source.cfg, r.trace)
	return nil
}

func (r *Reader) decodeAccessUnit() ([]byte, error) {
	for {
		if r.source == nil || r.nextAU >= len(r.source.units) {
			return nil, io.EOF
		}

		unitIndex := r.nextAU
		unit := r.source.units[unitIndex]
		r.nextAU++

		au, err := r.source.readAccessUnit(unitIndex, r.tmpAU)
		if err != nil {
			return nil, err
		}
		r.tmpAU = au

		samples, err := r.decoder.decodeAccessUnit(r.source, au, unitIndex, unit.rawStart, unit.pcmFrames)
		if err != nil {
			return nil, fmt.Errorf("decoding AAC access unit %d: %w", unitIndex, err)
		}

		expected := aacFrameSize * r.info.ChannelCount
		if len(samples) != expected {
			return nil, malformedf("unexpected decoded sample count: got %d want %d", len(samples), expected)
		}

		frameCount := unit.pcmFrames
		if frameCount < 0 || frameCount > aacFrameSize {
			return nil, malformedf("invalid PCM frame count: %d", frameCount)
		}

		sampleCount := frameCount * r.info.ChannelCount
		rawSize := sampleCount * 2
		if cap(r.tmpRaw) < rawSize {
			r.tmpRaw = make([]byte, rawSize)
		}
		raw := r.tmpRaw[:rawSize]
		for i := 0; i < sampleCount; i++ {
			binary.LittleEndian.PutUint16(raw[i*2:], uint16(floatToPCM16(samples[i])))
		}

		if r.discard > 0 {
			if r.discard >= len(raw) {
				r.discard -= len(raw)
				continue
			}
			raw = raw[r.discard:]
			r.discard = 0
		}
		if len(raw) == 0 {
			continue
		}
		return raw, nil
	}
}
