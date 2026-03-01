package player

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

type stubPCMDecoder struct {
	data       []byte
	pos        int64
	sampleRate int
	channels   int
}

func (d *stubPCMDecoder) Read(p []byte) (int, error) {
	if d.pos >= int64(len(d.data)) {
		return 0, io.EOF
	}
	n := copy(p, d.data[d.pos:])
	d.pos += int64(n)
	if d.pos >= int64(len(d.data)) {
		return n, io.EOF
	}
	return n, nil
}

func (d *stubPCMDecoder) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = d.pos + offset
	case io.SeekEnd:
		next = int64(len(d.data)) + offset
	}
	if next < 0 {
		next = 0
	}
	if next > int64(len(d.data)) {
		next = int64(len(d.data))
	}
	d.pos = next
	return next, nil
}

func (d *stubPCMDecoder) Length() int64     { return int64(len(d.data)) }
func (d *stubPCMDecoder) SampleRate() int   { return d.sampleRate }
func (d *stubPCMDecoder) ChannelCount() int { return d.channels }

func TestNormalizedDecoderUpmixesMono(t *testing.T) {
	src := &stubPCMDecoder{
		data:       pcm16(1000, -2000, 3000),
		sampleRate: playbackSampleRate,
		channels:   1,
	}

	dec, err := newNormalizedDecoder(src)
	if err != nil {
		t.Fatalf("newNormalizedDecoder() error = %v", err)
	}

	out, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	want := pcm16(1000, 1000, -2000, -2000, 3000, 3000)
	if !bytes.Equal(out, want) {
		t.Fatalf("upmixed PCM mismatch:\n got %v\nwant %v", out, want)
	}
	if dec.SampleRate() != playbackSampleRate {
		t.Fatalf("SampleRate() = %d, want %d", dec.SampleRate(), playbackSampleRate)
	}
	if dec.ChannelCount() != playbackChannels {
		t.Fatalf("ChannelCount() = %d, want %d", dec.ChannelCount(), playbackChannels)
	}
}

func TestNormalizedDecoderResamplesAndSeeks(t *testing.T) {
	src := &stubPCMDecoder{
		data:       pcm16(0, 1000, 10000, 11000, 20000, 21000),
		sampleRate: 24000,
		channels:   2,
	}

	dec, err := newNormalizedDecoder(src)
	if err != nil {
		t.Fatalf("newNormalizedDecoder() error = %v", err)
	}

	out, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	want := pcm16(
		0, 1000,
		5000, 6000,
		10000, 11000,
		15000, 16000,
		20000, 21000,
		20000, 21000,
	)
	if !bytes.Equal(out, want) {
		t.Fatalf("resampled PCM mismatch:\n got %v\nwant %v", out, want)
	}
	if got, wantLen := dec.Length(), int64(len(want)); got != wantLen {
		t.Fatalf("Length() = %d, want %d", got, wantLen)
	}

	if _, err := dec.Seek(8, io.SeekStart); err != nil {
		t.Fatalf("Seek() error = %v", err)
	}
	buf := make([]byte, 4)
	n, err := dec.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read() after seek error = %v", err)
	}
	if n != len(buf) {
		t.Fatalf("Read() after seek = %d bytes, want %d", n, len(buf))
	}
	if !bytes.Equal(buf, pcm16(10000, 11000)) {
		t.Fatalf("seeked PCM mismatch:\n got %v\nwant %v", buf, pcm16(10000, 11000))
	}
}

func pcm16(samples ...int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(sample))
	}
	return out
}
