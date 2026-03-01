package aacfile

import (
	"fmt"
	"math"

	"github.com/Comcast/gaad"
)

type channelState struct {
	overlap         []float64
	prevWindowShape uint8
}

type synthDecoder struct {
	cfg       ascConfig
	state     []channelState
	noiseSeed uint32
	trace     TraceSink

	longIMDCTWork  imdctWork
	shortIMDCTWork imdctWork
}

type icsMeta struct {
	windowSequence    uint8
	windowShape       uint8
	maxSFB            int
	numWindows        int
	numWindowGroups   int
	windowGroupLength []int
	sectSFBOffset     [][]int
	swbOffset         []int
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
	tnsData      *tnsFilterData
	tnsFilters   int
}

type tnsFilterData struct {
	nFilt        []int
	coefRes      []int
	length       [][]int
	order        [][]int
	direction    [][]bool
	coefCompress [][]int
	coef         [][][]uint8
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
	for i := range state {
		state[i] = channelState{
			overlap: make([]float64, longWindowLength),
		}
	}
	return &synthDecoder{
		cfg:            cfg,
		state:          state,
		noiseSeed:      1,
		trace:          trace,
		longIMDCTWork:  newIMDCTWork(longWindowLength),
		shortIMDCTWork: newIMDCTWork(shortWindowLength),
	}
}

func (d *synthDecoder) decodeAccessUnit(src *containerSource, payload []byte, unitIndex, rawStart, pcmFrames int) ([]float64, error) {
	frame := payload
	if src.container != ".aac" {
		frame = makeADTSFrame(src.asc, payload)
		if frame == nil {
			return nil, fmt.Errorf("building synthetic ADTS frame")
		}
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

func (d *synthDecoder) decodeSCE(sce any) ([]float64, FrameTrace, error) {
	stream := fieldAny(sce, "Channel_stream")
	decoded, err := d.buildICSDecoded(stream)
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
	out := make([]float64, len(pcm))
	copy(out, pcm)
	return out, trace, nil
}

func (d *synthDecoder) decodeCPE(cpe any) ([]float64, FrameTrace, error) {
	leftStream := fieldAny(cpe, "Channel_stream1")
	rightStream := fieldAny(cpe, "Channel_stream2")

	left, err := d.buildICSDecoded(leftStream)
	if err != nil {
		return nil, FrameTrace{}, err
	}
	right, err := d.buildICSDecoded(rightStream)
	if err != nil {
		return nil, FrameTrace{}, err
	}

	msUsed := boolMatrix(fieldValue(cpe, "Ms_used"))
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

	out := make([]float64, len(leftPCM)*2)
	for i := range leftPCM {
		out[i*2] = leftPCM[i]
		out[i*2+1] = rightPCM[i]
	}
	return out, trace, nil
}

func (d *synthDecoder) buildICSDecoded(stream any) (*icsDecoded, error) {
	info := fieldAny(stream, "Ics_info")
	if info == nil {
		return nil, malformedf("missing ICS info")
	}

	meta := readICSMeta(info)
	if meta == nil {
		return nil, malformedf("building ICS metadata")
	}

	quantized, err := rebuildQuantizedSpectral(stream, meta)
	if err != nil {
		return nil, err
	}

	scaleFactors := decodeScaleFactors(stream, meta)
	globalGain := int(uint8Field(stream, "Global_gain"))
	if boolField(stream, "Pulse_data_present") {
		applyPulseData(quantized, meta, fieldAny(stream, "Pulse_data"))
	}
	spec := inverseQuantize(quantized)
	applyScaleFactors(spec, meta, scaleFactors)
	if meta.windowSequence == windowEightShort {
		spec = reorderShortSpectral(spec, meta)
	}

	return &icsDecoded{
		meta:         meta,
		globalGain:   globalGain,
		scaleFactors: scaleFactors,
		spec:         spec,
		currentShape: meta.windowShape,
		pulsePresent: boolField(stream, "Pulse_data_present"),
		pnsBands:     countCodebookBands(meta, gaad.NOISE_HCB),
		escBands:     countCodebookBands(meta, gaad.ESC_HCB),
		maxQuantized: maxAbsInt(quantized),
		tnsPresent:   boolField(stream, "Tns_data_present"),
		tnsData:      readTNSData(fieldAny(stream, "Tns_data")),
		tnsFilters:   countTNSFilters(fieldAny(stream, "Tns_data")),
	}, nil
}

func readICSMeta(info any) *icsMeta {
	return &icsMeta{
		windowSequence:    uint8Field(info, "Window_sequence"),
		windowShape:       uint8Field(info, "Window_shape"),
		maxSFB:            int(uint8Field(info, "Max_sfb")),
		numWindows:        int(fieldValue(info, "num_windows").Uint()),
		numWindowGroups:   int(fieldValue(info, "num_window_groups").Uint()),
		windowGroupLength: intSlice(fieldValue(info, "window_group_length")),
		sectSFBOffset:     uint16Matrix(fieldValue(info, "sect_sfb_offset")),
		swbOffset:         uint16Slice(fieldValue(info, "swb_offset")),
		sfbCB:             uint8Matrix(fieldValue(info, "sfb_cb")),
	}
}

func decodeScaleFactors(stream any, meta *icsMeta) [][]int {
	scaleData := fieldAny(stream, "Scale_factor_data")
	globalGain := int(uint8Field(stream, "Global_gain"))
	scaleFactor := globalGain
	noiseEnergy := globalGain - 90
	intensityPosition := 0
	noisePCM := true

	dcpmSF := uint8Matrix(fieldValue(scaleData, "Dcpm_sf"))
	dcpmNoise := uint16Matrix(fieldValue(scaleData, "Dcpm_noise_nrg"))
	dcpmIntensity := uint8Matrix(fieldValue(scaleData, "Dcpm_is_position"))

	out := make([][]int, meta.numWindowGroups)
	for g := 0; g < meta.numWindowGroups; g++ {
		out[g] = make([]int, meta.maxSFB)
		for sfb := 0; sfb < meta.maxSFB; sfb++ {
			switch meta.sfbCB[g][sfb] {
			case gaad.ZERO_HCB:
			case gaad.INTENSITY_HCB, gaad.INTENSITY_HCB2:
				intensityPosition += int(dcpmIntensity[g][sfb]) - 60
				out[g][sfb] = intensityPosition
			case gaad.NOISE_HCB:
				if noisePCM {
					noisePCM = false
					noiseEnergy += dcpmNoise[g][sfb] - 256
				} else {
					noiseEnergy += dcpmNoise[g][sfb] - 60
				}
				out[g][sfb] = noiseEnergy
			default:
				scaleFactor += int(dcpmSF[g][sfb]) - 60
				out[g][sfb] = scaleFactor
			}
		}
	}
	return out
}

func rebuildQuantizedSpectral(stream any, meta *icsMeta) ([]int, error) {
	specData := fieldAny(stream, "Spectral_data")
	hcod := int8Matrix(fieldValue(specData, "Hcod"))

	spec := make([]int, aacFrameSize)
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

			start := meta.sectSFBOffset[g][sfb]
			end := meta.sectSFBOffset[g][sfb+1]
			step := 4
			if cb >= gaad.FIRST_PAIR_HCB {
				step = 2
			}

			for k := start; k < end; k += step {
				if huffIndex >= len(hcod) {
					return nil, fmt.Errorf("malformed spectral data")
				}
				values := hcod[huffIndex]
				huffIndex++
				for i, value := range values {
					pos := groupBase + k + i
					if pos >= len(spec) {
						return nil, malformedf("spectral coefficient overflow")
					}
					spec[pos] = value
				}
			}
		}
		windowBase += meta.windowGroupLength[g]
	}

	return spec, nil
}

