package player

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Eyevinn/mp4ff/mp4"
	aacadts "github.com/skrashevich/go-aac/pkg/adts"
	aacdecoder "github.com/skrashevich/go-aac/pkg/decoder"
)

const (
	aacLCProfile  = 2
	aacFrameBytes = 2
	aacFrameSize  = 1024
)

type aacAccessUnitSource interface {
	ASC() []byte
	LeadingPCMFrames() int64
	TotalPCMFrames() int64
	TotalAccessUnits() int64
	SeekAccessUnit(index int64) error
	ReadAccessUnit() ([]byte, error)
}

type aacDecoder struct {
	source   aacAccessUnitSource
	asc      []byte
	codec    *aacdecoder.Decoder
	length   int64
	pos      int64
	channels int
	leading  int64

	buf          []byte
	tmpRaw       []byte
	discardBytes int
}

func newAACDecoder(f *os.File) (*aacDecoder, error) {
	var (
		source aacAccessUnitSource
		err    error
	)

	switch strings.ToLower(filepath.Ext(f.Name())) {
	case ".aac":
		source, err = newADTSAACSource(f)
	case ".m4a", ".m4b":
		source, err = newMP4AACSource(f)
	default:
		return nil, fmt.Errorf("unsupported AAC container: %s", filepath.Ext(f.Name()))
	}
	if err != nil {
		return nil, err
	}

	cfg, err := parseAACConfig(source.ASC())
	if err != nil {
		return nil, err
	}
	if cfg.Profile != aacLCProfile {
		return nil, fmt.Errorf("unsupported AAC profile: %d (AAC-LC only)", cfg.Profile)
	}
	if cfg.ChanConfig < 1 || cfg.ChanConfig > playbackChannels {
		return nil, fmt.Errorf("unsupported AAC channel configuration: %d", cfg.ChanConfig)
	}
	if cfg.FrameLength != aacFrameSize {
		return nil, fmt.Errorf("unsupported AAC frame length: %d", cfg.FrameLength)
	}

	d := &aacDecoder{
		source:   source,
		asc:      append([]byte(nil), source.ASC()...),
		length:   source.TotalPCMFrames() * int64(cfg.ChanConfig*aacFrameBytes),
		channels: cfg.ChanConfig,
		leading:  source.LeadingPCMFrames(),
	}
	if err := d.resetCodec(); err != nil {
		return nil, err
	}
	d.discardBytes = int(d.leading) * d.channels * aacFrameBytes
	return d, nil
}

func parseAACConfig(asc []byte) (aacdecoder.Config, error) {
	dec := aacdecoder.New()
	if err := dec.SetASC(asc); err != nil {
		return aacdecoder.Config{}, err
	}
	return dec.Config, nil
}

func (d *aacDecoder) resetCodec() error {
	dec := aacdecoder.New()
	if err := dec.SetASC(d.asc); err != nil {
		return err
	}
	d.codec = dec
	return nil
}

func (d *aacDecoder) Read(p []byte) (int, error) {
	total := 0
	remaining := d.length - d.pos
	if remaining <= 0 {
		return 0, io.EOF
	}

	if len(d.buf) > 0 {
		buf := d.buf
		if int64(len(buf)) > remaining {
			buf = buf[:int(remaining)]
		}
		n := copy(p, buf)
		d.buf = d.buf[n:]
		d.pos += int64(n)
		total += n
		p = p[n:]
		remaining -= int64(n)
		if len(p) == 0 {
			return total, nil
		}
	}

	for len(p) > 0 {
		raw, err := d.decodeAccessUnit()
		if err != nil {
			if total > 0 && err == io.EOF {
				return total, nil
			}
			if total > 0 {
				return total, nil
			}
			return total, err
		}

		remaining := d.length - d.pos
		if remaining <= 0 {
			if total > 0 {
				return total, nil
			}
			return 0, io.EOF
		}
		if int64(len(raw)) > remaining {
			raw = raw[:remaining]
		}

		n := copy(p, raw)
		if n < len(raw) {
			d.buf = append(d.buf[:0], raw[n:]...)
		}

		d.pos += int64(n)
		total += n
		p = p[n:]
		if n < len(raw) {
			return total, nil
		}
	}

	return total, nil
}

func (d *aacDecoder) decodeAccessUnit() ([]byte, error) {
	for {
		au, err := d.source.ReadAccessUnit()
		if err != nil {
			return nil, err
		}

		samples, err := d.codec.DecodeFrame(au)
		if err != nil {
			return nil, err
		}

		rawSize := len(samples) * aacFrameBytes
		if cap(d.tmpRaw) < rawSize {
			d.tmpRaw = make([]byte, rawSize)
		}
		raw := d.tmpRaw[:rawSize]
		for i, sample := range samples {
			binary.LittleEndian.PutUint16(raw[i*2:], uint16(floatSampleToPCM16(sample)))
		}

		if d.discardBytes > 0 {
			if d.discardBytes >= len(raw) {
				d.discardBytes -= len(raw)
				continue
			}
			raw = raw[d.discardBytes:]
			d.discardBytes = 0
		}

		if len(raw) == 0 {
			continue
		}
		return raw, nil
	}
}

