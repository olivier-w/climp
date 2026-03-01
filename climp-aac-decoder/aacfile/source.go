package aacfile

import (
	"bytes"
	"fmt"
	"io"

	"github.com/Eyevinn/mp4ff/mp4"
)

const (
	aacLCProfile = 2
	aacFrameSize = 1024
)

type ascConfig struct {
	objectType      int
	sampleRate      int
	sampleRateIndex int
	channelConfig   int
}

type accessUnit struct {
	offset    int64
	size      int
	rawStart  int
	pcmFrames int
}

type containerSource struct {
	container string
	reader    io.ReaderAt
	asc       []byte
	cfg       ascConfig
	units     []accessUnit
	leading   int
	totalPCM  int
	totalRaw  int
}

func parseContainer(container string, reader io.ReaderAt, size int64) (*containerSource, error) {
	switch container {
	case ".aac":
		return parseADTSContainer(reader, size)
	case ".m4a", ".m4b":
		return parseMP4Container(container, reader, size)
	default:
		return nil, fmt.Errorf("unsupported AAC container: %s", container)
	}
}

func parseADTSContainer(reader io.ReaderAt, size int64) (*containerSource, error) {
	offset, err := skipID3v2(reader, size)
	if err != nil {
		return nil, err
	}
	if offset >= size {
		return nil, fmt.Errorf("invalid ADTS input: no frames")
	}

	var (
		asc   []byte
		cfg   ascConfig
		units []accessUnit
		rawAt int
	)
	var headerBuf [9]byte

	for offset < size {
		n, err := reader.ReadAt(headerBuf[:], offset)
		if err != nil && err != io.EOF {
			return nil, err
		}
		header, err := readADTSHeader(headerBuf[:n])
		if err != nil {
			return nil, fmt.Errorf("parsing ADTS header at byte %d: %w", offset, err)
		}
		if header.frameLength <= 0 || offset+int64(header.frameLength) > size {
			return nil, fmt.Errorf("truncated ADTS frame at byte %d", offset)
		}
		if header.rawDataBlocks != 1 {
			return nil, fmt.Errorf("unsupported ADTS frame count: %d", header.rawDataBlocks)
		}

		frameASC := makeASC(header.profile, header.sampleRateIndex, header.channelConfig)
		if asc == nil {
			asc = frameASC
			cfg, err = parseASC(frameASC)
			if err != nil {
				return nil, err
			}
		} else if !bytes.Equal(asc, frameASC) {
			return nil, fmt.Errorf("unsupported ADTS config change at byte %d", offset)
		}

		units = append(units, accessUnit{
			offset:    offset,
			size:      header.frameLength,
			rawStart:  rawAt,
			pcmFrames: aacFrameSize,
		})
		offset += int64(header.frameLength)
		rawAt += aacFrameSize
	}

	if len(units) == 0 {
		return nil, fmt.Errorf("no ADTS frames found")
	}

	return &containerSource{
		container: ".aac",
		reader:    reader,
		asc:       asc,
		cfg:       cfg,
		units:     units,
		totalPCM:  rawAt,
		totalRaw:  rawAt,
	}, nil
}

