package player

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

const mp3DecoderDelaySamples = 529

func readMP3GaplessTrim(f *os.File) (startSamples int64, endSamples int64, err error) {
	pos, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		_, _ = f.Seek(pos, io.SeekStart)
	}()

	frameOffset, err := firstMP3FrameOffset(f)
	if err != nil {
		return 0, 0, nil
	}
	if _, err := f.Seek(frameOffset, io.SeekStart); err != nil {
		return 0, 0, err
	}

	headerBytes := make([]byte, 4)
	if _, err := io.ReadFull(f, headerBytes); err != nil {
		return 0, 0, nil
	}
	header, err := parseMP3FrameHeader(headerBytes)
	if err != nil {
		return 0, 0, nil
	}

	xingOffset := 4 + header.crcBytes + header.sideInfoBytes
	if _, err := f.Seek(frameOffset+int64(xingOffset), io.SeekStart); err != nil {
		return 0, 0, err
	}

	buf := make([]byte, 256)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return 0, 0, err
	}
	buf = buf[:n]

	startSamples, endSamples, ok := parseXingLAMEGapless(buf)
	if !ok {
		return 0, 0, nil
	}
	return startSamples, endSamples, nil
}

func firstMP3FrameOffset(f *os.File) (int64, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}

	header := make([]byte, 10)
	n, err := io.ReadFull(f, header)
	if err != nil && err != io.ErrUnexpectedEOF {
		return 0, err
	}
	if n < 10 {
		return 0, io.EOF
	}

	if bytes.Equal(header[:3], []byte("ID3")) {
		size := synchsafeUint32(header[6:10])
		footer := 0
		if header[5]&0x10 != 0 {
			footer = 10
		}
		return int64(10 + size + footer), nil
	}
	return 0, nil
}

func synchsafeUint32(b []byte) int {
	return int(b[0]&0x7f)<<21 | int(b[1]&0x7f)<<14 | int(b[2]&0x7f)<<7 | int(b[3]&0x7f)
}

type mp3FrameHeader struct {
	crcBytes     int
	sideInfoBytes int
}

func parseMP3FrameHeader(b []byte) (mp3FrameHeader, error) {
	if len(b) < 4 {
		return mp3FrameHeader{}, fmt.Errorf("short mp3 header")
	}
	h := binary.BigEndian.Uint32(b)
	if h>>21 != 0x7ff {
		return mp3FrameHeader{}, fmt.Errorf("invalid mp3 sync")
	}

	versionID := (h >> 19) & 0x3
	layer := (h >> 17) & 0x3
	protectionBit := (h >> 16) & 0x1
	channelMode := (h >> 6) & 0x3

	if layer != 0x1 {
		return mp3FrameHeader{}, fmt.Errorf("not layer iii")
	}
	if versionID == 0x1 {
		return mp3FrameHeader{}, fmt.Errorf("reserved mpeg version")
	}

	isMPEG1 := versionID == 0x3
	isMono := channelMode == 0x3

	sideInfoBytes := 0
	switch {
	case isMPEG1 && isMono:
		sideInfoBytes = 17
	case isMPEG1:
		sideInfoBytes = 32
	case isMono:
		sideInfoBytes = 9
	default:
		sideInfoBytes = 17
	}

	crcBytes := 0
	if protectionBit == 0 {
		crcBytes = 2
	}

	return mp3FrameHeader{
		crcBytes:      crcBytes,
		sideInfoBytes: sideInfoBytes,
	}, nil
}

func parseXingLAMEGapless(b []byte) (int64, int64, bool) {
	if len(b) < 8 {
		return 0, 0, false
	}
	tag := string(b[:4])
	if tag != "Xing" && tag != "Info" {
		return 0, 0, false
	}

	flags := binary.BigEndian.Uint32(b[4:8])
	offset := 8
	if flags&0x1 != 0 {
		offset += 4
	}
	if flags&0x2 != 0 {
		offset += 4
	}
	if flags&0x4 != 0 {
		offset += 100
	}
	if flags&0x8 != 0 {
		offset += 4
	}
	if len(b) < offset+24 {
		return 0, 0, false
	}

	delayPadding := b[offset+21 : offset+24]
	encDelay := int(delayPadding[0])<<4 | int(delayPadding[1]>>4)
	encPadding := int(delayPadding[1]&0x0f)<<8 | int(delayPadding[2])
	if encDelay == 0 && encPadding == 0 {
		return 0, 0, false
	}

	startSamples := int64(encDelay + mp3DecoderDelaySamples)
	endSamples := int64(encPadding - mp3DecoderDelaySamples)
	if endSamples < 0 {
		endSamples = 0
	}
	return startSamples, endSamples, true
}