func floatSampleToPCM16(sample float32) int16 {
	if sample >= 1 {
		return 32767
	}
	if sample <= -1 {
		return -32768
	}
	return int16(sample * 32767)
}

func (d *aacDecoder) Seek(offset int64, whence int) (int64, error) {
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

	frameSize := int64(d.channels * aacFrameBytes)
	newPos -= newPos % frameSize

	rawTotalFrames := d.source.TotalAccessUnits() * aacFrameSize
	targetFrame := newPos / frameSize
	rawFrameTarget := targetFrame + d.leading
	targetAU := rawFrameTarget / aacFrameSize
	discardFrames := rawFrameTarget % aacFrameSize
	startAU := targetAU
	if rawFrameTarget < rawTotalFrames && startAU > 0 {
		startAU--
		discardFrames += aacFrameSize
	}

	if err := d.source.SeekAccessUnit(startAU); err != nil {
		return d.pos, err
	}
	if err := d.resetCodec(); err != nil {
		return d.pos, err
	}

	d.buf = nil
	d.discardBytes = int(discardFrames * frameSize)
	d.pos = newPos
	return newPos, nil
}

func (d *aacDecoder) Length() int64     { return d.length }
func (d *aacDecoder) SampleRate() int   { return d.codec.Config.SampleRate }
func (d *aacDecoder) ChannelCount() int { return d.channels }

type adtsAACSource struct {
	file    *os.File
	asc     []byte
	offsets []int64
	sizes   []int
	total   int64
	index   int64
	buf     []byte
}

func newADTSAACSource(f *os.File) (*adtsAACSource, error) {
	startOffset, err := skipID3v2(f)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	var headerBuf [9]byte
	var offsets []int64
	var sizes []int
	var asc []byte
	offset := startOffset

	for offset < info.Size() {
		n, err := f.ReadAt(headerBuf[:], offset)
		if err != nil && err != io.EOF {
			return nil, err
		}
		if n < 7 {
			return nil, fmt.Errorf("invalid ADTS frame at byte %d", offset)
		}

		header, err := aacadts.ReadHeaderFromBytes(headerBuf[:n])
		if err != nil {
			return nil, fmt.Errorf("parsing ADTS header at byte %d: %w", offset, err)
		}
		if header.Profile != aacLCProfile {
			return nil, fmt.Errorf("unsupported AAC profile: %d (AAC-LC only)", header.Profile)
		}
		if header.ChannelConfig < 1 || header.ChannelConfig > playbackChannels {
			return nil, fmt.Errorf("unsupported AAC channel configuration: %d", header.ChannelConfig)
		}
		if header.NumFrames != 1 {
			return nil, fmt.Errorf("unsupported ADTS frame count: %d", header.NumFrames)
		}
		headerLen := 7
		if !header.ProtectionAbsent {
			headerLen = 9
		}
		if header.FrameLength < headerLen {
			return nil, fmt.Errorf("invalid ADTS frame length: %d", header.FrameLength)
		}
		if offset+int64(header.FrameLength) > info.Size() {
			return nil, fmt.Errorf("truncated ADTS frame at byte %d", offset)
		}

		frameASC, err := aacadts.AudioSpecificConfig(header)
		if err != nil {
			return nil, err
		}
		if asc == nil {
			asc = append([]byte(nil), frameASC[:]...)
		} else if asc[0] != frameASC[0] || asc[1] != frameASC[1] {
			return nil, fmt.Errorf("unsupported ADTS config change at byte %d", offset)
		}

		offsets = append(offsets, offset)
		sizes = append(sizes, header.FrameLength)
		offset += int64(header.FrameLength)
	}

	if len(offsets) == 0 {
		return nil, fmt.Errorf("no ADTS frames found")
	}

	return &adtsAACSource{
		file:    f,
		asc:     asc,
		offsets: offsets,
		sizes:   sizes,
		total:   int64(len(offsets)) * aacFrameSize,
	}, nil
}

func skipID3v2(f *os.File) (int64, error) {
	var header [10]byte
	n, err := f.ReadAt(header[:], 0)
	if err != nil && err != io.EOF {
		return 0, err
	}
	if n < len(header) || string(header[:3]) != "ID3" {
		return 0, nil
	}

	size := int64(header[6]&0x7f)<<21 |
		int64(header[7]&0x7f)<<14 |
		int64(header[8]&0x7f)<<7 |
		int64(header[9]&0x7f)
	total := int64(10) + size
	if header[5]&0x10 != 0 {
		total += 10
	}
	return total, nil
}

func (s *adtsAACSource) ASC() []byte             { return s.asc }
func (s *adtsAACSource) LeadingPCMFrames() int64 { return 0 }
func (s *adtsAACSource) TotalPCMFrames() int64   { return s.total }
func (s *adtsAACSource) TotalAccessUnits() int64 { return int64(len(s.offsets)) }

func (s *adtsAACSource) SeekAccessUnit(index int64) error {
	if index < 0 || index > int64(len(s.offsets)) {
		return fmt.Errorf("invalid AAC access unit index: %d", index)
	}
	s.index = index
	return nil
}

