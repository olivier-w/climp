package player

import (
	"encoding/binary"
	"io"
	"os"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/olivier-w/climp/internal/visualizer"
)

// countingReader wraps an io.Reader and tracks bytes read.
// It also copies PCM data into a ring buffer for visualization.
// It has its own mutex (separate from Player's) because Oto's audio goroutine
// calls Read() concurrently with UI goroutine calls to Pos().
type countingReader struct {
	reader    io.ReadSeeker
	pos       int64
	mu        sync.Mutex
	sampleBuf *visualizer.RingBuffer
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.reader.Read(p)
	cr.mu.Lock()
	cr.pos += int64(n)
	cr.mu.Unlock()
	if n > 0 && cr.sampleBuf != nil {
		cr.sampleBuf.Write(p[:n])
	}
	return n, err
}

func (cr *countingReader) Pos() int64 {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	return cr.pos
}

func (cr *countingReader) SetPos(pos int64) {
	cr.mu.Lock()
	cr.pos = pos
	cr.mu.Unlock()
}

// Player manages audio playback.
type Player struct {
	file        *os.File
	decoder     audioDecoder
	counter     *countingReader
	sr          *speedReader
	otoCtx      *oto.Context
	otoPlayer   *oto.Player
	duration    time.Duration
	volume      float64
	paused      bool
	done        chan struct{}
	stopMon     chan struct{} // signals current monitor goroutine to exit
	mu          sync.Mutex
	closed      bool
	bytesPerSec int // immutable after init — safe to read without mutex
	speed       SpeedMode
	sampleBuf   *visualizer.RingBuffer
}

var (
	globalOtoCtx *oto.Context
	otoOnce      sync.Once
	otoInitErr   error
)

func initOto(sampleRate, channelCount int) (*oto.Context, error) {
	otoOnce.Do(func() {
		op := &oto.NewContextOptions{
			SampleRate:   sampleRate,
			ChannelCount: channelCount,
			Format:       oto.FormatSignedInt16LE,
		}
		var ready chan struct{}
		globalOtoCtx, ready, otoInitErr = oto.NewContext(op)
		if otoInitErr == nil {
			<-ready
		}
	})
	return globalOtoCtx, otoInitErr
}

// New creates a new Player for the given audio file path.
func New(path string) (*Player, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	dec, err := newDecoder(f)
	if err != nil {
		f.Close()
		return nil, err
	}

	ctx, err := initOto(dec.SampleRate(), dec.ChannelCount())
	if err != nil {
		f.Close()
		return nil, err
	}

	bytesPerSec := dec.SampleRate() * dec.ChannelCount() * 2 // 16-bit = 2 bytes
	totalBytes := dec.Length()
	dur := time.Duration(float64(totalBytes) / float64(bytesPerSec) * float64(time.Second))

	// ~90ms at 44100Hz stereo 16-bit = 44100 * 2 * 2 * 0.09 ≈ 16KB
	sampleBuf := visualizer.NewRingBuffer(16384)
	cr := &countingReader{reader: dec, sampleBuf: sampleBuf}
	frameSize := dec.ChannelCount() * 2
	sr := newSpeedReader(cr, frameSize)

	p := &Player{
		file:        f,
		decoder:     dec,
		counter:     cr,
		sr:          sr,
		otoCtx:      ctx,
		duration:    dur,
		volume:      0.8,
		done:        make(chan struct{}),
		stopMon:     make(chan struct{}),
		bytesPerSec: bytesPerSec,
		sampleBuf:   sampleBuf,
	}

	p.otoPlayer = ctx.NewPlayer(sr)
	p.otoPlayer.SetVolume(p.volume)
	p.otoPlayer.Play()

	// Monitor for playback end
	go p.monitor()

	return p, nil
}

func (p *Player) monitor() {
	// Poll until playback finishes, player is closed, or stopMon is signalled.
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopMon:
			return
		case <-ticker.C:
		}

		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}
		pos := p.counter.Pos()
		total := p.decoder.Length()
		paused := p.paused
		p.mu.Unlock()

		if !paused && pos >= total {
			close(p.done)
			return
		}
	}
}

// Done returns a channel that closes when playback finishes.
func (p *Player) Done() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done
}

