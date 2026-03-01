package aacfile

import (
	"fmt"
	"math"

	"github.com/olivier-w/climp-aac-decoder/third_party/gaad"
)

type channelState struct {
	overlap         []float64
	prevWindowShape uint8
}

type channelScratch struct {
	quantized    []int
	spec         []float64
	reorder      []float64
	pcm          []float64
	windowed     []float64
	noise        []float64
	scaleFactors [][]int
}

type synthDecoder struct {
	cfg       ascConfig
	state     []channelState
	scratch   []channelScratch
	noiseSeed uint32
	trace     TraceSink

	longIMDCTWork  imdctWork
	shortIMDCTWork imdctWork
	monoOut        []float64
	stereoOut      []float64
	adtsFrame      []byte
	tnsCoeffs      []float64
	tnsPredictor   []float64
	tnsWork        []float64
	tnsState       []float64
}

type icsMeta struct {
	windowSequence    uint8
	windowShape       uint8
	maxSFB            int
	numWindows        int
	numWindowGroups   int
	windowGroupLength []uint8
	sectSFBOffset     [][]uint16
	swbOffset         []uint16
	sfbCB             [][]uint8
}

type icsDecoded struct {
	meta         *icsMeta
	globalGain   int
	scaleFactors [][]int
	spec         []float64
	currentShape uint8
	pulsePresent bool
	pnsBands     int
	escBands     int
	maxQuantized int
	tnsPresent   bool
	tnsData      *gaad.TNSData
	tnsFilters   int
}

type channelSynthStats struct {
	specPeakPreTNS  float64
	specPeakPostTNS float64
	imdctPeak       float64
	overlapPeak     float64
	pcmPeak         float64
}

func newSynthDecoder(cfg ascConfig, trace TraceSink) *synthDecoder {
	initDecodeTables()

	state := make([]channelState, cfg.channelConfig)
	scratch := make([]channelScratch, cfg.channelConfig)
	for i := range state {
		state[i] = channelState{
			overlap: make([]float64, longWindowLength),
		}
		scratch[i] = channelScratch{
			quantized: make([]int, aacFrameSize),
			spec:      make([]float64, aacFrameSize),
			reorder:   make([]float64, aacFrameSize),
			pcm:       make([]float64, longWindowLength),
			windowed:  make([]float64, longBlockLength),
		}
	}
	d := &synthDecoder{
		cfg:            cfg,
		state:          state,
		scratch:        scratch,
		noiseSeed:      1,
		trace:          trace,
		longIMDCTWork:  newIMDCTWork(longWindowLength),
		shortIMDCTWork: newIMDCTWork(shortWindowLength),
	}
	if cfg.channelConfig == 1 {
		d.monoOut = make([]float64, longWindowLength)
	} else {
		d.stereoOut = make([]float64, longWindowLength*2)
	}
	return d
}

func (d *synthDecoder) reset(trace TraceSink) {
	d.trace = trace
	d.noiseSeed = 1
	for i := range d.state {
		clear(d.state[i].overlap)
		d.state[i].prevWindowShape = 0
	}
}

func (d *synthDecoder) decodeAccessUnit(src *containerSource, payload []byte, unitIndex, rawStart, pcmFrames int) ([]float64, error) {
	frame := payload
	if src.container != ".aac" {
		frame = appendADTSFrame(d.adtsFrame, src.cfg, payload)
		if frame == nil {
			return nil, fmt.Errorf("building synthetic ADTS frame")
		}
		d.adtsFrame = frame
	}

	adts, err := gaad.ParseADTS(frame)
	if err != nil {
		return nil, malformedf("parsing AAC frame: %v", err)
	}

	switch {
	case len(adts.Channel_pair_elements) == 1:
		if src.cfg.channelConfig != 2 {
			return nil, malformedf("unexpected channel pair element for %d-channel stream", src.cfg.channelConfig)
		}
		pcm, trace, err := d.decodeCPE(adts.Channel_pair_elements[0])
		if err != nil {
			return nil, err
		}
		d.emitTrace(src, unitIndex, rawStart, pcmFrames, trace)
		return pcm, nil
	case len(adts.Single_channel_elements) == 1:
		if src.cfg.channelConfig != 1 {
			return nil, malformedf("unexpected single channel element for %d-channel stream", src.cfg.channelConfig)
		}
		pcm, trace, err := d.decodeSCE(adts.Single_channel_elements[0])
		if err != nil {
			return nil, err
		}
		d.emitTrace(src, unitIndex, rawStart, pcmFrames, trace)
		return pcm, nil
	default:
		return nil, unsupportedFeature("AAC element layout", "only single-channel and channel-pair elements are supported")
	}
}

