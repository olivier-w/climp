package video

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

const defaultFPS = 15

// Session manages an ffmpeg subprocess that decodes video frames at a fixed FPS
// and renders them as terminal strings on demand.
type Session struct {
	path     string
	probe    Probe
	renderer *Renderer

	// Current decode geometry.
	scaleW int // ffmpeg output width in pixels
	scaleH int // ffmpeg output height in pixels
	outW   int // terminal cells width
	outH   int // terminal cells height

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdout io.ReadCloser
	cancel context.CancelFunc
	closed bool

	// Frame state.
	frameBuf  []byte        // reusable buffer for one raw frame (rgb24)
	frameSize int           // bytes per frame: scaleW * scaleH * 3
	frameIdx  int64         // index of the frame currently in frameBuf (-1 = none)
	startTime time.Duration // audio position corresponding to frame 0 of current decode session
}

// NewSession probes the media file and starts decoding from the given position.
// termW, termH: available terminal cells for the video pane.
func NewSession(path string, start time.Duration, termW, termH int) (*Session, error) {
	probe, err := ProbeMedia(path)
	if err != nil {
		return nil, err
	}
	if !probe.HasVideo {
		return nil, fmt.Errorf("no video stream in %s", path)
	}

	renderer := NewRenderer()
	color := renderer.mode != colorOff

	outW, outH, scaleW, scaleH := CalcFrameDimensions(termW, termH, probe.Width, probe.Height, color)

	s := &Session{
		path:      path,
		probe:     probe,
		renderer:  renderer,
		scaleW:    scaleW,
		scaleH:    scaleH,
		outW:      outW,
		outH:      outH,
		frameSize: scaleW * scaleH * 3,
		frameBuf:  make([]byte, scaleW*scaleH*3),
		frameIdx:  -1,
		startTime: start,
	}

	if err := s.startDecode(start); err != nil {
		return nil, err
	}

	return s, nil
}

// startDecode launches the ffmpeg video decode subprocess from the given position.
func (s *Session) startDecode(from time.Duration) error {
	s.stopDecode()

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found")
	}

	ctx, cancel := context.WithCancel(context.Background())

	args := []string{
		"-v", "quiet",
	}

	if from > 0 {
		args = append(args, "-ss", formatDuration(from))
	}

	args = append(args,
		"-i", s.path,
		"-f", "rawvideo",
		"-pix_fmt", "rgb24",
		"-vf", fmt.Sprintf("scale=%d:%d,fps=%d", s.scaleW, s.scaleH, defaultFPS),
		"-an", // no audio
		"pipe:1",
	)

	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	cmd.Stdin = nil

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("starting ffmpeg video decode: %w", err)
	}

	s.cmd = cmd
	s.stdout = stdout
	s.cancel = cancel
	s.startTime = from
	s.frameIdx = -1

	return nil
}

// stopDecode kills and cleans up the current ffmpeg process.
func (s *Session) stopDecode() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.cmd != nil {
		s.cmd.Wait()
		s.cmd = nil
	}
	s.stdout = nil
}

// readNextFrame reads one complete raw frame from the ffmpeg stdout pipe.
// Returns false if the stream ended.
func (s *Session) readNextFrame() bool {
	if s.stdout == nil {
		return false
	}
	_, err := io.ReadFull(s.stdout, s.frameBuf)
	if err != nil {
		return false
	}
	s.frameIdx++
	return true
}

// FrameFor returns a rendered terminal string for the frame corresponding to
// the given audio playback position. It advances the decode stream as needed
// and drops stale frames to keep up with the audio clock.
//
// Returns ("", false, nil) if no frame is available yet.
func (s *Session) FrameFor(audioPos time.Duration) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.stdout == nil {
		return "", false, nil
	}

	// Compute which frame index the audio clock says we should be at.
	elapsed := audioPos - s.startTime
	if elapsed < 0 {
		elapsed = 0
	}
	targetIdx := int64(elapsed.Seconds() * float64(defaultFPS))

	// Fast-forward: read and discard frames until we reach or pass the target.
	for s.frameIdx < targetIdx {
		if !s.readNextFrame() {
			// Stream ended or error — return last frame if we have one.
			if s.frameIdx >= 0 {
				return s.renderer.Render(s.frameBuf, s.scaleW, s.scaleH, s.outW, s.outH), true, nil
			}
			return "", false, nil
		}
	}

	// We have a frame at or past the target.
	if s.frameIdx >= 0 {
		return s.renderer.Render(s.frameBuf, s.scaleW, s.scaleH, s.outW, s.outH), true, nil
	}

	return "", false, nil
}

// Seek restarts the decode session at the given audio position.
func (s *Session) Seek(pos time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("session closed")
	}

	return s.startDecode(pos)
}

// Resize recalculates output dimensions and restarts decoding at the given position.
func (s *Session) Resize(termW, termH int, audioPos time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("session closed")
	}

	color := s.renderer.mode != colorOff
	outW, outH, scaleW, scaleH := CalcFrameDimensions(termW, termH, s.probe.Width, s.probe.Height, color)

	s.outW = outW
	s.outH = outH
	s.scaleW = scaleW
	s.scaleH = scaleH
	s.frameSize = scaleW * scaleH * 3

	// Reallocate frame buffer if size changed.
	if len(s.frameBuf) != s.frameSize {
		s.frameBuf = make([]byte, s.frameSize)
	}

	return s.startDecode(audioPos)
}

// HasVideo returns whether the media file has a video stream.
func (s *Session) HasVideo() bool {
	return s.probe.HasVideo
}

// Close releases all resources.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	s.stopDecode()
	return nil
}

// Path returns the media file path for this session.
func (s *Session) Path() string {
	return s.path
}

// formatDuration formats a time.Duration for ffmpeg -ss (HH:MM:SS.mmm).
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSec := d.Seconds()
	h := int(totalSec) / 3600
	m := (int(totalSec) % 3600) / 60
	sec := totalSec - float64(h*3600+m*60)
	return fmt.Sprintf("%02d:%02d:%06.3f", h, m, sec)
}

// FPS returns the decode frame rate.
func FPS() int {
	return defaultFPS
}

// TickInterval returns the recommended tick interval for video frame updates.
func TickInterval() time.Duration {
	return time.Second / time.Duration(defaultFPS)
}

// HasVideoStream is a quick check that doesn't require a full session.
func HasVideoStream(path string) bool {
	probe, err := ProbeMedia(path)
	if err != nil {
		return false
	}
	return probe.HasVideo
}

// probeFFmpeg returns whether ffmpeg is available.
func probeFFmpeg() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// Available returns whether video playback is possible (ffmpeg present).
func Available() bool {
	return probeFFmpeg()
}

// FrameInterval returns the tick interval as a string for display.
func FrameInterval() string {
	return strconv.Itoa(1000/defaultFPS) + "ms"
}
