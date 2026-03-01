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
	scaleFactors [][]int
	spec         []float64
	currentShape uint8
	tnsPresent   bool
	tnsData      any
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

func newSynthDecoder(cfg ascConfig) *synthDecoder {
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
		longIMDCTWork:  newIMDCTWork(longWindowLength),
		shortIMDCTWork: newIMDCTWork(shortWindowLength),
	}
}

func (d *synthDecoder) decodeAccessUnit(src *containerSource, payload []byte) ([]float64, error) {
	frame := payload
	if src.container != ".aac" {
		frame = makeADTSFrame(src.asc, payload)
		if frame == nil {
			return nil, fmt.Errorf("building synthetic ADTS frame")
		}
	}

	adts, err := gaad.ParseADTS(frame)
	if err != nil {
		return nil, err
	}

	switch {
	case len(adts.Channel_pair_elements) == 1:
		if src.cfg.channelConfig != 2 {
			return nil, fmt.Errorf("unexpected channel pair element for %d-channel stream", src.cfg.channelConfig)
		}
		return d.decodeCPE(adts.Channel_pair_elements[0])
	case len(adts.Single_channel_elements) == 1:
		if src.cfg.channelConfig != 1 {
			return nil, fmt.Errorf("unexpected single channel element for %d-channel stream", src.cfg.channelConfig)
		}
		return d.decodeSCE(adts.Single_channel_elements[0])
	default:
		return nil, fmt.Errorf("unsupported AAC element layout")
	}
}

func (d *synthDecoder) decodeSCE(sce any) ([]float64, error) {
	stream := fieldAny(sce, "Channel_stream")
	decoded, err := d.buildICSDecoded(stream)
	if err != nil {
		return nil, err
	}

	d.applyPNSMono(decoded)
	d.applyTNS(decoded)
	pcm := d.synthChannel(0, decoded.meta, decoded.spec, decoded.currentShape)
	out := make([]float64, len(pcm))
	copy(out, pcm)
	return out, nil
}

func (d *synthDecoder) decodeCPE(cpe any) ([]float64, error) {
	leftStream := fieldAny(cpe, "Channel_stream1")
	rightStream := fieldAny(cpe, "Channel_stream2")

	left, err := d.buildICSDecoded(leftStream)
	if err != nil {
		return nil, err
	}
	right, err := d.buildICSDecoded(rightStream)
	if err != nil {
		return nil, err
	}

	msUsed := boolMatrix(fieldValue(cpe, "Ms_used"))
	d.applyPNSPair(left, right, msUsed)
	d.applyMS(left, right, msUsed)
	d.applyIntensity(left, right, msUsed)
	d.applyTNS(left)
	d.applyTNS(right)

	leftPCM := d.synthChannel(0, left.meta, left.spec, left.currentShape)
	rightPCM := d.synthChannel(1, right.meta, right.spec, right.currentShape)

	out := make([]float64, len(leftPCM)*2)
	for i := range leftPCM {
		out[i*2] = leftPCM[i]
		out[i*2+1] = rightPCM[i]
	}
	return out, nil
}

func (d *synthDecoder) buildICSDecoded(stream any) (*icsDecoded, error) {
	info := fieldAny(stream, "Ics_info")
	if info == nil {
		return nil, fmt.Errorf("missing ICS info")
	}

	meta := readICSMeta(info)
	if meta == nil {
		return nil, fmt.Errorf("building ICS metadata")
	}

	spec, err := rebuildSpectral(stream, meta)
	if err != nil {
		return nil, err
	}

	scaleFactors := decodeScaleFactors(stream, meta)
	applyScaleFactors(spec, meta, scaleFactors)
	if boolField(stream, "Pulse_data_present") {
		applyPulseData(spec, meta, fieldAny(stream, "Pulse_data"))
	}
	if meta.windowSequence == windowEightShort {
		spec = reorderShortSpectral(spec, meta)
	}

	return &icsDecoded{
		meta:         meta,
		scaleFactors: scaleFactors,
		spec:         spec,
		currentShape: meta.windowShape,
		tnsPresent:   boolField(stream, "Tns_data_present"),
		tnsData:      fieldAny(stream, "Tns_data"),
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

func rebuildSpectral(stream any, meta *icsMeta) ([]float64, error) {
	specData := fieldAny(stream, "Spectral_data")
	hcod := int8Matrix(fieldValue(specData, "Hcod"))

	spec := make([]float64, aacFrameSize)
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
						return nil, fmt.Errorf("spectral coefficient overflow")
					}
					spec[pos] = pow43(value)
				}
			}
		}
		windowBase += meta.windowGroupLength[g]
	}

	return spec, nil
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

			scale := math.Pow(2, 0.25*float64(scaleFactors[g][sfb]-100))
			start := groupBase + meta.sectSFBOffset[g][sfb]
			end := groupBase + meta.sectSFBOffset[g][sfb+1]
			for i := start; i < end; i++ {
				spec[i] *= scale
			}
		}
		windowBase += meta.windowGroupLength[g]
	}
}

func applyPulseData(spec []float64, meta *icsMeta, pulse any) {
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
		amp := float64(amps[i])
		if spec[k] < 0 {
			spec[k] -= amp
		} else {
			spec[k] += amp
		}
	}
}