func (d *synthDecoder) decodeSCE(sce *gaad.SingleChannelElement) ([]float64, FrameTrace, error) {
	decoded, err := d.buildICSDecoded(0, sce.Channel_stream)
	if err != nil {
		return nil, FrameTrace{}, err
	}

	d.applyPNSMono(decoded)
	trace := baseTrace(decoded.meta)
	trace.PulsePresent = decoded.pulsePresent
	trace.PNSBands = decoded.pnsBands
	trace.ESCBands = decoded.escBands
	trace.MaxQuantized = decoded.maxQuantized
	trace.TNSPresent = decoded.tnsPresent
	trace.TNSFilters = decoded.tnsFilters
	trace.SpecPeakPreTNS = peakAbs(decoded.spec)
	if err := d.applyTNS(decoded); err != nil {
		return nil, FrameTrace{}, err
	}
	trace.SpecPeakPostTNS = peakAbs(decoded.spec)
	pcm, stats := d.synthChannel(0, decoded.meta, decoded.spec, decoded.currentShape)
	trace.IMDCTPeak = stats.imdctPeak
	trace.OverlapPeak = stats.overlapPeak
	trace.PCMPeak = stats.pcmPeak
	copy(d.monoOut, pcm)
	return d.monoOut, trace, nil
}

func (d *synthDecoder) decodeCPE(cpe *gaad.ChannelPairElement) ([]float64, FrameTrace, error) {
	left, err := d.buildICSDecoded(0, cpe.Channel_stream1)
	if err != nil {
		return nil, FrameTrace{}, err
	}
	right, err := d.buildICSDecoded(1, cpe.Channel_stream2)
	if err != nil {
		return nil, FrameTrace{}, err
	}

	msUsed := cpe.Ms_used
	d.applyPNSPair(left, right, msUsed)
	d.applyMS(left, right, msUsed)
	d.applyIntensity(left, right, msUsed)
	trace := baseTrace(left.meta)
	trace.PulsePresent = left.pulsePresent || right.pulsePresent
	trace.PNSBands = left.pnsBands + right.pnsBands
	trace.IntensityBands = countIntensityBands(right.meta)
	trace.MSBands = countMSBands(msUsed)
	trace.ESCBands = left.escBands + right.escBands
	trace.MaxQuantized = maxInt(left.maxQuantized, right.maxQuantized)
	trace.TNSPresent = left.tnsPresent || right.tnsPresent
	trace.TNSFilters = left.tnsFilters + right.tnsFilters
	trace.SpecPeakPreTNS = maxFloat64(peakAbs(left.spec), peakAbs(right.spec))
	if err := d.applyTNS(left); err != nil {
		return nil, FrameTrace{}, err
	}
	if err := d.applyTNS(right); err != nil {
		return nil, FrameTrace{}, err
	}
	trace.SpecPeakPostTNS = maxFloat64(peakAbs(left.spec), peakAbs(right.spec))

	leftPCM, leftStats := d.synthChannel(0, left.meta, left.spec, left.currentShape)
	rightPCM, rightStats := d.synthChannel(1, right.meta, right.spec, right.currentShape)
	trace.IMDCTPeak = maxFloat64(leftStats.imdctPeak, rightStats.imdctPeak)
	trace.OverlapPeak = maxFloat64(leftStats.overlapPeak, rightStats.overlapPeak)
	trace.PCMPeak = maxFloat64(leftStats.pcmPeak, rightStats.pcmPeak)

	out := d.stereoOut[:len(leftPCM)*2]
	for i := range leftPCM {
		out[i*2] = leftPCM[i]
		out[i*2+1] = rightPCM[i]
	}
	return out, trace, nil
}