func inverseQuantize(quantized []int) []float64 {
	spec := make([]float64, len(quantized))
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
			start := groupBase + meta.sectSFBOffset[g][sfb]
			end := groupBase + meta.sectSFBOffset[g][sfb+1]
			for i := start; i < end; i++ {
				spec[i] *= scale
			}
		}
		windowBase += meta.windowGroupLength[g]
	}
}

func applyPulseData(spec []int, meta *icsMeta, pulse any) {
	if pulse == nil || meta.windowSequence == windowEightShort {
		return
	}

	numberPulse := int(uint8Field(pulse, "Number_pulse")) + 1
	startSFB := int(uint8Field(pulse, "Pulse_start_sfb"))
	offsets := uint8Slice(fieldValue(pulse, "Pulse_offset"))
	amps := uint8Slice(fieldValue(pulse, "Pulse_amp"))
	if startSFB >= len(meta.swbOffset) {
		return
	}

	k := meta.swbOffset[startSFB]
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

func reorderShortSpectral(spec []float64, meta *icsMeta) []float64 {
	out := make([]float64, len(spec))
	windowBase := 0
	for g := 0; g < meta.numWindowGroups; g++ {
		groupLen := meta.windowGroupLength[g]
		groupBase := windowBase * shortWindowLength
		srcOffset := 0
		for sfb := 0; sfb < meta.maxSFB; sfb++ {
			start := meta.swbOffset[sfb]
			end := meta.swbOffset[sfb+1]
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

func (d *synthDecoder) applyPNSMono(ch *icsDecoded) {
	windowBase := 0
	for g := 0; g < ch.meta.numWindowGroups; g++ {
		for sfb := 0; sfb < ch.meta.maxSFB; sfb++ {
			if ch.meta.sfbCB[g][sfb] != gaad.NOISE_HCB {
				continue
			}
			for w := 0; w < ch.meta.windowGroupLength[g]; w++ {
				start, end := ch.meta.bandRange(windowBase+w, sfb)
				noise := d.generateNoise(end - start)
				scaleNoise(ch.spec[start:end], noise, ch.scaleFactors[g][sfb])
			}
		}
		windowBase += ch.meta.windowGroupLength[g]
	}
}

func (d *synthDecoder) applyPNSPair(left, right *icsDecoded, msUsed [][]bool) {
	windowBase := 0
	for g := 0; g < left.meta.numWindowGroups; g++ {
		for sfb := 0; sfb < left.meta.maxSFB; sfb++ {
			leftNoise := left.meta.sfbCB[g][sfb] == gaad.NOISE_HCB
			rightNoise := right.meta.sfbCB[g][sfb] == gaad.NOISE_HCB
			if !leftNoise && !rightNoise {
				continue
			}

			useSame := leftNoise && rightNoise && msBandUsed(msUsed, g, sfb)
			for w := 0; w < left.meta.windowGroupLength[g]; w++ {
				if leftNoise {
					start, end := left.meta.bandRange(windowBase+w, sfb)
					noise := d.generateNoise(end - start)
					scaleNoise(left.spec[start:end], noise, left.scaleFactors[g][sfb])
					if useSame {
						rStart, rEnd := right.meta.bandRange(windowBase+w, sfb)
						scaleNoise(right.spec[rStart:rEnd], noise, right.scaleFactors[g][sfb])
						continue
					}
				}
				if rightNoise {
					rStart, rEnd := right.meta.bandRange(windowBase+w, sfb)
					noise := d.generateNoise(rEnd - rStart)
					scaleNoise(right.spec[rStart:rEnd], noise, right.scaleFactors[g][sfb])
				}
			}
		}
		windowBase += left.meta.windowGroupLength[g]
	}
}

func (d *synthDecoder) applyMS(left, right *icsDecoded, msUsed [][]bool) {
	windowBase := 0
	for g := 0; g < left.meta.numWindowGroups; g++ {
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

			for w := 0; w < left.meta.windowGroupLength[g]; w++ {
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
		windowBase += left.meta.windowGroupLength[g]
	}
}

func (d *synthDecoder) applyIntensity(left, right *icsDecoded, msUsed [][]bool) {
	windowBase := 0
	for g := 0; g < right.meta.numWindowGroups; g++ {
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

			for w := 0; w < right.meta.windowGroupLength[g]; w++ {
				lStart, lEnd := left.meta.bandRange(windowBase+w, sfb)
				rStart, _ := right.meta.bandRange(windowBase+w, sfb)
				for i := 0; i < lEnd-lStart; i++ {
					right.spec[rStart+i] = left.spec[lStart+i] * scale
				}
			}
		}
		windowBase += right.meta.windowGroupLength[g]
	}
}

func (d *synthDecoder) applyTNS(ch *icsDecoded) error {
	return d.applyTNSTools(ch)
}

func (d *synthDecoder) generateNoise(width int) []float64 {
	noise := make([]float64, width)
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
		return base + m.swbOffset[sfb], base + m.swbOffset[sfb+1]
	}
	return m.swbOffset[sfb], m.swbOffset[sfb+1]
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

func countTNSFilters(data any) int {
	nFilt := intSlice(fieldValue(data, "N_filt"))
	total := 0
	for _, n := range nFilt {
		total += n
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

func readTNSData(data any) *tnsFilterData {
	nFilt := intSlice(fieldValue(data, "N_filt"))
	coefRes := intSlice(fieldValue(data, "Coef_res"))
	length := uint8Matrix(fieldValue(data, "Len"))
	order := uint8Matrix(fieldValue(data, "Order"))
	direction := boolMatrix(fieldValue(data, "Direction"))
	coefCompress := uint8Matrix(fieldValue(data, "Coef_compress"))
	coef := uint8Cube(fieldValue(data, "Coef"))

	if len(nFilt) == 0 {
		return nil
	}

	out := &tnsFilterData{
		nFilt:        nFilt,
		coefRes:      coefRes,
		length:       make([][]int, len(length)),
		order:        make([][]int, len(order)),
		direction:    direction,
		coefCompress: make([][]int, len(coefCompress)),
		coef:         coef,
	}
	for i := range length {
		out.length[i] = make([]int, len(length[i]))
		for j := range length[i] {
			out.length[i][j] = int(length[i][j])
		}
	}
	for i := range order {
		out.order[i] = make([]int, len(order[i]))
		for j := range order[i] {
			out.order[i][j] = int(order[i][j])
		}
	}
	for i := range coefCompress {
		out.coefCompress[i] = make([]int, len(coefCompress[i]))
		for j := range coefCompress[i] {
			out.coefCompress[i][j] = int(coefCompress[i][j])
		}
	}
	return out
}