// Restart seeks to the beginning and resumes playback.
// This resets the done channel so Done() can be used again.
func (p *Player) Restart() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop the old monitor goroutine before replacing the done channel.
	close(p.stopMon)

	p.decoder.Seek(0, io.SeekStart)
	p.counter.SetPos(0)
	if p.sampleBuf != nil {
		p.sampleBuf.Clear()
	}

	p.otoPlayer.Pause()
	p.sr.clearBuf()
	p.otoPlayer = p.otoCtx.NewPlayer(p.sr)
	p.otoPlayer.SetVolume(p.volume)

	p.done = make(chan struct{})
	p.stopMon = make(chan struct{})
	p.paused = false
	p.otoPlayer.Play()

	go p.monitor()
}

// TogglePause toggles between play and pause.
func (p *Player) TogglePause() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.paused {
		p.otoPlayer.Play()
		p.paused = false
	} else {
		p.otoPlayer.Pause()
		p.paused = true
	}
}

// Paused returns whether playback is paused.
func (p *Player) Paused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paused
}

// Position returns the current playback position.
func (p *Player) Position() time.Duration {
	pos := p.counter.Pos()
	secs := float64(pos) / float64(p.bytesPerSec)
	return time.Duration(secs * float64(time.Second))
}

// Duration returns the total duration of the track.
func (p *Player) Duration() time.Duration {
	return p.duration
}

// Seek moves playback by the given delta from current position.
func (p *Player) Seek(delta time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	currentPos := p.counter.Pos()
	deltaBytes := int64(delta.Seconds() * float64(p.bytesPerSec))
	newPos := currentPos + deltaBytes

	// Clamp to valid range
	if newPos < 0 {
		newPos = 0
	}
	totalBytes := p.decoder.Length()
	if newPos > totalBytes {
		newPos = totalBytes
	}

	// Align to frame boundary (channels * 2 bytes per sample)
	frameSize := int64(p.decoder.ChannelCount()) * 2
	newPos = newPos - (newPos % frameSize)

	// Pause Oto BEFORE seeking to stop concurrent reads on the decoder
	wasPaused := p.paused
	p.otoPlayer.Pause()

	// Seek the decoder (safe now that Oto is paused)
	if _, err := p.decoder.Seek(newPos, io.SeekStart); err != nil {
		// Resume if seek failed
		if !wasPaused {
			p.otoPlayer.Play()
		}
		return
	}
	p.counter.SetPos(newPos)
	if p.sampleBuf != nil {
		p.sampleBuf.Clear()
	}

	// Recreate the Oto player to flush buffers
	p.sr.clearBuf()
	p.otoPlayer = p.otoCtx.NewPlayer(p.sr)
	p.otoPlayer.SetVolume(p.volume)
	if !wasPaused {
		p.otoPlayer.Play()
	}
}

// Volume returns current volume (0.0 to 1.0).
func (p *Player) Volume() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

// SetVolume sets volume (clamped to 0.0 - 1.0).
func (p *Player) SetVolume(v float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	p.volume = v
	p.otoPlayer.SetVolume(v)
}

// AdjustVolume adjusts volume by delta.
func (p *Player) AdjustVolume(delta float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.volume + delta
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	p.volume = v
	p.otoPlayer.SetVolume(v)
}

// Speed returns the current playback speed.
func (p *Player) Speed() SpeedMode {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.speed
}

// SetSpeed sets the playback speed.
func (p *Player) SetSpeed(s SpeedMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.speed = s
	p.sr.setSpeed(s)
}

// CycleSpeed advances to the next speed mode and returns it.
func (p *Player) CycleSpeed() SpeedMode {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.speed = p.speed.Next()
	p.sr.setSpeed(p.speed)
	return p.speed
}

// Samples returns the most recent n int16 samples from the audio stream.
// Returns interleaved stereo samples (left, right, left, right, ...).
func (p *Player) Samples(n int) []int16 {
	p.mu.Lock()
	buf := p.sampleBuf
	p.mu.Unlock()
	if buf == nil {
		return nil
	}
	// Each int16 sample = 2 bytes
	raw := buf.Read(n * 2)
	if len(raw) < 2 {
		return nil
	}
	samples := make([]int16, len(raw)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(raw[i*2 : i*2+2]))
	}
	return samples
}

// Close releases all resources.
func (p *Player) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}
	p.closed = true
	close(p.stopMon)
	p.otoPlayer.Pause()
	p.file.Close()
}
