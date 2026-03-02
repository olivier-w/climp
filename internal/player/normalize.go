package player

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	playbackSampleRate     = 48000
	playbackChannels       = 2
	playbackBytesPerSample = 2
	playbackFrameSize      = playbackChannels * playbackBytesPerSample
)

// normalizedDecoder wraps a seekable PCM decoder and presents a fixed
// 48 kHz stereo s16le stream to the player.
type normalizedDecoder struct {
	src          audioDecoder
	passthrough  bool
	length       int64
	pos          int64
	srcRate      int
	srcChannels  int
	srcFrameSize int

	totalSrcFrames int64
	totalOutFrames int64
	outFramePos    int64
	srcPosNum      int64

	buf      []byte
	tmpOut   []byte
	tmpSrc   []byte
	srcFrames []int16

	srcBaseFrame int64
	lastFrame    [playbackChannels]int16
	haveLast     bool
}

func newNormalizedDecoder(src audioDecoder) (audioDecoder, error) {
	sampleRate := src.SampleRate()
	if sampleRate <= 0 {
		return nil, fmt.Errorf("unsupported sample rate: %d", sampleRate)
	}

	channels := src.ChannelCount()
	if channels < 1 || channels > playbackChannels {
		return nil, fmt.Errorf("unsupported channel count: %d", channels)
	}

	srcFrameSize := channels * playbackBytesPerSample
	totalSrcFrames := src.Length() / int64(srcFrameSize)
	totalOutFrames := totalSrcFrames * playbackSampleRate / int64(sampleRate)
	if totalSrcFrames > 0 && totalOutFrames == 0 {
		totalOutFrames = 1
	}

	d := &normalizedDecoder{
		src:            src,
		passthrough:    sampleRate == playbackSampleRate && channels == playbackChannels,
		length:         totalOutFrames * playbackFrameSize,
		srcRate:        sampleRate,
		srcChannels:    channels,
		srcFrameSize:   srcFrameSize,
		totalSrcFrames: totalSrcFrames,
		totalOutFrames: totalOutFrames,
	}
	if d.passthrough {
		d.length = src.Length()
		d.totalOutFrames = d.length / playbackFrameSize
	}
	return d, nil
}

func (d *normalizedDecoder) Length() int64     { return d.length }
func (d *normalizedDecoder) SampleRate() int   { return playbackSampleRate }
func (d *normalizedDecoder) ChannelCount() int { return playbackChannels }

func (d *normalizedDecoder) Read(p []byte) (int, error) {
	if d.passthrough {
		n, err := d.src.Read(p)
		d.pos += int64(n)
		return n, err
	}

	if len(d.buf) > 0 {
		n := copy(p, d.buf)
		d.buf = d.buf[n:]
		d.pos += int64(n)
		return n, nil
	}

	if d.outFramePos >= d.totalOutFrames {
		return 0, io.EOF
	}

	framesToGenerate := len(p) / playbackFrameSize
	if len(p)%playbackFrameSize != 0 {
		framesToGenerate++
	}
	if framesToGenerate == 0 {
		framesToGenerate = 1
	}

	remainingFrames := d.totalOutFrames - d.outFramePos
	if int64(framesToGenerate) > remainingFrames {
		framesToGenerate = int(remainingFrames)
	}

	raw, err := d.generateFrames(framesToGenerate)
	if len(raw) == 0 {
		if err == nil {
			err = io.EOF
		}
		return 0, err
	}

	n := copy(p, raw)
	if n < len(raw) {
		d.buf = append(d.buf[:0], raw[n:]...)
	}
	d.pos += int64(n)
	return n, nil
}

func (d *normalizedDecoder) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = d.pos + offset
	case io.SeekEnd:
		newPos = d.length + offset
	default:
		return d.pos, fmt.Errorf("invalid seek whence: %d", whence)
	}

	if newPos < 0 {
		newPos = 0
	}
	if newPos > d.length {
		newPos = d.length
	}
	newPos -= newPos % playbackFrameSize

	if d.passthrough {
		pos, err := d.src.Seek(newPos, io.SeekStart)
		if err != nil {
			return d.pos, err
		}
		d.buf = nil
		d.pos = pos
		return pos, nil
	}

	outFrame := newPos / playbackFrameSize
	srcFrame := outFrame * int64(d.srcRate) / playbackSampleRate
	srcBytePos := srcFrame * int64(d.srcFrameSize)
	if _, err := d.src.Seek(srcBytePos, io.SeekStart); err != nil {
		return d.pos, err
	}

	d.buf = nil
	d.pos = newPos
	d.outFramePos = outFrame
	d.srcPosNum = outFrame * int64(d.srcRate)
	d.srcFrames = d.srcFrames[:0]
	d.srcBaseFrame = srcFrame
	d.haveLast = false
	return newPos, nil
}

