package player

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
)

const (
	streamSampleRate = 44100
	streamChannels   = 2
)

// streamDecoder adapts an ffmpeg live decode subprocess to the audioDecoder interface.
type streamDecoder struct {
	cmd       *exec.Cmd
	stdout    io.ReadCloser
	titleMeta *icyTitleWatcher
	titles    <-chan string
	waitDone  chan struct{}
	closeOnce sync.Once
}

func newStreamDecoder(url string) (*streamDecoder, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found (required for live stream playback)")
	}

	cmd := exec.Command(
		ffmpeg,
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-i", url,
		"-vn",
		"-ac", "2",
		"-ar", "44100",
		"-f", "s16le",
		"pipe:1",
	)
	cmd.Stdin = nil
	cmd.Stderr = io.Discard

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("setting up ffmpeg stream: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting ffmpeg stream: %w", err)
	}

	titleMeta, err := newICYTitleWatcher(url)
	if err != nil {
		titleMeta = nil
	}

	d := &streamDecoder{
		cmd:       cmd,
		stdout:    stdout,
		titleMeta: titleMeta,
		waitDone:  make(chan struct{}),
	}
	if titleMeta != nil {
		d.titles = titleMeta.Updates()
	}
	go func() {
		_ = cmd.Wait()
		close(d.waitDone)
	}()
	return d, nil
}

func (d *streamDecoder) Read(p []byte) (int, error) {
	return d.stdout.Read(p)
}

func (d *streamDecoder) Seek(int64, int) (int64, error) {
	return 0, fmt.Errorf("live stream is not seekable")
}

func (d *streamDecoder) TitleUpdates() <-chan string { return d.titles }
func (d *streamDecoder) Length() int64               { return -1 }
func (d *streamDecoder) SampleRate() int             { return streamSampleRate }
func (d *streamDecoder) ChannelCount() int           { return streamChannels }

func (d *streamDecoder) Close() error {
	d.closeOnce.Do(func() {
		if d.titleMeta != nil {
			_ = d.titleMeta.Close()
		}
		if d.stdout != nil {
			_ = d.stdout.Close()
		}
		if d.cmd != nil && d.cmd.Process != nil {
			_ = d.cmd.Process.Kill()
		}
		<-d.waitDone
	})
	return nil
}
