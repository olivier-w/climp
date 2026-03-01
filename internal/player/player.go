package player

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
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
	file         *os.File
	decoder      audioDecoder
	counter      *countingReader
	sr           *speedReader
	otoCtx       *oto.Context
	otoPlayer    *oto.Player
	duration     time.Duration
	volume       float64
	paused       bool
	done         chan struct{}
	stopMon      chan struct{} // signals current monitor goroutine to exit
	mu           sync.Mutex
	closed       bool
	bytesPerSec  int // immutable after init — safe to read without mutex
	speed        SpeedMode
	sampleBuf    *visualizer.RingBuffer
	canSeek      bool
	titleUpdates <-chan string
	cleanup      func()
}

type liveTitleProvider interface {
	TitleUpdates() <-chan string
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
			if globalOtoCtx != nil {
				if ctxErr := globalOtoCtx.Err(); ctxErr != nil {
					otoInitErr = friendlyAudioInitError(ctxErr)
				} else {
					warmAudioOutput(globalOtoCtx, sampleRate, channelCount)
				}
			}
		} else {
			otoInitErr = friendlyAudioInitError(otoInitErr)
		}
	})
	return globalOtoCtx, otoInitErr
}

func warmAudioOutput(ctx *oto.Context, sampleRate, channelCount int) {
	if runtime.GOOS != "windows" || ctx == nil {
		return
	}

	const warmup = 500 * time.Millisecond
	byteCount := sampleRate * channelCount * 2 * int(warmup) / int(time.Second)
	if byteCount <= 0 {
		return
	}

	silence := bytes.NewReader(make([]byte, byteCount))
	player := ctx.NewPlayer(silence)
	player.SetVolume(0)
	player.Play()
	time.Sleep(warmup)
	_ = player.Close()
}

func friendlyAudioInitError(err error) error {
	if err == nil {
		return nil
	}
	if runtime.GOOS != "linux" {
		return err
	}

	msg := strings.ToLower(err.Error())
	isNoDevice := strings.Contains(msg, "alsa error at snd_pcm_open") ||
		strings.Contains(msg, "unknown pcm default") ||
		strings.Contains(msg, "cannot find card '0'")
	if !isNoDevice {
		return err
	}

	return fmt.Errorf("no Linux audio output device found (ALSA default device unavailable). This is common on headless VMs/containers; configure ALSA/PipeWire/PulseAudio or use a machine with audio")
}

func clampSeekByteOffset(target time.Duration, bytesPerSec int, totalBytes, frameSize int64) int64 {
	if bytesPerSec <= 0 {
		return 0
	}

	newPos := int64(target.Seconds() * float64(bytesPerSec))
	if newPos < 0 {
		newPos = 0
	}
	if totalBytes >= 0 && newPos > totalBytes {
		newPos = totalBytes
	}
	if frameSize > 0 {
		newPos -= newPos % frameSize
	}
	return newPos
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

	p, err := newFromDecoder(f, dec, true)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// NewStream creates a new Player for a live URL stream decoded by ffmpeg.
func NewStream(url string) (*Player, error) {
	dec, err := newStreamDecoder(url)
	if err != nil {
		return nil, err
	}
	return newFromDecoder(nil, dec, false)
}

func newFromDecoder(file *os.File, dec audioDecoder, canSeek bool) (*Player, error) {
	ctx, err := initOto(dec.SampleRate(), dec.ChannelCount())
	if err != nil {
		if file != nil {
			file.Close()
		}
		if c, ok := dec.(io.Closer); ok {
			c.Close()
		}
		return nil, err
	}

	bytesPerSec := dec.SampleRate() * dec.ChannelCount() * 2 // 16-bit = 2 bytes
	totalBytes := dec.Length()
	dur := time.Duration(0)
	if totalBytes > 0 {
		dur = time.Duration(float64(totalBytes) / float64(bytesPerSec) * float64(time.Second))
	}

	// ~90ms at 48kHz stereo 16-bit = 48000 * 2 * 2 * 0.09 ~= 17KB
	sampleBuf := visualizer.NewRingBuffer(16384)
	cr := &countingReader{reader: dec, sampleBuf: sampleBuf}
	frameSize := dec.ChannelCount() * 2
	sr := newSpeedReader(cr, frameSize)

	p := &Player{
		file:        file,
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
		canSeek:     canSeek,
	}
	if provider, ok := dec.(liveTitleProvider); ok {
		p.titleUpdates = provider.TitleUpdates()
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
		paused := p.paused
		canSeek := p.canSeek
		p.mu.Unlock()

		if paused {
			continue
		}

		if canSeek {
			total := p.decoder.Length()
			if total >= 0 && pos >= total {
				close(p.done)
				return
			}
			continue
		}

		// Non-seekable/live sources finish when Oto drains and pauses naturally.
		if !p.otoPlayer.IsPlaying() && p.otoPlayer.BufferedSize() == 0 {
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
	if !p.canSeek {
		return
	}

	// Stop the old monitor goroutine before replacing the done channel.
	close(p.stopMon)

	p.decoder.Seek(0, io.SeekStart)
	p.counter.SetPos(0)
	if p.sampleBuf != nil {
		p.sampleBuf.Clear()
	}

	p.disposeOtoPlayerLocked()
	if p.sr != nil {
		p.sr.clearBuf()
	}
	p.recreateOtoPlayerLocked(false)

	p.done = make(chan struct{})
	p.stopMon = make(chan struct{})
	p.resumeLocked()

	go p.monitor()
}

// TogglePause toggles between play and pause.
func (p *Player) TogglePause() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.paused {
		p.resumeLocked()
	} else {
		p.pauseLocked()
	}
}

// Pause pauses playback without toggling it back on.
func (p *Player) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pauseLocked()
}

// Paused returns whether playback is paused.
func (p *Player) Paused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paused
}

