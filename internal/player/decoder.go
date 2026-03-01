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

// baseDecoder holds shared state and helpers for WAV, FLAC, and OGG decoders.
// Embed in format-specific decoders to reuse buffer drain, seek, and accessor logic.
type baseDecoder struct {
	buf        []byte
	pos        int64
	totalBytes int64
	sampleRate int
	channels   int
}

func (b *baseDecoder) Length() int64     { return b.totalBytes }
func (b *baseDecoder) SampleRate() int   { return b.sampleRate }
func (b *baseDecoder) ChannelCount() int { return b.channels }

// drainBuf copies buffered leftover data into p. Returns bytes copied and
// whether there was buffered data to drain.
func (b *baseDecoder) drainBuf(p []byte) (int, bool) {
	if len(b.buf) == 0 {
		return 0, false
	}
	n := copy(p, b.buf)
	b.buf = b.buf[n:]
	b.pos += int64(n)
	return n, true
}

// calcSeekPos computes the clamped byte position for a Seek call.
func (b *baseDecoder) calcSeekPos(offset int64, whence int) int64 {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = b.pos + offset
	case io.SeekEnd:
		newPos = b.totalBytes + offset
	}
	if newPos < 0 {
		newPos = 0
	}
	if newPos > b.totalBytes {
		newPos = b.totalBytes
	}
	return newPos
}

// commitSeek updates state after a successful underlying seek.
func (b *baseDecoder) commitSeek(newPos int64) {
	b.buf = nil
	b.pos = newPos
}

// bufferOutput copies raw decoded bytes into p and stashes any remainder.
// Returns the number of bytes written to p.
func (b *baseDecoder) bufferOutput(p, raw []byte) int {
	written := copy(p, raw)
	if written < len(raw) {
		b.buf = raw[written:]
	}
	b.pos += int64(written)
	return written
}

// newDecoder detects format by file extension and returns the appropriate decoder.
func newDecoder(f *os.File) (audioDecoder, error) {
	dec, err := newNativeDecoder(f)
	if err != nil {
		return nil, err
	}

	norm, err := newNormalizedDecoder(dec)
	if err != nil {
		if c, ok := dec.(io.Closer); ok {
			_ = c.Close()
		}
		return nil, err
	}
	return norm, nil
}

// newNativeDecoder detects format by file extension and returns a decoder that
// emits 16-bit PCM in the source's native sample rate and channel layout.
func newNativeDecoder(f *os.File) (audioDecoder, error) {
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
	case ".aac", ".m4a", ".m4b":
		return newAACDecoder(f)
	default:
		return nil, fmt.Errorf("unsupported format: %s", ext)
	}
}

// --- MP3 decoder ---

type mp3Decoder struct {
	dec    *mp3.Decoder
	pos    int64
	length int64
	start  int64
}

func newMP3Decoder(f *os.File) (*mp3Decoder, error) {
	startTrim, endTrim, err := readMP3GaplessTrim(f)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	dec, err := mp3.NewDecoder(f)
	if err != nil {
		return nil, err
	}

	length := dec.Length()
	frameBytes := int64(4) // go-mp3 always outputs 16-bit stereo PCM.
	startBytes := startTrim * frameBytes
	endBytes := endTrim * frameBytes
	if length >= 0 {
		if startBytes > length {
			startBytes = length
		}
		if endBytes > length-startBytes {
			endBytes = length - startBytes
		}
		length -= startBytes + endBytes
	}

	d := &mp3Decoder{
		dec:    dec,
		length: length,
		start:  startBytes,
	}
	if startBytes > 0 {
		if _, err := dec.Seek(startBytes, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (d *mp3Decoder) Read(p []byte) (int, error) {
	if d.length >= 0 && d.pos >= d.length {
		return 0, io.EOF
	}
	if d.length >= 0 {
		remaining := d.length - d.pos
		if remaining <= 0 {
			return 0, io.EOF
		}
		if int64(len(p)) > remaining {
			p = p[:remaining]
		}
	}

	n, err := d.dec.Read(p)
	d.pos += int64(n)
	if d.length >= 0 && d.pos >= d.length {
		if n == 0 {
			return 0, io.EOF
		}
		return n, io.EOF
	}
	return n, err
}
func (d *mp3Decoder) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = d.pos + offset
	case io.SeekEnd:
		next = d.length + offset
	default:
		return d.pos, fmt.Errorf("invalid seek whence: %d", whence)
	}
	if next < 0 {
		next = 0
	}
	if d.length >= 0 && next > d.length {
		next = d.length
	}
	next -= next % 4

	if _, err := d.dec.Seek(d.start+next, io.SeekStart); err != nil {
		return d.pos, err
	}
	d.pos = next
	return next, nil
}
func (d *mp3Decoder) Length() int64    { return d.length }
func (d *mp3Decoder) SampleRate() int  { return d.dec.SampleRate() }
// ChannelCount returns 2 because go-mp3 always decodes to stereo output.
func (d *mp3Decoder) ChannelCount() int { return 2 }

// --- WAV decoder ---

type wavDecoder struct {
	baseDecoder
	file         *os.File
	pcmStart     int64 // byte offset in file where PCM data begins
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
		baseDecoder: baseDecoder{
			totalBytes: totalBytes,
			sampleRate: sampleRate,
			channels:   channels,
		},
		file:         f,
		srcBitDepth:  bitDepth,
		srcFrameSize: srcFrameSize,
		pcmStart:     pcmStart,
	}, nil
}

