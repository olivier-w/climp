package player

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ffmpegDecoder decodes audio from any container format via ffmpeg subprocess.
// It outputs signed 16-bit LE PCM at the source sample rate and channel count.
// Seek is implemented by restarting the ffmpeg process with -ss.
type ffmpegDecoder struct {
	path       string
	sampleRate int
	channels   int
	totalBytes int64
	duration   time.Duration

	mu       sync.Mutex
	cmd      *exec.Cmd
	stdout   io.ReadCloser
	cancel   context.CancelFunc
	pos      int64 // current byte position in PCM stream
	seekBase int64 // byte offset from last seek (for position tracking)
	closed   bool
}

// ffprobeResult holds parsed ffprobe JSON output.
type ffprobeResult struct {
	Streams []struct {
		CodecType  string `json:"codec_type"`
		SampleRate string `json:"sample_rate"`
		Channels   int    `json:"channels"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

var errFFmpegNotFound = fmt.Errorf("ffmpeg not found (required for video/container playback)")

// newFFmpegDecoder creates a decoder that extracts audio from any ffmpeg-supported format.
func newFFmpegDecoder(path string) (*ffmpegDecoder, error) {
	// Probe the file for audio stream info and duration.
	probe, err := probeAudio(path)
	if err != nil {
		return nil, fmt.Errorf("probing %s: %w", path, err)
	}

	sampleRate := probe.sampleRate
	channels := probe.channels
	bytesPerSec := sampleRate * channels * 2 // 16-bit = 2 bytes per sample
	totalBytes := int64(probe.duration.Seconds() * float64(bytesPerSec))

	d := &ffmpegDecoder{
		path:       path,
		sampleRate: sampleRate,
		channels:   channels,
		totalBytes: totalBytes,
		duration:   probe.duration,
	}

	if err := d.startProcess(0); err != nil {
		return nil, err
	}

	return d, nil
}

type audioProbe struct {
	sampleRate int
	channels   int
	duration   time.Duration
}

// probeAudio uses ffprobe to get audio stream metadata.
func probeAudio(path string) (*audioProbe, error) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not found (required for video/container playback)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		"-select_streams", "a:0",
		path,
	)
	cmd.Stdin = nil

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var result ffprobeResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("parsing ffprobe output: %w", err)
	}

	if len(result.Streams) == 0 {
		return nil, fmt.Errorf("no audio stream found")
	}

	stream := result.Streams[0]
	sr, err := strconv.Atoi(stream.SampleRate)
	if err != nil || sr <= 0 {
		sr = 44100 // fallback
	}

	channels := stream.Channels
	if channels <= 0 {
		channels = 2 // fallback
	}

	durStr := result.Format.Duration
	durSec, err := strconv.ParseFloat(durStr, 64)
	if err != nil || durSec <= 0 {
		// Try stream-level duration parsing as fallback
		durSec = 0
	}
	dur := time.Duration(durSec * float64(time.Second))

	return &audioProbe{
		sampleRate: sr,
		channels:   channels,
		duration:   dur,
	}, nil
}

// startProcess launches ffmpeg decoding from the given byte offset position.
func (d *ffmpegDecoder) startProcess(fromPos int64) error {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return errFFmpegNotFound
	}

	// Stop any running process.
	d.stopProcess()

	ctx, cancel := context.WithCancel(context.Background())

	args := []string{
		"-v", "quiet",
	}

	// If seeking to a non-zero position, use -ss before -i for fast seek.
	if fromPos > 0 {
		bytesPerSec := float64(d.sampleRate * d.channels * 2)
		seekSec := float64(fromPos) / bytesPerSec
		args = append(args, "-ss", formatSeekTime(seekSec))
	}

	args = append(args,
		"-i", d.path,
		"-f", "s16le", // signed 16-bit little-endian PCM
		"-acodec", "pcm_s16le", // explicit codec
		"-ar", strconv.Itoa(d.sampleRate),
		"-ac", strconv.Itoa(d.channels),
		"pipe:1", // output to stdout
	)

	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	cmd.Stdin = nil

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("setting up ffmpeg stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("starting ffmpeg: %w", err)
	}

	d.cmd = cmd
	d.stdout = stdout
	d.cancel = cancel
	d.pos = fromPos
	d.seekBase = fromPos

	return nil
}

// stopProcess kills and cleans up the current ffmpeg process.
func (d *ffmpegDecoder) stopProcess() {
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	if d.cmd != nil {
		// Wait for process cleanup (ignore errors from cancellation).
		d.cmd.Wait()
		d.cmd = nil
	}
	d.stdout = nil
}

func (d *ffmpegDecoder) Read(p []byte) (int, error) {
	d.mu.Lock()
	if d.closed || d.stdout == nil {
		d.mu.Unlock()
		return 0, io.EOF
	}
	stdout := d.stdout
	d.mu.Unlock()

	n, err := stdout.Read(p)

	d.mu.Lock()
	d.pos += int64(n)
	d.mu.Unlock()

	return n, err
}

func (d *ffmpegDecoder) Seek(offset int64, whence int) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = d.pos + offset
	case io.SeekEnd:
		newPos = d.totalBytes + offset
	}

	// Clamp to valid range.
	if newPos < 0 {
		newPos = 0
	}
	if newPos > d.totalBytes {
		newPos = d.totalBytes
	}

	// Align to frame boundary.
	frameSize := int64(d.channels) * 2
	newPos = newPos - (newPos % frameSize)

	if err := d.startProcess(newPos); err != nil {
		return d.pos, err
	}

	return newPos, nil
}

func (d *ffmpegDecoder) Length() int64     { return d.totalBytes }
func (d *ffmpegDecoder) SampleRate() int   { return d.sampleRate }
func (d *ffmpegDecoder) ChannelCount() int { return d.channels }

// Close releases all resources.
func (d *ffmpegDecoder) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	d.stopProcess()
}

// formatSeekTime formats seconds into HH:MM:SS.mmm for ffmpeg -ss.
func formatSeekTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	h := int(seconds) / 3600
	m := (int(seconds) % 3600) / 60
	s := seconds - float64(h*3600+m*60)
	return fmt.Sprintf("%02d:%02d:%06.3f", h, m, s)
}

// hasFFmpeg returns true if ffmpeg is available on PATH.
func hasFFmpeg() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// canDecodeNatively returns true if the extension has a native Go decoder.
func canDecodeNatively(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp3", ".wav", ".flac", ".ogg":
		return true
	}
	return false
}