func (d *synthDecoder) buildICSDecoded(channel int, stream *gaad.IndividualChannelStream) (*icsDecoded, error) {
	if stream == nil || stream.Ics_info == nil {
		return nil, malformedf("missing ICS info")
	}

	meta := readICSMeta(stream.Ics_info)
	if meta == nil {
		return nil, malformedf("building ICS metadata")
	}

	quantized, err := d.rebuildQuantizedSpectral(channel, stream, meta)
	if err != nil {
		return nil, err
	}

	scaleFactors := d.decodeScaleFactors(channel, stream, meta)
	globalGain := int(stream.Global_gain)
	if stream.Pulse_data_present {
		applyPulseData(quantized, meta, stream.Pulse_data)
	}
	spec := d.inverseQuantize(channel, quantized)
	applyScaleFactors(spec, meta, scaleFactors)
	if meta.windowSequence == windowEightShort {
		spec = d.reorderShortSpectral(channel, spec, meta)
	}

	return &icsDecoded{
		meta:         meta,
		globalGain:   globalGain,
		scaleFactors: scaleFactors,
		spec:         spec,
		currentShape: meta.windowShape,
		pulsePresent: stream.Pulse_data_present,
		pnsBands:     countCodebookBands(meta, gaad.NOISE_HCB),
		escBands:     countCodebookBands(meta, gaad.ESC_HCB),
		maxQuantized: maxAbsInt(quantized),
		tnsPresent:   stream.Tns_data_present,
		tnsData:      stream.Tns_data,
		tnsFilters:   countTNSFilters(stream.Tns_data),
	}, nil
}

func readICSMeta(info *gaad.ICSInfo) *icsMeta {
	if info == nil {
		return nil
	}
	return &icsMeta{
		windowSequence:    info.Window_sequence,
		windowShape:       info.Window_shape,
		maxSFB:            int(info.Max_sfb),
		numWindows:        info.NumWindows(),
		numWindowGroups:   info.NumWindowGroups(),
		windowGroupLength: info.WindowGroupLength(),
		sectSFBOffset:     info.SectSFBOffset(),
		swbOffset:         info.SWBOffset(),
		sfbCB:             info.SFBCB(),
	}
}

func (d *synthDecoder) decodeScaleFactors(channel int, stream *gaad.IndividualChannelStream, meta *icsMeta) [][]int {
	if stream == nil || stream.Scale_factor_data == nil {
		return nil
	}

	scaleData := stream.Scale_factor_data
	globalGain := int(stream.Global_gain)
	scaleFactor := globalGain
	noiseEnergy := globalGain - 90
	intensityPosition := 0
	noisePCM := true

	scratch := &d.scratch[channel]
	if cap(scratch.scaleFactors) < meta.numWindowGroups {
		scratch.scaleFactors = make([][]int, meta.numWindowGroups)
	}
	out := scratch.scaleFactors[:meta.numWindowGroups]
	for g := 0; g < meta.numWindowGroups; g++ {
		if cap(out[g]) < meta.maxSFB {
			out[g] = make([]int, meta.maxSFB)
		}
		out[g] = out[g][:meta.maxSFB]
		clear(out[g])
		sfRow := scaleData.Dcpm_sf[g]
		noiseRow := scaleData.Dcpm_noise_nrg[g]
		intensityRow := scaleData.Dcpm_is_position[g]
		for sfb := 0; sfb < meta.maxSFB; sfb++ {
			switch meta.sfbCB[g][sfb] {
			case gaad.ZERO_HCB:
			case gaad.INTENSITY_HCB, gaad.INTENSITY_HCB2:
				intensityPosition += int(intensityRow[sfb]) - 60
				out[g][sfb] = intensityPosition
			case gaad.NOISE_HCB:
				if noisePCM {
					noisePCM = false
					noiseEnergy += int(noiseRow[sfb]) - 256
				} else {
					noiseEnergy += int(noiseRow[sfb]) - 60
				}
				out[g][sfb] = noiseEnergy
			default:
				scaleFactor += int(sfRow[sfb]) - 60
				out[g][sfb] = scaleFactor
			}
		}
	}
	return out
}