func reorderShortSpectral(spec []float64, meta *icsMeta) []float64 {
	out := make([]float64, len(spec))
	windowBase := 0
	srcBase := 0
	for g := 0; g < meta.numWindowGroups; g++ {
		groupLen := meta.windowGroupLength[g]
		for sfb := 0; sfb < meta.maxSFB; sfb++ {
			start := meta.swbOffset[sfb]
			end := meta.swbOffset[sfb+1]
			width := end - start
			for w := 0; w < groupLen; w++ {
				dst := (windowBase+w)*shortWindowLength + start
				copy(out[dst:dst+width], spec[srcBase:srcBase+width])
				srcBase += width
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

func (d *synthDecoder) applyTNS(ch *icsDecoded) {
	if !ch.tnsPresent || ch.tnsData == nil {
		return
	}

	tns := readTNSData(ch.tnsData)
	if tns == nil {
		return
	}

	windowCount := ch.meta.numWindows
	if windowCount <= 0 {
		windowCount = 1
	}
	maxBand := ch.meta.maxSFB
	if len(ch.meta.swbOffset) > 0 && maxBand > len(ch.meta.swbOffset)-1 {
		maxBand = len(ch.meta.swbOffset) - 1
	}
	for w := 0; w < windowCount && w < len(tns.nFilt); w++ {
		topBand := maxBand
		for filt := 0; filt < tns.nFilt[w] && filt < len(tns.length[w]) && filt < len(tns.order[w]); filt++ {
			order := tns.order[w][filt]
			if order <= 0 {
				continue
			}

			bottomBand := topBand - tns.length[w][filt]
			if bottomBand < 0 {
				bottomBand = 0
			}
			if bottomBand >= topBand {
				topBand = bottomBand
				continue
			}

			start := ch.meta.swbOffset[bottomBand]
			end := ch.meta.swbOffset[topBand]
			if ch.meta.windowSequence == windowEightShort {
				base := w * shortWindowLength
				start += base
				end += base
			}
			if start < 0 {
				start = 0
			}
			if end > len(ch.spec) {
				end = len(ch.spec)
			}
			if start >= end {
				topBand = bottomBand
				continue
			}

			coefs := tns.coefficients(w, filt, order)
			if len(coefs) == 0 {
				topBand = bottomBand
				continue
			}
			lpc := reflectionToLPC(coefs)
			if len(lpc) == 0 {
				topBand = bottomBand
				continue
			}

			filtered := ch.spec[start:end]
			if tns.direction[w][filt] {
				applyARFilterReverse(filtered, lpc)
			} else {
				applyARFilterForward(filtered, lpc)
			}

			topBand = bottomBand
		}
	}
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
	scale := math.Pow(2, 0.25*float64(sf-100))
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
	if sample >= 1 {
		return 32767
	}
	if sample <= -1 {
		return -32768
	}
	return int16(sample * 32767)
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

func (t *tnsFilterData) coefficients(window, filt, order int) []float64 {
	if t == nil || window < 0 || window >= len(t.coef) || filt < 0 || filt >= len(t.coef[window]) {
		return nil
	}
	resolution := 3
	if window < len(t.coefRes) && t.coefRes[window] > 0 {
		resolution = 4
	}
	compress := 0
	if window < len(t.coefCompress) && filt < len(t.coefCompress[window]) {
		compress = t.coefCompress[window][filt]
	}
	coefBits := resolution - compress
	if coefBits < 2 {
		coefBits = 2
	}

	raw := t.coef[window][filt]
	if order > len(raw) {
		order = len(raw)
	}
	out := make([]float64, order)
	for i := 0; i < order; i++ {
		out[i] = tnsQuantToReflection(raw[i], coefBits)
	}
	return out
}

func tnsQuantToReflection(raw uint8, coefBits int) float64 {
	mask := (1 << coefBits) - 1
	value := int(raw) & mask
	signBit := 1 << (coefBits - 1)
	if value&signBit != 0 {
		value -= 1 << coefBits
	}
	scale := float64(signBit)
	if scale == 0 {
		return 0
	}
	reflection := float64(value) / scale
	if reflection >= 1 {
		reflection = 0.999
	}
	if reflection <= -1 {
		reflection = -0.999
	}
	return reflection
}

func reflectionToLPC(reflection []float64) []float64 {
	lpc := make([]float64, 0, len(reflection))
	for _, rc := range reflection {
		m := len(lpc)
		next := make([]float64, m+1)
		next[m] = rc
		for i := 0; i < m; i++ {
			next[i] = lpc[i] + rc*lpc[m-1-i]
		}
		lpc = next
	}
	return lpc
}

func applyARFilterForward(spec []float64, lpc []float64) {
	state := make([]float64, len(lpc))
	for i := 0; i < len(spec); i++ {
		y := spec[i]
		for j := range lpc {
			y -= lpc[j] * state[j]
		}
		for j := len(state) - 1; j > 0; j-- {
			state[j] = state[j-1]
		}
		if len(state) > 0 {
			state[0] = y
		}
		spec[i] = y
	}
}

func applyARFilterReverse(spec []float64, lpc []float64) {
	state := make([]float64, len(lpc))
	for i := len(spec) - 1; i >= 0; i-- {
		y := spec[i]
		for j := range lpc {
			y -= lpc[j] * state[j]
		}
		for j := len(state) - 1; j > 0; j-- {
			state[j] = state[j-1]
		}
		if len(state) > 0 {
			state[0] = y
		}
		spec[i] = y
	}
}
