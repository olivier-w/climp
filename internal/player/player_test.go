package player

import (
	"io"
	"testing"
	"time"
)

type stubSeekDecoder struct {
	pos        int64
	length     int64
	sampleRate int
	channels   int
	seekErr    error
}

func (d *stubSeekDecoder) Read([]byte) (int, error) { return 0, io.EOF }

func (d *stubSeekDecoder) Seek(offset int64, whence int) (int64, error) {
	if d.seekErr != nil {
		return d.pos, d.seekErr
	}
	switch whence {
	case io.SeekStart:
		d.pos = offset
	case io.SeekCurrent:
		d.pos += offset
	case io.SeekEnd:
		d.pos = d.length + offset
	}
	return d.pos, nil
}

func (d *stubSeekDecoder) Length() int64     { return d.length }
func (d *stubSeekDecoder) SampleRate() int   { return d.sampleRate }
func (d *stubSeekDecoder) ChannelCount() int { return d.channels }

func TestClampSeekByteOffsetClampsAndAligns(t *testing.T) {
	got := clampSeekByteOffset(3900*time.Millisecond, 10, 10, 4)
	if got != 8 {
		t.Fatalf("expected clamped aligned seek offset 8, got %d", got)
	}

	got = clampSeekByteOffset(-1*time.Second, 10, 100, 4)
	if got != 0 {
		t.Fatalf("expected negative seek to clamp to 0, got %d", got)
	}
}

func TestPauseSetsPausedWithoutToggle(t *testing.T) {
	p := &Player{}
	p.Pause()
	if !p.paused {
		t.Fatal("expected pause to set paused state")
	}
}

func TestSeekToClampsAndAlignsToFrameBoundary(t *testing.T) {
	dec := &stubSeekDecoder{
		length:     41,
		sampleRate: 44100,
		channels:   2,
	}
	counter := &countingReader{}
	p := &Player{
		decoder:     dec,
		counter:     counter,
		bytesPerSec: 10,
		canSeek:     true,
	}

	if err := p.SeekTo(3900*time.Millisecond, false); err != nil {
		t.Fatalf("SeekTo returned error: %v", err)
	}
	if dec.pos != 36 {
		t.Fatalf("expected decoder seek position 36, got %d", dec.pos)
	}
	if got := counter.Pos(); got != 36 {
		t.Fatalf("expected counter position 36, got %d", got)
	}
	if !p.paused {
		t.Fatal("expected paused state after non-resuming seek")
	}
}

func TestPlayerCloseRunsCleanupOnce(t *testing.T) {
	calls := 0
	p := &Player{
		stopMon: make(chan struct{}),
		cleanup: func() {
			calls++
		},
	}

	p.Close()
	p.Close()

	if calls != 1 {
		t.Fatalf("expected cleanup to run once, got %d", calls)
	}
}