func (s *adtsAACSource) ReadAccessUnit() ([]byte, error) {
	if s.index >= int64(len(s.offsets)) {
		return nil, io.EOF
	}

	size := s.sizes[s.index]
	if cap(s.buf) < size {
		s.buf = make([]byte, size)
	}
	au := s.buf[:size]
	if _, err := s.file.ReadAt(au, s.offsets[s.index]); err != nil {
		return nil, err
	}
	s.index++
	return au, nil
}

type mp4AACSource struct {
	file    *os.File
	trak    *mp4.TrakBox
	asc     []byte
	leading int64
	total   int64
	index   int64
	buf     []byte
}

func newMP4AACSource(f *os.File) (*mp4AACSource, error) {
	file, err := mp4.DecodeFile(f, mp4.WithDecodeMode(mp4.DecModeLazyMdat))
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

	total := int64(trak.GetNrSamples())
	if total == 0 {
		return nil, fmt.Errorf("invalid MP4 AAC track: no samples")
	}

	leading, pcmTotal, err := mp4AACPCMWindow(trak)
	if err != nil {
		return nil, err
	}

	return &mp4AACSource{
		file:    f,
		trak:    trak,
		leading: leading,
		total:   pcmTotal,
		asc: append([]byte(nil),
			sampleEntry.Esds.DecConfigDescriptor.DecSpecificInfo.DecConfig...),
	}, nil
}

func mp4AACPCMWindow(trak *mp4.TrakBox) (leadingFrames int64, totalPCMFrames int64, err error) {
	if trak.Mdia == nil || trak.Mdia.Mdhd == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil || trak.Mdia.Minf.Stbl.Stts == nil {
		return 0, 0, fmt.Errorf("invalid MP4 AAC track: incomplete timing tables")
	}

	stts := trak.Mdia.Minf.Stbl.Stts
	for i := range stts.SampleCount {
		delta := stts.SampleTimeDelta[i]
		isLastEntry := i == len(stts.SampleCount)-1
		if delta == 0 || delta > aacFrameSize {
			return 0, 0, fmt.Errorf("unsupported MP4 AAC sample delta: %d", delta)
		}
		if !isLastEntry && delta != aacFrameSize {
			return 0, 0, fmt.Errorf("unsupported MP4 AAC sample delta: %d", delta)
		}
		if isLastEntry && stts.SampleCount[i] > 1 && delta != aacFrameSize {
			return 0, 0, fmt.Errorf("unsupported MP4 AAC sample delta layout")
		}
	}

	totalPCMFrames = int64(trak.Mdia.Mdhd.Duration)
	if trak.Edts == nil || len(trak.Edts.Elst) == 0 {
		return 0, totalPCMFrames, nil
	}
	if len(trak.Edts.Elst) != 1 || len(trak.Edts.Elst[0].Entries) != 1 {
		return 0, 0, fmt.Errorf("unsupported MP4 AAC track: complex edit lists")
	}

	entry := trak.Edts.Elst[0].Entries[0]
	if entry.MediaRateInteger != 1 || entry.MediaRateFraction != 0 {
		return 0, 0, fmt.Errorf("unsupported MP4 AAC track: non-unit edit rate")
	}
	if entry.MediaTime < 0 {
		return 0, 0, fmt.Errorf("unsupported MP4 AAC track: empty edit lists")
	}

	leadingFrames = entry.MediaTime
	if leadingFrames > totalPCMFrames {
		return 0, 0, fmt.Errorf("unsupported MP4 AAC track: edit list beyond duration")
	}
	return leadingFrames, totalPCMFrames - leadingFrames, nil
}

func (s *mp4AACSource) ASC() []byte             { return s.asc }
func (s *mp4AACSource) LeadingPCMFrames() int64 { return s.leading }
func (s *mp4AACSource) TotalPCMFrames() int64   { return s.total }
func (s *mp4AACSource) TotalAccessUnits() int64 { return int64(s.trak.GetNrSamples()) }

func (s *mp4AACSource) SeekAccessUnit(index int64) error {
	if index < 0 || index > int64(s.trak.GetNrSamples()) {
		return fmt.Errorf("invalid AAC access unit index: %d", index)
	}
	s.index = index
	return nil
}

func (s *mp4AACSource) ReadAccessUnit() ([]byte, error) {
	if s.index >= int64(s.trak.GetNrSamples()) {
		return nil, io.EOF
	}

	sampleNr := uint32(s.index) + 1
	ranges, err := s.trak.GetRangesForSampleInterval(sampleNr, sampleNr)
	if err != nil {
		return nil, err
	}
	if len(ranges) != 1 {
		return nil, fmt.Errorf("unexpected AAC sample range count: %d", len(ranges))
	}

	size := int(ranges[0].Size)
	if cap(s.buf) < size {
		s.buf = make([]byte, size)
	}
	au := s.buf[:size]
	if _, err := s.file.ReadAt(au, int64(ranges[0].Offset)); err != nil {
		return nil, err
	}

	s.index++
	return au, nil
}
