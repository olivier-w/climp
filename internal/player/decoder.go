package player

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-audio/wav"
	"github.com/hajimehoshi/go-mp3"
	"github.com/jfreymuth/oggvorbis"
	"github.com/mewkiz/flac"
)

// audioDecoder is implemented by all format-specific decoders.
type audioDecoder interface {
	io.ReadSeeker
	Length() int64
	SampleRate() int
	ChannelCount() int
}

// newDecoder detects format by file extension and returns the appropriate decoder.
func newDecoder(f *os.File) (audioDecoder, error) {
	ext := strings.ToLower(filepath.Ext(f.Name()))
	switch ext {
	case ".mp3":
		return newMP3Decoder(f)
	case ".wav":
		return newWAVDecoder(f)
	case ".flac":
		return newFLACDecoder(f)
	case ".ogg":
		return newOGGDecoder(f)
	default:
		return nil, fmt.Errorf("unsupported format: %s", ext)
	}
}

// --- MP3 decoder ---

type mp3Decoder struct {
	dec *mp3.Decoder
}

func newMP3Decoder(f *os.File) (*mp3Decoder, error) {
	dec, err := mp3.NewDecoder(f)
	if err != nil {
		return nil, err
	}
	return &mp3Decoder{dec: dec}, nil
}

func (d *mp3Decoder) Read(p []byte) (int, error) { return d.dec.Read(p) }
func (d *mp3Decoder) Seek(offset int64, whence int) (int64, error) {
	return d.dec.Seek(offset, whence)
}
func (d *mp3Decoder) Length() int64    { return d.dec.Length() }
func (d *mp3Decoder) SampleRate() int  { return 44100 }
func (d *mp3Decoder) ChannelCount() int { return 2 }

// --- WAV decoder ---

type wavDecoder struct {
	file         *os.File
	buf          []byte
	pos          int64
	totalBytes   int64
	pcmStart     int64 // byte offset in file where PCM data begins
	sampleRate   int
	channels     int
	srcBitDepth  int
	srcFrameSize int64 // bytes per sample frame in source format
}

func newWAVDecoder(f *os.File) (*wavDecoder, error) {
	dec := wav.NewDecoder(f)
	if !dec.IsValidFile() {
		return nil, fmt.Errorf("invalid WAV file")
	}

	// FwdToPCM positions the reader at the start of PCM data
	if err := dec.FwdToPCM(); err != nil {
		return nil, fmt.Errorf("reading WAV PCM data: %w", err)
	}

	sampleRate := int(dec.SampleRate)
	channels := int(dec.NumChans)
	bitDepth := int(dec.BitDepth)
	srcFrameSize := int64(channels) * int64(bitDepth) / 8

	// Total output bytes (16-bit stereo)
	pcmSize := dec.PCMLen() // source PCM bytes
	totalSourceFrames := pcmSize / srcFrameSize
	totalBytes := totalSourceFrames * int64(channels) * 2 // 16-bit output

	// Record where PCM data starts in the file
	pcmStart, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, fmt.Errorf("getting PCM start position: %w", err)
	}

	return &wavDecoder{
		file:         f,
		sampleRate:   sampleRate,
		channels:     channels,
		srcBitDepth:  bitDepth,
		srcFrameSize: srcFrameSize,
		totalBytes:   totalBytes,
		pcmStart:     pcmStart,
	}, nil
}

func (d *wavDecoder) Read(p []byte) (int, error) {
	// Drain buffered data first
	if len(d.buf) > 0 {
		n := copy(p, d.buf)
		d.buf = d.buf[n:]
		d.pos += int64(n)
		return n, nil
	}

	srcBytesPerSample := d.srcBitDepth / 8
	// Read source samples: each output sample is 2 bytes (16-bit)
	numOutputSamples := len(p) / 2
	if numOutputSamples == 0 {
		numOutputSamples = 1
	}
	srcBytes := make([]byte, numOutputSamples*srcBytesPerSample)
	n, err := io.ReadFull(d.file, srcBytes)
	if n == 0 {
		if err != nil {
			return 0, err
		}
		return 0, io.EOF
	}

	// Truncate to whole samples
	samplesRead := n / srcBytesPerSample
	if samplesRead == 0 {
		return 0, io.EOF
	}

	// Convert to 16-bit LE PCM
	raw := make([]byte, samplesRead*2)
	for i := 0; i < samplesRead; i++ {
		var sample int
		off := i * srcBytesPerSample
		switch d.srcBitDepth {
		case 8:
			// 8-bit WAV is unsigned
			sample = (int(srcBytes[off]) - 128) << 8
		case 16:
			sample = int(int16(binary.LittleEndian.Uint16(srcBytes[off:])))
		case 24:
			s := int32(srcBytes[off]) | int32(srcBytes[off+1])<<8 | int32(srcBytes[off+2])<<16
			if s&0x800000 != 0 {
				s |= ^0xFFFFFF // sign extend
			}
			sample = int(s >> 8)
		case 32:
			sample = int(int32(binary.LittleEndian.Uint32(srcBytes[off:])) >> 16)
		}
		if sample > 32767 {
			sample = 32767
		} else if sample < -32768 {
			sample = -32768
		}
		binary.LittleEndian.PutUint16(raw[i*2:], uint16(int16(sample)))
	}

	written := copy(p, raw)
	if written < len(raw) {
		d.buf = raw[written:]
	}
	d.pos += int64(written)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return written, err
}

