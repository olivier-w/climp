package aacfile

import (
	"bytes"
	"errors"
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
		return nil, unsupportedFeature("container", container)
	}
}

func parseADTSContainer(reader io.ReaderAt, size int64) (*containerSource, error) {
	offset, err := skipID3v2(reader, size)
	if err != nil {
		return nil, err
	}
	if offset >= size {
		return nil, malformedf("invalid ADTS input: no frames")
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
			return nil, malformedf("parsing ADTS header at byte %d: %v", offset, err)
		}
		if header.frameLength <= 0 || offset+int64(header.frameLength) > size {
			return nil, malformedf("truncated ADTS frame at byte %d", offset)
		}
		if header.rawDataBlocks != 1 {
			return nil, unsupportedFeature("ADTS raw data blocks", fmt.Sprintf("%d", header.rawDataBlocks))
		}

		frameASC := makeASC(header.profile, header.sampleRateIndex, header.channelConfig)
		if asc == nil {
			asc = frameASC
			cfg, err = parseASC(frameASC)
			if err != nil {
				return nil, err
			}
		} else if !bytes.Equal(asc, frameASC) {
			return nil, unsupportedFeature("ADTS config change", fmt.Sprintf("byte %d", offset))
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
		return nil, malformedf("no ADTS frames found")
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
		return nil, malformedf("decoding MP4: %v", err)
	}
	if file.IsFragmented() {
		return nil, unsupportedFeature("fragmented MP4", "")
	}
	if file.Moov == nil {
		return nil, malformedf("invalid MP4: missing moov box")
	}

	var audioTracks []*mp4.TrakBox
	for _, trak := range file.Moov.Traks {
		if trak != nil && trak.Mdia != nil && trak.Mdia.Hdlr != nil && trak.Mdia.Hdlr.HandlerType == "soun" {
			audioTracks = append(audioTracks, trak)
		}
	}
	if len(audioTracks) != 1 {
		return nil, unsupportedFeature("MP4 audio track layout", fmt.Sprintf("expected exactly one audio track, found %d", len(audioTracks)))
	}

	trak := audioTracks[0]
	if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil || trak.Mdia.Minf.Stbl.Stsd == nil {
		return nil, malformedf("invalid MP4 AAC track: incomplete sample table")
	}

	stsd := trak.Mdia.Minf.Stbl.Stsd
	if len(stsd.Children) != 1 {
		return nil, unsupportedFeature("MP4 sample descriptions", "multiple sample descriptions")
	}
	if stsd.Enca != nil {
		return nil, unsupportedFeature("encrypted MP4", "")
	}
	sampleEntry := stsd.Mp4a
	if sampleEntry == nil {
		return nil, unsupportedFeature("MP4 audio sample entry", stsd.Children[0].Type())
	}
	if sampleEntry.Sinf != nil {
		return nil, unsupportedFeature("encrypted MP4", "")
	}
	if sampleEntry.Esds == nil ||
		sampleEntry.Esds.DecConfigDescriptor == nil ||
		sampleEntry.Esds.DecConfigDescriptor.DecSpecificInfo == nil ||
		len(sampleEntry.Esds.DecConfigDescriptor.DecSpecificInfo.DecConfig) == 0 {
		return nil, malformedf("invalid MP4 AAC track: missing AudioSpecificConfig")
	}

	asc := append([]byte(nil), sampleEntry.Esds.DecConfigDescriptor.DecSpecificInfo.DecConfig...)
	cfg, err := parseASC(asc)
	if err != nil {
		return nil, err
	}

	leading, err := mp4LeadingTrim(trak)
	if err != nil {
		return nil, err
	}

	units, totalPCM, rawAt, err := buildMP4AccessUnits(trak, size)
	if err != nil {
		return nil, err
	}

	if leading > totalPCM {
		return nil, unsupportedFeature("MP4 edit list", "edit list beyond duration")
	}
	totalPCM -= leading
	if totalPCM <= 0 {
		return nil, malformedf("invalid MP4 AAC track: empty decoded duration")
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
		return adtsHeader{}, malformedf("unexpected EOF in ADTS header")
	}
	if frame[0] != 0xFF || frame[1]&0xF0 != 0xF0 {
		return adtsHeader{}, malformedf("invalid ADTS syncword")
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
		return adtsHeader{}, unsupportedFeature("AAC profile", fmt.Sprintf("%d", header.profile))
	}
	if header.sampleRateIndex >= len(sampleRates) || sampleRates[header.sampleRateIndex] == 0 {
		return adtsHeader{}, unsupportedFeature("AAC sample rate index", fmt.Sprintf("%d", header.sampleRateIndex))
	}
	if header.channelConfig < 1 || header.channelConfig > 2 {
		return adtsHeader{}, unsupportedFeature("AAC channel configuration", fmt.Sprintf("%d", header.channelConfig))
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

func buildMP4AccessUnits(trak *mp4.TrakBox, size int64) ([]accessUnit, int, int, error) {
	if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil {
		return nil, 0, 0, malformedf("invalid MP4 AAC track: incomplete sample table")
	}

	stbl := trak.Mdia.Minf.Stbl
	if stbl.Stsc == nil || stbl.Stsz == nil || stbl.Stts == nil {
		return nil, 0, 0, malformedf("invalid MP4 AAC track: incomplete sample table")
	}
	if stbl.Stco == nil && stbl.Co64 == nil {
		return nil, 0, 0, malformedf("invalid MP4 AAC track: missing chunk offsets")
	}
	if len(stbl.Stsc.Entries) == 0 {
		return nil, 0, 0, malformedf("invalid MP4 AAC track: empty chunk map")
	}

	totalSamples := int(trak.GetNrSamples())
	if totalSamples <= 0 {
		return nil, 0, 0, malformedf("invalid MP4 AAC track: empty sample table")
	}

	sampleSizes, err := mp4SampleSizes(stbl.Stsz, totalSamples)
	if err != nil {
		return nil, 0, 0, err
	}
	stts, err := newMP4STTSCursor(stbl.Stts, totalSamples)
	if err != nil {
		return nil, 0, 0, err
	}
	chunkOffsets, err := mp4ChunkOffsets(stbl)
	if err != nil {
		return nil, 0, 0, err
	}

	units := make([]accessUnit, totalSamples)
	rawAt := 0
	totalPCM := 0
	sampleIndex := 0
	entryIndex := 0
	entry := stbl.Stsc.Entries[entryIndex]

	for chunkIndex := 0; chunkIndex < len(chunkOffsets) && sampleIndex < totalSamples; chunkIndex++ {
		chunkNr := uint32(chunkIndex + 1)
		for entryIndex+1 < len(stbl.Stsc.Entries) && chunkNr >= stbl.Stsc.Entries[entryIndex+1].FirstChunk {
			entryIndex++
			entry = stbl.Stsc.Entries[entryIndex]
		}
		if entry.SamplesPerChunk == 0 {
			return nil, 0, 0, malformedf("invalid MP4 AAC track: zero samples per chunk")
		}

		offset := chunkOffsets[chunkIndex]
		samplesPerChunk := int(entry.SamplesPerChunk)
		for i := 0; i < samplesPerChunk && sampleIndex < totalSamples; i++ {
			sampleSize := sampleSizes[sampleIndex]
			end := offset + int64(sampleSize)
			if offset < 0 || end < offset || end > size {
				return nil, 0, 0, malformedf("invalid MP4 sample bounds for sample %d", sampleIndex+1)
			}

			pcmFrames, err := stts.Next()
			if err != nil {
				return nil, 0, 0, err
			}

			units[sampleIndex] = accessUnit{
				offset:    offset,
				size:      sampleSize,
				rawStart:  rawAt,
				pcmFrames: pcmFrames,
			}
			offset = end
			rawAt += pcmFrames
			totalPCM += pcmFrames
			sampleIndex++
		}
	}

	if sampleIndex != totalSamples {
		return nil, 0, 0, malformedf("invalid MP4 AAC track: sample table mismatch")
	}
	if err := stts.Done(); err != nil {
		return nil, 0, 0, err
	}
	return units, totalPCM, rawAt, nil
}

func mp4SampleSizes(stsz *mp4.StszBox, totalSamples int) ([]int, error) {
	if stsz == nil {
		return nil, malformedf("invalid MP4 AAC track: missing sample sizes")
	}
	if int(stsz.GetNrSamples()) != totalSamples {
		return nil, malformedf("invalid MP4 AAC track: sample table mismatch")
	}

	sizes := make([]int, totalSamples)
	if stsz.SampleUniformSize != 0 {
		size := int(stsz.SampleUniformSize)
		for i := range sizes {
			sizes[i] = size
		}
		return sizes, nil
	}
	if len(stsz.SampleSize) != totalSamples {
		return nil, malformedf("invalid MP4 AAC track: sample table mismatch")
	}
	for i, size := range stsz.SampleSize {
		sizes[i] = int(size)
	}
	return sizes, nil
}

func mp4ChunkOffsets(stbl *mp4.StblBox) ([]int64, error) {
	switch {
	case stbl == nil:
		return nil, malformedf("invalid MP4 AAC track: incomplete sample table")
	case stbl.Stco != nil:
		offsets := make([]int64, len(stbl.Stco.ChunkOffset))
		for i, offset := range stbl.Stco.ChunkOffset {
			offsets[i] = int64(offset)
		}
		return offsets, nil
	case stbl.Co64 != nil:
		offsets := make([]int64, len(stbl.Co64.ChunkOffset))
		for i, offset := range stbl.Co64.ChunkOffset {
			if offset > uint64(^uint64(0)>>1) {
				return nil, malformedf("invalid MP4 chunk offset")
			}
			offsets[i] = int64(offset)
		}
		return offsets, nil
	default:
		return nil, malformedf("invalid MP4 AAC track: missing chunk offsets")
	}
}

type mp4STTSCursor struct {
	sampleCount []uint32
	delta       []uint32
	entryIndex  int
	remaining   uint32
	left        int
}

func newMP4STTSCursor(stts *mp4.SttsBox, totalSamples int) (*mp4STTSCursor, error) {
	if stts == nil || len(stts.SampleCount) == 0 || len(stts.SampleCount) != len(stts.SampleTimeDelta) {
		return nil, malformedf("invalid MP4 AAC track: incomplete timing tables")
	}

	total := 0
	for i := range stts.SampleCount {
		count := int(stts.SampleCount[i])
		delta := int(stts.SampleTimeDelta[i])
		isLastEntry := i == len(stts.SampleCount)-1
		if count <= 0 {
			return nil, malformedf("invalid MP4 AAC track: empty timing entry")
		}
		if delta <= 0 || delta > aacFrameSize {
			return nil, unsupportedFeature("MP4 sample delta", fmt.Sprintf("%d", delta))
		}
		if !isLastEntry && delta != aacFrameSize {
			return nil, unsupportedFeature("MP4 sample delta", fmt.Sprintf("%d", delta))
		}
		if isLastEntry && count > 1 && delta != aacFrameSize {
			return nil, unsupportedFeature("MP4 sample delta layout", "")
		}
		total += count
	}
	if total != totalSamples {
		return nil, malformedf("invalid MP4 AAC track: sample table mismatch")
	}

	return &mp4STTSCursor{
		sampleCount: stts.SampleCount,
		delta:       stts.SampleTimeDelta,
		remaining:   stts.SampleCount[0],
		left:        totalSamples,
	}, nil
}

func (c *mp4STTSCursor) Next() (int, error) {
	if c.left == 0 {
		return 0, malformedf("invalid MP4 AAC track: sample table mismatch")
	}
	for c.remaining == 0 {
		c.entryIndex++
		if c.entryIndex >= len(c.sampleCount) {
			return 0, malformedf("invalid MP4 AAC track: sample table mismatch")
		}
		c.remaining = c.sampleCount[c.entryIndex]
	}

	delta := int(c.delta[c.entryIndex])
	c.remaining--
	c.left--
	return delta, nil
}

func (c *mp4STTSCursor) Done() error {
	if c.left != 0 {
		return malformedf("invalid MP4 AAC track: sample table mismatch")
	}
	return nil
}

func mp4LeadingTrim(trak *mp4.TrakBox) (int, error) {
	if trak.Edts == nil || len(trak.Edts.Elst) == 0 {
		return 0, nil
	}
	if len(trak.Edts.Elst) != 1 || len(trak.Edts.Elst[0].Entries) != 1 {
		return 0, unsupportedFeature("MP4 edit list", "complex edit lists")
	}

	entry := trak.Edts.Elst[0].Entries[0]
	if entry.MediaRateInteger != 1 || entry.MediaRateFraction != 0 {
		return 0, unsupportedFeature("MP4 edit list", "non-unit edit rate")
	}
	if entry.MediaTime < 0 {
		return 0, unsupportedFeature("MP4 edit list", "negative media time")
	}
	return int(entry.MediaTime), nil
}

func parseASC(asc []byte) (ascConfig, error) {
	if len(asc) < 2 {
		return ascConfig{}, malformedf("invalid AudioSpecificConfig")
	}

	br := ascBitReader{data: asc}

	objectType, err := br.readAudioObjectType()
	if err != nil {
		return ascConfig{}, malformedf("invalid AudioSpecificConfig: %v", err)
	}
	if objectType == 5 || objectType == 29 {
		return ascConfig{}, unsupportedFeature("HE-AAC/SBR", "base AudioSpecificConfig")
	}

	sampleRateIndex, err := br.readSampleRateIndex()
	if err != nil {
		if errors.Is(err, ErrUnsupportedBitstream) {
			return ascConfig{}, err
		}
		return ascConfig{}, malformedf("invalid AudioSpecificConfig: %v", err)
	}
	channelConfig, err := br.readInt(4)
	if err != nil {
		return ascConfig{}, malformedf("invalid AudioSpecificConfig: %v", err)
	}

	if objectType != aacLCProfile {
		return ascConfig{}, unsupportedFeature("AAC profile", fmt.Sprintf("%d", objectType))
	}
	if sampleRateIndex >= len(sampleRates) || sampleRates[sampleRateIndex] == 0 {
		return ascConfig{}, unsupportedFeature("AAC sample rate index", fmt.Sprintf("%d", sampleRateIndex))
	}
	if channelConfig < 1 || channelConfig > 2 {
		return ascConfig{}, unsupportedFeature("AAC channel configuration", fmt.Sprintf("%d", channelConfig))
	}
	if err := br.skipGASpecificConfig(objectType, channelConfig); err != nil {
		return ascConfig{}, err
	}
	if err := br.rejectSyncExtensions(); err != nil {
		return ascConfig{}, err
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

type ascBitReader struct {
	data []byte
	bit  int
}

func (r *ascBitReader) bitsLeft() int {
	return len(r.data)*8 - r.bit
}

func (r *ascBitReader) readBits(n int) (uint64, error) {
	if n < 0 {
		return 0, fmt.Errorf("invalid bit count %d", n)
	}
	if r.bitsLeft() < n {
		return 0, fmt.Errorf("unexpected EOF")
	}

	var out uint64
	for i := 0; i < n; i++ {
		byteIdx := (r.bit + i) / 8
		bitIdx := 7 - ((r.bit + i) % 8)
		out = (out << 1) | uint64((r.data[byteIdx]>>bitIdx)&0x01)
	}
	r.bit += n
	return out, nil
}

func (r *ascBitReader) readInt(n int) (int, error) {
	v, err := r.readBits(n)
	return int(v), err
}

func (r *ascBitReader) readAudioObjectType() (int, error) {
	objectType, err := r.readInt(5)
	if err != nil {
		return 0, err
	}
	if objectType == 31 {
		ext, err := r.readInt(6)
		if err != nil {
			return 0, err
		}
		objectType = 32 + ext
	}
	return objectType, nil
}

func (r *ascBitReader) readSampleRateIndex() (int, error) {
	sampleRateIndex, err := r.readInt(4)
	if err != nil {
		return 0, err
	}
	if sampleRateIndex == 15 {
		return 0, unsupportedFeature("AAC sample rate", "explicit sample rate escape")
	}
	return sampleRateIndex, nil
}

func (r *ascBitReader) skipGASpecificConfig(objectType, channelConfig int) error {
	frameLengthFlag, err := r.readInt(1)
	if err != nil {
		return malformedf("invalid AudioSpecificConfig: %v", err)
	}
	if frameLengthFlag != 0 {
		return unsupportedFeature("AAC frame length", "960-sample frames")
	}

	dependsOnCoreCoder, err := r.readInt(1)
	if err != nil {
		return malformedf("invalid AudioSpecificConfig: %v", err)
	}
	if dependsOnCoreCoder != 0 {
		return unsupportedFeature("AAC core coder dependency", "")
	}

	extensionFlag, err := r.readInt(1)
	if err != nil {
		return malformedf("invalid AudioSpecificConfig: %v", err)
	}
	if channelConfig == 0 {
		return unsupportedFeature("AAC channel configuration", "program config element")
	}
	if extensionFlag != 0 {
		return unsupportedFeature("AAC extension flag", "")
	}
	if objectType != aacLCProfile {
		return unsupportedFeature("AAC profile", fmt.Sprintf("%d", objectType))
	}
	return nil
}

func (r *ascBitReader) rejectSyncExtensions() error {
	if r.bitsLeft() < 16 {
		return nil
	}

	syncExtensionType, err := r.readInt(11)
	if err != nil {
		return malformedf("invalid AudioSpecificConfig sync extension: %v", err)
	}
	if syncExtensionType != 0x2b7 {
		return nil
	}

	extensionObjectType, err := r.readAudioObjectType()
	if err != nil {
		return malformedf("invalid AudioSpecificConfig sync extension: %v", err)
	}
	switch extensionObjectType {
	case 5:
		sbrPresent, err := r.readInt(1)
		if err != nil {
			return malformedf("invalid AudioSpecificConfig sync extension: %v", err)
		}
		if sbrPresent != 0 {
			return unsupportedFeature("HE-AAC/SBR", "sync extension")
		}
	case 29:
		sbrPresent, err := r.readInt(1)
		if err != nil {
			return malformedf("invalid AudioSpecificConfig sync extension: %v", err)
		}
		if sbrPresent != 0 {
			return unsupportedFeature("HE-AACv2/PS", "sync extension")
		}
	}

	return nil
}

func appendADTSFrame(dst []byte, cfg ascConfig, payload []byte) []byte {
	frameLength := len(payload) + 7
	if cap(dst) < frameLength {
		dst = make([]byte, frameLength)
	}
	dst = dst[:frameLength]
	header := [...]byte{
		0xFF,
		0xF1,
		byte((cfg.objectType-1)<<6 | (cfg.sampleRateIndex&0x0f)<<2 | ((cfg.channelConfig >> 2) & 0x01)),
		byte((cfg.channelConfig&0x03)<<6 | ((frameLength >> 11) & 0x03)),
		byte((frameLength >> 3) & 0xFF),
		byte(((frameLength & 0x07) << 5) | 0x1F),
		0xFC,
	}
	copy(dst[:7], header[:])
	copy(dst[7:], payload)
	return dst
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