func (d *synthDecoder) rebuildQuantizedSpectral(channel int, stream *gaad.IndividualChannelStream, meta *icsMeta) ([]int, error) {
	if stream == nil || stream.Spectral_data == nil {
		return nil, malformedf("missing spectral data")
	}
	hcod := stream.Spectral_data.Hcod

	spec := d.scratch[channel].quantized[:aacFrameSize]
	clear(spec)
	huffIndex := 0
	windowBase := 0
	for g := 0; g < meta.numWindowGroups; g++ {
		groupBase := 0
		if meta.windowSequence == windowEightShort {
			groupBase = windowBase * shortWindowLength
		}
		for sfb := 0; sfb < meta.maxSFB; sfb++ {
			cb := meta.sfbCB[g][sfb]
			if cb == gaad.ZERO_HCB || cb == gaad.NOISE_HCB || cb == gaad.INTENSITY_HCB || cb == gaad.INTENSITY_HCB2 {
				continue
			}

			start := int(meta.sectSFBOffset[g][sfb])
			end := int(meta.sectSFBOffset[g][sfb+1])
			step := 4
			if cb >= gaad.FIRST_PAIR_HCB {
				step = 2
			}

			for k := start; k < end; k += step {
				if huffIndex >= len(hcod) {
					return nil, fmt.Errorf("malformed spectral data")
				}
				row := hcod[huffIndex]
				huffIndex++
				for i := 0; i < int(row.Count); i++ {
					pos := groupBase + k + i
					if pos >= len(spec) {
						return nil, malformedf("spectral coefficient overflow")
					}
					spec[pos] = row.Values[i]
				}
			}
		}
		windowBase += int(meta.windowGroupLength[g])
	}

	return spec, nil
}

func (d *synthDecoder) inverseQuantize(channel int, quantized []int) []float64 {
	spec := d.scratch[channel].spec[:len(quantized)]
	for i, value := range quantized {
		spec[i] = pow43(value)
	}
	return spec
}

func applyScaleFactors(spec []float64, meta *icsMeta, scaleFactors [][]int) {
	windowBase := 0
	for g := 0; g < meta.numWindowGroups; g++ {
		groupBase := 0
		if meta.windowSequence == windowEightShort {
			groupBase = windowBase * shortWindowLength
		}
		for sfb := 0; sfb < meta.maxSFB; sfb++ {
			cb := meta.sfbCB[g][sfb]
			if cb == gaad.ZERO_HCB || cb == gaad.NOISE_HCB || cb == gaad.INTENSITY_HCB || cb == gaad.INTENSITY_HCB2 {
				continue
			}

			sf := scaleFactors[g][sfb]
			scale := math.Pow(2, 0.25*float64(sf-100))
			start := groupBase + int(meta.sectSFBOffset[g][sfb])
			end := groupBase + int(meta.sectSFBOffset[g][sfb+1])
			for i := start; i < end; i++ {
				spec[i] *= scale
			}
		}
		windowBase += int(meta.windowGroupLength[g])
	}
}