func (d *wavDecoder) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = d.pos + offset
	case io.SeekEnd:
		newPos = d.totalBytes + offset
	}
	if newPos < 0 {
		newPos = 0
	}
	if newPos > d.totalBytes {
		newPos = d.totalBytes
	}

	// Convert output byte position to source byte position
	outputFrameSize := int64(d.channels) * 2
	sampleFrame := newPos / outputFrameSize
	srcBytePos := sampleFrame * d.srcFrameSize

	if _, err := d.file.Seek(d.pcmStart+srcBytePos, io.SeekStart); err != nil {
		return d.pos, err
	}

	d.buf = nil
	d.pos = newPos
	return newPos, nil
}

func (d *wavDecoder) Length() int64     { return d.totalBytes }
func (d *wavDecoder) SampleRate() int   { return d.sampleRate }
func (d *wavDecoder) ChannelCount() int { return d.channels }

// --- FLAC decoder ---

type flacDecoder struct {
	stream     *flac.Stream
	buf        []byte
	pos        int64
	totalBytes int64
	sampleRate int
	channels   int
	bps        int
}

func newFLACDecoder(f *os.File) (*flacDecoder, error) {
	stream, err := flac.NewSeek(f)
	if err != nil {
		return nil, fmt.Errorf("decoding FLAC: %w", err)
	}

	info := stream.Info
	totalSamples := int64(info.NSamples)
	channels := int(info.NChannels)
	totalBytes := totalSamples * int64(channels) * 2 // 16-bit output

	return &flacDecoder{
		stream:     stream,
		sampleRate: int(info.SampleRate),
		channels:   channels,
		bps:        int(info.BitsPerSample),
		totalBytes: totalBytes,
	}, nil
}

func (d *flacDecoder) Read(p []byte) (int, error) {
	// Drain buffered data first
	if len(d.buf) > 0 {
		n := copy(p, d.buf)
		d.buf = d.buf[n:]
		d.pos += int64(n)
		return n, nil
	}

	frame, err := d.stream.ParseNext()
	if err != nil {
		return 0, err
	}

	nSamples := int(frame.Subframes[0].NSamples)
	raw := make([]byte, nSamples*d.channels*2)

	for i := 0; i < nSamples; i++ {
		for ch := 0; ch < d.channels; ch++ {
			sample := int(frame.Subframes[ch].Samples[i])
			switch {
			case d.bps > 16:
				sample >>= (d.bps - 16)
			case d.bps < 16:
				sample <<= (16 - d.bps)
			}
			if sample > 32767 {
				sample = 32767
			} else if sample < -32768 {
				sample = -32768
			}
			offset := (i*d.channels + ch) * 2
			binary.LittleEndian.PutUint16(raw[offset:], uint16(int16(sample)))
		}
	}

	written := copy(p, raw)
	if written < len(raw) {
		d.buf = raw[written:]
	}
	d.pos += int64(written)
	return written, nil
}

func (d *flacDecoder) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = d.pos + offset
	case io.SeekEnd:
		newPos = d.totalBytes + offset
	}
	if newPos < 0 {
		newPos = 0
	}
	if newPos > d.totalBytes {
		newPos = d.totalBytes
	}

	bytesPerFrame := int64(d.channels) * 2
	sampleNum := uint64(newPos / bytesPerFrame)

	if _, err := d.stream.Seek(sampleNum); err != nil {
		return d.pos, err
	}

	d.buf = nil
	d.pos = newPos
	return newPos, nil
}

func (d *flacDecoder) Length() int64     { return d.totalBytes }
func (d *flacDecoder) SampleRate() int   { return d.sampleRate }
func (d *flacDecoder) ChannelCount() int { return d.channels }

// --- OGG Vorbis decoder ---

type oggDecoder struct {
	reader     *oggvorbis.Reader
	buf        []byte
	pos        int64
	totalBytes int64
	sampleRate int
	channels   int
}

func newOGGDecoder(f *os.File) (*oggDecoder, error) {
	reader, err := oggvorbis.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("decoding OGG: %w", err)
	}

	channels := reader.Channels()
	totalSamples := reader.Length() // total samples per channel
	totalBytes := totalSamples * int64(channels) * 2

	return &oggDecoder{
		reader:     reader,
		sampleRate: reader.SampleRate(),
		channels:   channels,
		totalBytes: totalBytes,
	}, nil
}

func (d *oggDecoder) Read(p []byte) (int, error) {
	if len(d.buf) > 0 {
		n := copy(p, d.buf)
		d.buf = d.buf[n:]
		d.pos += int64(n)
		return n, nil
	}

	// Read float32 samples (interleaved)
	samples := make([]float32, len(p)/2)
	n, err := d.reader.Read(samples)
	if n == 0 {
		if err != nil {
			return 0, err
		}
		return 0, io.EOF
	}

	raw := make([]byte, n*2)
	for i := 0; i < n; i++ {
		s := samples[i]
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		binary.LittleEndian.PutUint16(raw[i*2:], uint16(int16(s*32767)))
	}

	written := copy(p, raw)
	if written < len(raw) {
		d.buf = raw[written:]
	}
	d.pos += int64(written)
	return written, err
}

func (d *oggDecoder) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = d.pos + offset
	case io.SeekEnd:
		newPos = d.totalBytes + offset
	}
	if newPos < 0 {
		newPos = 0
	}
	if newPos > d.totalBytes {
		newPos = d.totalBytes
	}

	bytesPerFrame := int64(d.channels) * 2
	samplePos := newPos / bytesPerFrame

	d.reader.SetPosition(samplePos)
	d.buf = nil
	d.pos = newPos
	return newPos, nil
}

func (d *oggDecoder) Length() int64     { return d.totalBytes }
func (d *oggDecoder) SampleRate() int   { return d.sampleRate }
func (d *oggDecoder) ChannelCount() int { return d.channels }