func (d *wavDecoder) Read(p []byte) (int, error) {
	if n, ok := d.drainBuf(p); ok {
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

	written := d.bufferOutput(p, raw)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return written, err
}

func (d *wavDecoder) Seek(offset int64, whence int) (int64, error) {
	newPos := d.calcSeekPos(offset, whence)

	// Convert output byte position to source byte position
	outputFrameSize := int64(d.channels) * 2
	sampleFrame := newPos / outputFrameSize
	srcBytePos := sampleFrame * d.srcFrameSize

	if _, err := d.file.Seek(d.pcmStart+srcBytePos, io.SeekStart); err != nil {
		return d.pos, err
	}

	d.commitSeek(newPos)
	return newPos, nil
}

// --- FLAC decoder ---

type flacDecoder struct {
	baseDecoder
	stream *flac.Stream
	bps    int
	tmpRaw []byte // reusable output buffer (grow-only)
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
		baseDecoder: baseDecoder{
			totalBytes: totalBytes,
			sampleRate: int(info.SampleRate),
			channels:   channels,
		},
		stream: stream,
		bps:    int(info.BitsPerSample),
	}, nil
}

func (d *flacDecoder) Read(p []byte) (int, error) {
	if n, ok := d.drainBuf(p); ok {
		return n, nil
	}

	frame, err := d.stream.ParseNext()
	if err != nil {
		return 0, err
	}

	nSamples := int(frame.Subframes[0].NSamples)
	rawSize := nSamples * d.channels * 2
	if cap(d.tmpRaw) < rawSize {
		d.tmpRaw = make([]byte, rawSize)
	}
	raw := d.tmpRaw[:rawSize]

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

	return d.bufferOutput(p, raw), nil
}

func (d *flacDecoder) Seek(offset int64, whence int) (int64, error) {
	newPos := d.calcSeekPos(offset, whence)

	bytesPerFrame := int64(d.channels) * 2
	sampleNum := uint64(newPos / bytesPerFrame)

	if _, err := d.stream.Seek(sampleNum); err != nil {
		return d.pos, err
	}

	d.commitSeek(newPos)
	return newPos, nil
}

// --- OGG Vorbis decoder ---

type oggDecoder struct {
	baseDecoder
	reader     *oggvorbis.Reader
	tmpSamples []float32 // reusable decode buffer (grow-only)
	tmpRaw     []byte    // reusable output buffer (grow-only)
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
		baseDecoder: baseDecoder{
			totalBytes: totalBytes,
			sampleRate: reader.SampleRate(),
			channels:   channels,
		},
		reader: reader,
	}, nil
}

func (d *oggDecoder) Read(p []byte) (int, error) {
	if n, ok := d.drainBuf(p); ok {
		return n, nil
	}

	// Read float32 samples (interleaved)
	sampleCount := len(p) / 2
	if cap(d.tmpSamples) < sampleCount {
		d.tmpSamples = make([]float32, sampleCount)
	}
	samples := d.tmpSamples[:sampleCount]
	n, err := d.reader.Read(samples)
	if n == 0 {
		if err != nil {
			return 0, err
		}
		return 0, io.EOF
	}

	rawSize := n * 2
	if cap(d.tmpRaw) < rawSize {
		d.tmpRaw = make([]byte, rawSize)
	}
	raw := d.tmpRaw[:rawSize]
	for i := 0; i < n; i++ {
		s := samples[i]
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		binary.LittleEndian.PutUint16(raw[i*2:], uint16(int16(s*32767)))
	}

	return d.bufferOutput(p, raw), err
}

func (d *oggDecoder) Seek(offset int64, whence int) (int64, error) {
	newPos := d.calcSeekPos(offset, whence)

	bytesPerFrame := int64(d.channels) * 2
	samplePos := newPos / bytesPerFrame

	d.reader.SetPosition(samplePos)
	d.commitSeek(newPos)
	return newPos, nil
}