func applyPulseData(spec []int, meta *icsMeta, pulse *gaad.PulseData) {
	if pulse == nil || meta.windowSequence == windowEightShort {
		return
	}

	numberPulse := int(pulse.Number_pulse) + 1
	startSFB := int(pulse.Pulse_start_sfb)
	offsets := pulse.Pulse_offset
	amps := pulse.Pulse_amp
	if startSFB >= len(meta.swbOffset) {
		return
	}

	k := int(meta.swbOffset[startSFB])
	for i := 0; i < numberPulse && i < len(offsets) && i < len(amps); i++ {
		k += int(offsets[i])
		if k < 0 || k >= len(spec) {
			break
		}
		if spec[k] < 0 {
			spec[k] -= int(amps[i])
		} else {
			spec[k] += int(amps[i])
		}
	}
}

func (d *synthDecoder) reorderShortSpectral(channel int, spec []float64, meta *icsMeta) []float64 {
	out := d.scratch[channel].reorder[:len(spec)]
	clear(out)
	windowBase := 0
	for g := 0; g < meta.numWindowGroups; g++ {
		groupLen := int(meta.windowGroupLength[g])
		groupBase := windowBase * shortWindowLength
		srcOffset := 0
		for sfb := 0; sfb < meta.maxSFB; sfb++ {
			start := int(meta.swbOffset[sfb])
			end := int(meta.swbOffset[sfb+1])
			width := end - start
			for w := 0; w < groupLen; w++ {
				dst := (windowBase+w)*shortWindowLength + start
				// Each grouped short-window payload is stored on a full
				// groupLen*128 stride even when maxSFB stops before the tail.
				// Advancing from the group's base keeps later groups aligned.
				src := groupBase + srcOffset
				copy(out[dst:dst+width], spec[src:src+width])
				srcOffset += width
			}
		}
		windowBase += groupLen
	}
	return out
}

func reorderShortSpectral(spec []float64, meta *icsMeta) []float64 {
	out := make([]float64, len(spec))
	windowBase := 0
	for g := 0; g < meta.numWindowGroups; g++ {
		groupLen := int(meta.windowGroupLength[g])
		groupBase := windowBase * shortWindowLength
		srcOffset := 0
		for sfb := 0; sfb < meta.maxSFB; sfb++ {
			start := int(meta.swbOffset[sfb])
			end := int(meta.swbOffset[sfb+1])
			width := end - start
			for w := 0; w < groupLen; w++ {
				dst := (windowBase+w)*shortWindowLength + start
				src := groupBase + srcOffset
				copy(out[dst:dst+width], spec[src:src+width])
				srcOffset += width
			}
		}
		windowBase += groupLen
	}
	return out
}

func (d *synthDecoder) applyPNSMono(ch *icsDecoded) {
	windowBase := 0
	for g := 0; g < ch.meta.numWindowGroups; g++ {
		groupLen := int(ch.meta.windowGroupLength[g])
		for sfb := 0; sfb < ch.meta.maxSFB; sfb++ {
			if ch.meta.sfbCB[g][sfb] != gaad.NOISE_HCB {
				continue
			}
			for w := 0; w < groupLen; w++ {
				start, end := ch.meta.bandRange(windowBase+w, sfb)
				noise := d.generateNoise(0, end-start)
				scaleNoise(ch.spec[start:end], noise, ch.scaleFactors[g][sfb])
			}
		}
		windowBase += groupLen
	}
}

