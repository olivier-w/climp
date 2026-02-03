package player

import (
	"io"
	"sync"
)

// SpeedMode represents the playback speed setting.
type SpeedMode int

const (
	Speed1x   SpeedMode = iota
	Speed2x
	SpeedHalf
)

// Next cycles to the next speed mode: 1x → 2x → 0.5x → 1x.
func (s SpeedMode) Next() SpeedMode {
	switch s {
	case Speed1x:
		return Speed2x
	case Speed2x:
		return SpeedHalf
	default:
		return Speed1x
	}
}

// Label returns a display label for the speed mode.
func (s SpeedMode) Label() string {
	switch s {
	case Speed2x:
		return "[2x]"
	case SpeedHalf:
		return "[0.5x]"
	default:
		return ""
	}
}

// speedReader sits between countingReader and Oto, dropping or duplicating
// frames to achieve playback speed changes. At 2x it drops every other frame;
// at 0.5x it duplicates each frame.
type speedReader struct {
	source    io.Reader
	frameSize int // channels * 2 (16-bit samples)
	speed     SpeedMode
	buf       []byte // leftover bytes from 0.5x duplication
	tmpBuf    []byte // reusable read buffer (grow-only)
	tmpExp    []byte // reusable expanded buffer for 0.5x (grow-only)
	mu        sync.Mutex
}

func newSpeedReader(source io.Reader, frameSize int) *speedReader {
	return &speedReader{
		source:    source,
		frameSize: frameSize,
		speed:     Speed1x,
	}
}

func (sr *speedReader) Read(p []byte) (int, error) {
	sr.mu.Lock()
	speed := sr.speed
	sr.mu.Unlock()

	switch speed {
	case Speed2x:
		return sr.read2x(p)
	case SpeedHalf:
		return sr.readHalf(p)
	default:
		return sr.source.Read(p)
	}
}

// read2x reads twice the requested data from source and keeps every other frame.
func (sr *speedReader) read2x(p []byte) (int, error) {
	fs := sr.frameSize
	// Align output request to frame boundary
	outFrames := len(p) / fs
	if outFrames == 0 {
		return sr.source.Read(p)
	}

	// We need 2x the frames from source
	srcSize := outFrames * 2 * fs
	if cap(sr.tmpBuf) < srcSize {
		sr.tmpBuf = make([]byte, srcSize)
	}
	tmp := sr.tmpBuf[:srcSize]
	n, err := io.ReadFull(sr.source, tmp)

	// Process whatever we got, aligned to frames
	srcFramesRead := n / fs
	outWritten := 0
	for i := 0; i < srcFramesRead && outWritten+fs <= len(p); i += 2 {
		copy(p[outWritten:outWritten+fs], tmp[i*fs:(i+1)*fs])
		outWritten += fs
	}

	if outWritten > 0 {
		return outWritten, nil
	}
	return 0, err
}

// readHalf reads half the requested data from source and duplicates each frame.
func (sr *speedReader) readHalf(p []byte) (int, error) {
	fs := sr.frameSize

	// First, drain any leftover buffer from a previous call
	if len(sr.buf) > 0 {
		n := copy(p, sr.buf)
		sr.buf = sr.buf[n:]
		return n, nil
	}

	// Align output request to frame boundary
	outFrames := len(p) / fs
	if outFrames == 0 {
		return sr.source.Read(p)
	}

	// Read half as many frames from source
	srcFrames := (outFrames + 1) / 2
	srcSize := srcFrames * fs
	if cap(sr.tmpBuf) < srcSize {
		sr.tmpBuf = make([]byte, srcSize)
	}
	tmp := sr.tmpBuf[:srcSize]
	n, err := io.ReadFull(sr.source, tmp)

	srcFramesRead := n / fs
	// Each source frame becomes 2 output frames
	totalOut := srcFramesRead * 2 * fs
	if cap(sr.tmpExp) < totalOut {
		sr.tmpExp = make([]byte, totalOut)
	}
	expanded := sr.tmpExp[:totalOut]
	for i := 0; i < srcFramesRead; i++ {
		frame := tmp[i*fs : (i+1)*fs]
		copy(expanded[i*2*fs:i*2*fs+fs], frame)
		copy(expanded[(i*2+1)*fs:(i*2+2)*fs], frame)
	}

	wrote := copy(p, expanded)
	if wrote < len(expanded) {
		// Must copy leftover since expanded references the reusable tmpExp buffer.
		sr.buf = append(sr.buf[:0], expanded[wrote:]...)
	}

	if wrote > 0 {
		return wrote, nil
	}
	return 0, err
}

func (sr *speedReader) setSpeed(s SpeedMode) {
	sr.mu.Lock()
	sr.speed = s
	sr.buf = nil
	sr.mu.Unlock()
}

func (sr *speedReader) clearBuf() {
	sr.mu.Lock()
	sr.buf = nil
	sr.mu.Unlock()
}