func parseMP4Container(container string, reader io.ReaderAt, size int64) (*containerSource, error) {
	file, err := mp4.DecodeFile(io.NewSectionReader(reader, 0, size), mp4.WithDecodeMode(mp4.DecModeLazyMdat))
	if err != nil {
		return nil, fmt.Errorf("decoding MP4: %w", err)
	}
	if file.IsFragmented() {
		return nil, fmt.Errorf("unsupported MP4 AAC track: fragmented")
	}
	if file.Moov == nil {
		return nil, fmt.Errorf("invalid MP4: missing moov box")
	}

	var audioTracks []*mp4.TrakBox
	for _, trak := range file.Moov.Traks {
		if trak != nil && trak.Mdia != nil && trak.Mdia.Hdlr != nil && trak.Mdia.Hdlr.HandlerType == "soun" {
			audioTracks = append(audioTracks, trak)
		}
	}
	if len(audioTracks) != 1 {
		return nil, fmt.Errorf("unsupported MP4 AAC track layout: expected exactly one audio track, found %d", len(audioTracks))
	}

	trak := audioTracks[0]
	if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil || trak.Mdia.Minf.Stbl.Stsd == nil {
		return nil, fmt.Errorf("invalid MP4 AAC track: incomplete sample table")
	}

	stsd := trak.Mdia.Minf.Stbl.Stsd
	if len(stsd.Children) != 1 {
		return nil, fmt.Errorf("unsupported MP4 AAC track: multiple sample descriptions")
	}
	if stsd.Enca != nil {
		return nil, fmt.Errorf("unsupported MP4 AAC track: encrypted")
	}
	sampleEntry := stsd.Mp4a
	if sampleEntry == nil {
		return nil, fmt.Errorf("unsupported MP4 audio sample entry: %s", stsd.Children[0].Type())
	}
	if sampleEntry.Sinf != nil {
		return nil, fmt.Errorf("unsupported MP4 AAC track: encrypted")
	}
	if sampleEntry.Esds == nil ||
		sampleEntry.Esds.DecConfigDescriptor == nil ||
		sampleEntry.Esds.DecConfigDescriptor.DecSpecificInfo == nil ||
		len(sampleEntry.Esds.DecConfigDescriptor.DecSpecificInfo.DecConfig) == 0 {
		return nil, fmt.Errorf("invalid MP4 AAC track: missing AudioSpecificConfig")
	}

	asc := append([]byte(nil), sampleEntry.Esds.DecConfigDescriptor.DecSpecificInfo.DecConfig...)
	cfg, err := parseASC(asc)
	if err != nil {
		return nil, err
	}

	sampleDurations, err := expandSTTS(trak)
	if err != nil {
		return nil, err
	}
	if len(sampleDurations) != int(trak.GetNrSamples()) {
		return nil, fmt.Errorf("invalid MP4 AAC track: sample table mismatch")
	}

	leading, err := mp4LeadingTrim(trak)
	if err != nil {
		return nil, err
	}

	totalPCM := 0
	rawAt := 0
	units := make([]accessUnit, 0, trak.GetNrSamples())
	for idx, pcmFrames := range sampleDurations {
		sampleNr := uint32(idx) + 1
		ranges, err := trak.GetRangesForSampleInterval(sampleNr, sampleNr)
		if err != nil {
			return nil, fmt.Errorf("locating MP4 sample %d: %w", sampleNr, err)
		}
		if len(ranges) != 1 {
			return nil, fmt.Errorf("unexpected AAC sample range count: %d", len(ranges))
		}
		start := int64(ranges[0].Offset)
		end := start + int64(ranges[0].Size)
		if start < 0 || end > size || start > end {
			return nil, fmt.Errorf("invalid MP4 sample bounds for sample %d", sampleNr)
		}
		units = append(units, accessUnit{
			offset:    start,
			size:      int(ranges[0].Size),
			rawStart:  rawAt,
			pcmFrames: pcmFrames,
		})
		rawAt += pcmFrames
		totalPCM += pcmFrames
	}

	if leading > totalPCM {
		return nil, fmt.Errorf("unsupported MP4 AAC track: edit list beyond duration")
	}
	totalPCM -= leading
	if totalPCM <= 0 {
		return nil, fmt.Errorf("invalid MP4 AAC track: empty decoded duration")
	}

	return &containerSource{
		container: container,
		reader:    reader,
		asc:       asc,
		cfg:       cfg,
		units:     units,
		leading:   leading,
		totalPCM:  totalPCM,
		totalRaw:  rawAt,
	}, nil
}

type adtsHeader struct {
	profile         int
	sampleRateIndex int
	channelConfig   int
	frameLength     int
	rawDataBlocks   int
}

func readADTSHeader(frame []byte) (adtsHeader, error) {
	if len(frame) < 7 {
		return adtsHeader{}, io.ErrUnexpectedEOF
	}
	if frame[0] != 0xFF || frame[1]&0xF0 != 0xF0 {
		return adtsHeader{}, fmt.Errorf("invalid syncword")
	}

	header := adtsHeader{
		profile:         int((frame[2]>>6)&0x03) + 1,
		sampleRateIndex: int((frame[2] >> 2) & 0x0F),
		channelConfig:   int((frame[2]&0x01)<<2 | (frame[3]>>6)&0x03),
		frameLength: int((uint16(frame[3]&0x03) << 11) |
			(uint16(frame[4]) << 3) |
			(uint16(frame[5]) >> 5)),
		rawDataBlocks: int(frame[6]&0x03) + 1,
	}
	if header.profile != aacLCProfile {
		return adtsHeader{}, fmt.Errorf("unsupported AAC profile: %d (AAC-LC only)", header.profile)
	}
	if header.sampleRateIndex >= len(sampleRates) || sampleRates[header.sampleRateIndex] == 0 {
		return adtsHeader{}, fmt.Errorf("unsupported AAC sample-rate index: %d", header.sampleRateIndex)
	}
	if header.channelConfig < 1 || header.channelConfig > 2 {
		return adtsHeader{}, fmt.Errorf("unsupported AAC channel configuration: %d", header.channelConfig)
	}
	return header, nil
}

func skipID3v2(reader io.ReaderAt, size int64) (int64, error) {
	var header [10]byte
	n, err := reader.ReadAt(header[:], 0)
	if err != nil && err != io.EOF {
		return 0, err
	}
	if n < len(header) || string(header[:3]) != "ID3" {
		return 0, nil
	}

	tagSize := int64(header[6]&0x7f)<<21 |
		int64(header[7]&0x7f)<<14 |
		int64(header[8]&0x7f)<<7 |
		int64(header[9]&0x7f)
	total := int64(10) + tagSize
	if header[5]&0x10 != 0 {
		total += 10
	}
	if total > size {
		return size, nil
	}
	return total, nil
}