func (d *synthDecoder) applyPNSPair(left, right *icsDecoded, msUsed [][]bool) {
	windowBase := 0
	for g := 0; g < left.meta.numWindowGroups; g++ {
		groupLen := int(left.meta.windowGroupLength[g])
		for sfb := 0; sfb < left.meta.maxSFB; sfb++ {
			leftNoise := left.meta.sfbCB[g][sfb] == gaad.NOISE_HCB
			rightNoise := right.meta.sfbCB[g][sfb] == gaad.NOISE_HCB
			if !leftNoise && !rightNoise {
				continue
			}

			useSame := leftNoise && rightNoise && msBandUsed(msUsed, g, sfb)
			for w := 0; w < groupLen; w++ {
				if leftNoise {
					start, end := left.meta.bandRange(windowBase+w, sfb)
					noise := d.generateNoise(0, end-start)
					scaleNoise(left.spec[start:end], noise, left.scaleFactors[g][sfb])
					if useSame {
						rStart, rEnd := right.meta.bandRange(windowBase+w, sfb)
						scaleNoise(right.spec[rStart:rEnd], noise, right.scaleFactors[g][sfb])
						continue
					}
				}
				if rightNoise {
					rStart, rEnd := right.meta.bandRange(windowBase+w, sfb)
					noise := d.generateNoise(1, rEnd-rStart)
					scaleNoise(right.spec[rStart:rEnd], noise, right.scaleFactors[g][sfb])
				}
			}
		}
		windowBase += groupLen
	}
}

func (d *synthDecoder) applyMS(left, right *icsDecoded, msUsed [][]bool) {
	windowBase := 0
	for g := 0; g < left.meta.numWindowGroups; g++ {
		groupLen := int(left.meta.windowGroupLength[g])
		for sfb := 0; sfb < left.meta.maxSFB; sfb++ {
			if !msBandUsed(msUsed, g, sfb) {
				continue
			}
			lcb := left.meta.sfbCB[g][sfb]
			rcb := right.meta.sfbCB[g][sfb]
			if lcb == gaad.ZERO_HCB || lcb == gaad.NOISE_HCB || lcb == gaad.INTENSITY_HCB || lcb == gaad.INTENSITY_HCB2 {
				continue
			}
			if rcb == gaad.ZERO_HCB || rcb == gaad.NOISE_HCB || rcb == gaad.INTENSITY_HCB || rcb == gaad.INTENSITY_HCB2 {
				continue
			}

			for w := 0; w < groupLen; w++ {
				lStart, lEnd := left.meta.bandRange(windowBase+w, sfb)
				rStart, _ := right.meta.bandRange(windowBase+w, sfb)
				for i := 0; i < lEnd-lStart; i++ {
					l := left.spec[lStart+i]
					r := right.spec[rStart+i]
					left.spec[lStart+i] = l + r
					right.spec[rStart+i] = l - r
				}
			}
		}
		windowBase += groupLen
	}
}

func (d *synthDecoder) applyIntensity(left, right *icsDecoded, msUsed [][]bool) {
	windowBase := 0
	for g := 0; g < right.meta.numWindowGroups; g++ {
		groupLen := int(right.meta.windowGroupLength[g])
		for sfb := 0; sfb < right.meta.maxSFB; sfb++ {
			cb := right.meta.sfbCB[g][sfb]
			if cb != gaad.INTENSITY_HCB && cb != gaad.INTENSITY_HCB2 {
				continue
			}

			sign := 1.0
			if cb == gaad.INTENSITY_HCB2 {
				sign = -1
			}
			if msBandUsed(msUsed, g, sfb) {
				sign = -sign
			}
			scale := sign * math.Pow(0.5, 0.25*float64(right.scaleFactors[g][sfb]))

			for w := 0; w < groupLen; w++ {
				lStart, lEnd := left.meta.bandRange(windowBase+w, sfb)
				rStart, _ := right.meta.bandRange(windowBase+w, sfb)
				for i := 0; i < lEnd-lStart; i++ {
					right.spec[rStart+i] = left.spec[lStart+i] * scale
				}
			}
		}
		windowBase += groupLen
	}
}

func (d *synthDecoder) applyTNS(ch *icsDecoded) error {
	return d.applyTNSTools(ch)
}