// Position returns the current playback position.
func (p *Player) Position() time.Duration {
	if p == nil || p.counter == nil || p.bytesPerSec <= 0 {
		return 0
	}
	pos := p.counter.Pos()
	secs := float64(pos) / float64(p.bytesPerSec)
	return time.Duration(secs * float64(time.Second))
}

// Duration returns the total duration of the track.
func (p *Player) Duration() time.Duration {
	return p.duration
}

// SeekTo moves playback to the given absolute target position.
func (p *Player) SeekTo(target time.Duration, resume bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p == nil || p.closed || !p.canSeek {
		return nil
	}
	if p.decoder == nil {
		p.paused = !resume
		return nil
	}

	frameSize := int64(p.decoder.ChannelCount()) * 2
	newPos := clampSeekByteOffset(target, p.bytesPerSec, p.decoder.Length(), frameSize)
	wasPaused := p.paused
	p.pauseLocked()

	if _, err := p.decoder.Seek(newPos, io.SeekStart); err != nil {
		if resume && !wasPaused {
			p.resumeLocked()
		} else {
			p.paused = wasPaused
		}
		return err
	}
	if p.counter != nil {
		p.counter.SetPos(newPos)
	}
	if p.sampleBuf != nil {
		p.sampleBuf.Clear()
	}
	if p.sr != nil {
		p.sr.clearBuf()
	}
	p.disposeOtoPlayerLocked()
	p.recreateOtoPlayerLocked(resume)
	return nil
}

// Seek moves playback by the given delta from current position.
func (p *Player) Seek(delta time.Duration) {
	target := p.Position() + delta
	if err := p.SeekTo(target, !p.Paused()); err != nil {
		return
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
	if p.otoPlayer != nil {
		p.otoPlayer.SetVolume(v)
	}
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
	if p.otoPlayer != nil {
		p.otoPlayer.SetVolume(v)
	}
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

// CanSeek reports whether this player supports seeking/restart semantics.
func (p *Player) CanSeek() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.canSeek
}

// TitleUpdates returns a stream of live title updates for stream-backed players.
func (p *Player) TitleUpdates() <-chan string {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.titleUpdates
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
	if p.stopMon != nil {
		close(p.stopMon)
	}
	p.disposeOtoPlayerLocked()
	if p.file != nil {
		p.file.Close()
	}
	if c, ok := p.decoder.(io.Closer); ok {
		c.Close()
	}
	if p.cleanup != nil {
		p.cleanup()
		p.cleanup = nil
	}
}

func (p *Player) pauseLocked() {
	if p == nil || p.closed {
		return
	}
	if p.otoPlayer != nil {
		p.otoPlayer.Pause()
	}
	p.paused = true
}

func (p *Player) resumeLocked() {
	if p == nil || p.closed {
		return
	}
	if p.otoPlayer != nil {
		p.otoPlayer.Play()
	}
	p.paused = false
}

func (p *Player) recreateOtoPlayerLocked(resume bool) {
	if p == nil || p.closed {
		return
	}
	if p.otoCtx == nil || p.sr == nil {
		p.paused = !resume
		return
	}
	p.otoPlayer = p.otoCtx.NewPlayer(p.sr)
	p.otoPlayer.SetVolume(p.volume)
	if resume {
		p.resumeLocked()
		return
	}
	p.paused = true
}

func (p *Player) disposeOtoPlayerLocked() {
	if p == nil || p.otoPlayer == nil {
		return
	}
	p.otoPlayer.Pause()
	_ = p.otoPlayer.Close()
	p.otoPlayer = nil
}