func expandSTTS(trak *mp4.TrakBox) ([]int, error) {
	if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil || trak.Mdia.Minf.Stbl.Stts == nil {
		return nil, fmt.Errorf("invalid MP4 AAC track: incomplete timing tables")
	}

	stts := trak.Mdia.Minf.Stbl.Stts
	totalSamples := trak.GetNrSamples()
	durations := make([]int, 0, totalSamples)
	for i := range stts.SampleCount {
		count := int(stts.SampleCount[i])
		delta := int(stts.SampleTimeDelta[i])
		isLastEntry := i == len(stts.SampleCount)-1
		if delta <= 0 || delta > aacFrameSize {
			return nil, fmt.Errorf("unsupported MP4 AAC sample delta: %d", delta)
		}
		if !isLastEntry && delta != aacFrameSize {
			return nil, fmt.Errorf("unsupported MP4 AAC sample delta: %d", delta)
		}
		if isLastEntry && count > 1 && delta != aacFrameSize {
			return nil, fmt.Errorf("unsupported MP4 AAC sample delta layout")
		}
		for j := 0; j < count; j++ {
			durations = append(durations, delta)
		}
	}
	return durations, nil
}

func mp4LeadingTrim(trak *mp4.TrakBox) (int, error) {
	if trak.Edts == nil || len(trak.Edts.Elst) == 0 {
		return 0, nil
	}
	if len(trak.Edts.Elst) != 1 || len(trak.Edts.Elst[0].Entries) != 1 {
		return 0, fmt.Errorf("unsupported MP4 AAC track: complex edit lists")
	}

	entry := trak.Edts.Elst[0].Entries[0]
	if entry.MediaRateInteger != 1 || entry.MediaRateFraction != 0 {
		return 0, fmt.Errorf("unsupported MP4 AAC track: non-unit edit rate")
	}
	if entry.MediaTime < 0 {
		return 0, fmt.Errorf("unsupported MP4 AAC track: negative edit list media time")
	}
	return int(entry.MediaTime), nil
}

func parseASC(asc []byte) (ascConfig, error) {
	if len(asc) < 2 {
		return ascConfig{}, fmt.Errorf("invalid AudioSpecificConfig")
	}

	objectType := int(asc[0] >> 3)
	sampleRateIndex := int(((asc[0] & 0x07) << 1) | (asc[1] >> 7))
	channelConfig := int((asc[1] >> 3) & 0x0F)

	if objectType != aacLCProfile {
		return ascConfig{}, fmt.Errorf("unsupported AAC profile: %d (AAC-LC only)", objectType)
	}
	if sampleRateIndex >= len(sampleRates) || sampleRates[sampleRateIndex] == 0 {
		return ascConfig{}, fmt.Errorf("unsupported AAC sample-rate index: %d", sampleRateIndex)
	}
	if channelConfig < 1 || channelConfig > 2 {
		return ascConfig{}, fmt.Errorf("unsupported AAC channel configuration: %d", channelConfig)
	}

	return ascConfig{
		objectType:      objectType,
		sampleRate:      sampleRates[sampleRateIndex],
		sampleRateIndex: sampleRateIndex,
		channelConfig:   channelConfig,
	}, nil
}

func makeASC(objectType, sampleRateIndex, channelConfig int) []byte {
	return []byte{
		byte(objectType<<3) | byte(sampleRateIndex>>1),
		byte(sampleRateIndex&0x01)<<7 | byte(channelConfig<<3),
	}
}

func makeADTSFrame(asc []byte, payload []byte) []byte {
	cfg, err := parseASC(asc)
	if err != nil {
		return nil
	}

	frameLength := len(payload) + 7
	header := []byte{
		0xFF,
		0xF1,
		byte((cfg.objectType-1)<<6 | (cfg.sampleRateIndex&0x0f)<<2 | ((cfg.channelConfig >> 2) & 0x01)),
		byte((cfg.channelConfig&0x03)<<6 | ((frameLength >> 11) & 0x03)),
		byte((frameLength >> 3) & 0xFF),
		byte(((frameLength & 0x07) << 5) | 0x1F),
		0xFC,
	}

	frame := make([]byte, 0, frameLength)
	frame = append(frame, header...)
	frame = append(frame, payload...)
	return frame
}

var sampleRates = [...]int{
	96000, 88200, 64000, 48000, 44100, 32000,
	24000, 22050, 16000, 12000, 11025, 8000,
}

func (s *containerSource) readAccessUnit(index int, dst []byte) ([]byte, error) {
	if index < 0 || index >= len(s.units) {
		return nil, io.EOF
	}
	unit := s.units[index]
	if cap(dst) < unit.size {
		dst = make([]byte, unit.size)
	}
	dst = dst[:unit.size]
	if _, err := s.reader.ReadAt(dst, unit.offset); err != nil {
		return nil, err
	}
	return dst, nil
}

func (s *containerSource) locateRawFrame(frame int) (int, int) {
	if frame <= 0 {
		return 0, 0
	}
	if frame >= s.totalRaw {
		return len(s.units), 0
	}

	lo, hi := 0, len(s.units)
	for lo < hi {
		mid := lo + (hi-lo)/2
		unit := s.units[mid]
		if unit.rawStart+unit.pcmFrames <= frame {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	unit := s.units[lo]
	return lo, frame - unit.rawStart
}