func (d *synthDecoder) generateNoise(channel, width int) []float64 {
	if width <= 0 {
		return nil
	}
	noise := d.scratch[channel].noise
	if cap(noise) < width {
		noise = make([]float64, width)
		d.scratch[channel].noise = noise
	}
	noise = noise[:width]
	energy := 0.0
	for i := range noise {
		d.noiseSeed = d.noiseSeed*1664525 + 1013904223
		value := float64(int32(d.noiseSeed)) / float64(math.MaxInt32)
		noise[i] = value
		energy += value * value
	}
	if energy == 0 {
		return noise
	}
	inv := 1.0 / math.Sqrt(energy)
	for i := range noise {
		noise[i] *= inv
	}
	return noise
}

func scaleNoise(dst, noise []float64, sf int) {
	scale := math.Pow(2, 0.25*float64(sf))
	for i := range dst {
		dst[i] = noise[i] * scale
	}
}

func msBandUsed(msUsed [][]bool, group, sfb int) bool {
	if group < 0 || group >= len(msUsed) {
		return false
	}
	if sfb < 0 || sfb >= len(msUsed[group]) {
		return false
	}
	return msUsed[group][sfb]
}

func (m *icsMeta) bandRange(windowIndex, sfb int) (int, int) {
	if m.windowSequence == windowEightShort {
		base := windowIndex * shortWindowLength
		return base + int(m.swbOffset[sfb]), base + int(m.swbOffset[sfb+1])
	}
	return int(m.swbOffset[sfb]), int(m.swbOffset[sfb+1])
}

func floatToPCM16(sample float64) int16 {
	if sample >= 32767 {
		return 32767
	}
	if sample <= -32768 {
		return -32768
	}
	if sample >= 0 {
		return int16(sample + 0.5)
	}
	return int16(sample - 0.5)
}

func (d *synthDecoder) emitTrace(src *containerSource, unitIndex, rawStart, pcmFrames int, trace FrameTrace) {
	if d.trace == nil {
		return
	}

	visibleStart := rawStart - src.leading
	if visibleStart < 0 {
		visibleStart = 0
	}
	visibleFrames := pcmFrames
	if trim := src.leading - rawStart; trim > 0 {
		if trim >= visibleFrames {
			visibleFrames = 0
		} else {
			visibleFrames -= trim
		}
	}

	trace.AUIndex = unitIndex
	trace.PCMStartFrame = int64(visibleStart)
	trace.PCMFrames = visibleFrames
	d.trace.OnFrame(trace)
}

func baseTrace(meta *icsMeta) FrameTrace {
	if meta == nil {
		return FrameTrace{}
	}
	return FrameTrace{
		WindowSequence:  meta.windowSequence,
		WindowShape:     meta.windowShape,
		NumWindows:      meta.numWindows,
		NumWindowGroups: meta.numWindowGroups,
		MaxSFB:          meta.maxSFB,
	}
}

func countCodebookBands(meta *icsMeta, targets ...uint8) int {
	if meta == nil {
		return 0
	}
	count := 0
	for g := 0; g < meta.numWindowGroups; g++ {
		for sfb := 0; sfb < meta.maxSFB; sfb++ {
			for _, target := range targets {
				if meta.sfbCB[g][sfb] == target {
					count++
					break
				}
			}
		}
	}
	return count
}

func countIntensityBands(meta *icsMeta) int {
	return countCodebookBands(meta, gaad.INTENSITY_HCB, gaad.INTENSITY_HCB2)
}

func countMSBands(msUsed [][]bool) int {
	count := 0
	for _, row := range msUsed {
		for _, used := range row {
			if used {
				count++
			}
		}
	}
	return count
}

func countTNSFilters(data *gaad.TNSData) int {
	if data == nil {
		return 0
	}
	total := 0
	for _, n := range data.N_filt {
		total += int(n)
	}
	return total
}

func peakAbs(values []float64) float64 {
	peak := 0.0
	for _, value := range values {
		if abs := math.Abs(value); abs > peak {
			peak = abs
		}
	}
	return peak
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxAbsInt(values []int) int {
	peak := 0
	for _, value := range values {
		if value < 0 {
			value = -value
		}
		if value > peak {
			peak = value
		}
	}
	return peak
}