func (d *normalizedDecoder) generateFrames(frameCount int) ([]byte, error) {
	rawSize := frameCount * playbackFrameSize
	if cap(d.tmpOut) < rawSize {
		d.tmpOut = make([]byte, rawSize)
	}
	raw := d.tmpOut[:rawSize]

	writtenFrames := 0
	for writtenFrames < frameCount && d.outFramePos < d.totalOutFrames {
		srcFrame := d.srcPosNum / playbackSampleRate
		if srcFrame >= d.totalSrcFrames {
			break
		}

		if err := d.ensureFrameAvailable(srcFrame); err != nil {
			return raw[:writtenFrames*playbackFrameSize], err
		}

		left0, right0, err := d.frameAt(srcFrame)
		if err != nil {
			return raw[:writtenFrames*playbackFrameSize], err
		}
		left1, right1 := left0, right0
		if srcFrame+1 < d.totalSrcFrames {
			if err := d.ensureFrameAvailable(srcFrame + 1); err != nil {
				return raw[:writtenFrames*playbackFrameSize], err
			}
			left1, right1, err = d.frameAt(srcFrame + 1)
			if err != nil {
				return raw[:writtenFrames*playbackFrameSize], err
			}
		}

		fracNum := d.srcPosNum % playbackSampleRate
		outOffset := writtenFrames * playbackFrameSize
		binary.LittleEndian.PutUint16(raw[outOffset:], uint16(interpolateSample(left0, left1, fracNum)))
		binary.LittleEndian.PutUint16(raw[outOffset+2:], uint16(interpolateSample(right0, right1, fracNum)))

		writtenFrames++
		d.outFramePos++
		d.srcPosNum += int64(d.srcRate)
	}

	if writtenFrames == 0 {
		return nil, io.EOF
	}
	return raw[:writtenFrames*playbackFrameSize], nil
}

func (d *normalizedDecoder) ensureFrameAvailable(absFrame int64) error {
	if absFrame >= d.totalSrcFrames {
		return io.EOF
	}
	d.compactFrames(absFrame - 1)

	for absFrame >= d.srcBaseFrame+int64(len(d.srcFrames))/playbackChannels {
		if err := d.readMoreFrames(); err != nil {
			return err
		}
	}
	return nil
}

func (d *normalizedDecoder) compactFrames(minKeepFrame int64) {
	if minKeepFrame <= d.srcBaseFrame {
		return
	}
	availableFrames := int64(len(d.srcFrames)) / playbackChannels
	dropFrames := minKeepFrame - d.srcBaseFrame
	if dropFrames <= 0 {
		return
	}
	if dropFrames >= availableFrames {
		d.srcFrames = d.srcFrames[:0]
		d.srcBaseFrame += availableFrames
		return
	}

	dropSamples := int(dropFrames) * playbackChannels
	remaining := len(d.srcFrames) - dropSamples
	copy(d.srcFrames, d.srcFrames[dropSamples:])
	d.srcFrames = d.srcFrames[:remaining]
	d.srcBaseFrame += dropFrames
}

func (d *normalizedDecoder) readMoreFrames() error {
	const chunkFrames = 2048

	readSize := chunkFrames * d.srcFrameSize
	if cap(d.tmpSrc) < readSize {
		d.tmpSrc = make([]byte, readSize)
	}
	buf := d.tmpSrc[:readSize]

	n, err := d.src.Read(buf)
	if n == 0 {
		if err == nil {
			return io.EOF
		}
		return err
	}

	frameCount := n / d.srcFrameSize
	if frameCount == 0 {
		if err != nil {
			return err
		}
		return fmt.Errorf("decoder returned partial PCM frame")
	}
	if frameCount*d.srcFrameSize != n {
		return fmt.Errorf("decoder returned %d trailing bytes", n-frameCount*d.srcFrameSize)
	}

	oldLen := len(d.srcFrames)
	needLen := oldLen + frameCount*playbackChannels
	if cap(d.srcFrames) < needLen {
		next := make([]int16, oldLen, maxInt(needLen, oldLen*2+playbackChannels))
		copy(next, d.srcFrames)
		d.srcFrames = next
	}
	d.srcFrames = d.srcFrames[:needLen]
	dst := d.srcFrames[oldLen:needLen]

	switch d.srcChannels {
	case 1:
		for i := 0; i < frameCount; i++ {
			s := int16(binary.LittleEndian.Uint16(buf[i*2:]))
			dst[i*2] = s
			dst[i*2+1] = s
			d.lastFrame[0] = s
			d.lastFrame[1] = s
		}
	case 2:
		for i := 0; i < frameCount; i++ {
			srcOff := i * playbackFrameSize
			left := int16(binary.LittleEndian.Uint16(buf[srcOff:]))
			right := int16(binary.LittleEndian.Uint16(buf[srcOff+2:]))
			dst[i*2] = left
			dst[i*2+1] = right
			d.lastFrame[0] = left
			d.lastFrame[1] = right
		}
	default:
		return fmt.Errorf("unsupported channel count: %d", d.srcChannels)
	}

	d.haveLast = true
	return nil
}

func (d *normalizedDecoder) frameAt(absFrame int64) (int16, int16, error) {
	if absFrame >= d.totalSrcFrames {
		if d.haveLast {
			return d.lastFrame[0], d.lastFrame[1], nil
		}
		return 0, 0, io.EOF
	}
	if absFrame < d.srcBaseFrame {
		return 0, 0, fmt.Errorf("frame %d fell behind buffered source data", absFrame)
	}

	relFrame := absFrame - d.srcBaseFrame
	offset := int(relFrame) * playbackChannels
	if offset+1 >= len(d.srcFrames) {
		return 0, 0, io.EOF
	}
	return d.srcFrames[offset], d.srcFrames[offset+1], nil
}

func interpolateSample(a, b int16, fracNum int64) int16 {
	if fracNum == 0 || a == b {
		return a
	}
	diff := int64(int32(b) - int32(a))
	return int16(int64(int32(a)) + (diff*fracNum+playbackSampleRate/2)/playbackSampleRate)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
