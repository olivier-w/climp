package aacfile

import (
	"fmt"
	"math"

	"github.com/olivier-w/climp-aac-decoder/third_party/gaad"
)

func (d *synthDecoder) applyTNSTools(ch *icsDecoded) error {
	if ch == nil || !ch.tnsPresent || ch.tnsData == nil {
		return nil
	}

	short := ch.meta.windowSequence == windowEightShort
	if short {
		return nil
	}
	maxTNSBands := tnsMaxSFB(d.cfg.sampleRateIndex, short)
	if maxTNSBands <= 0 {
		return unvalidatedFeature("AAC TNS sample rate", fmt.Sprintf("%d", d.cfg.sampleRateIndex))
	}

	swbLimit := len(ch.meta.swbOffset) - 1
	if swbLimit <= 0 {
		return malformedf("invalid TNS scalefactor band layout")
	}

	for w := 0; w < ch.meta.numWindows; w++ {
		bottomBand := swbLimit
		filterCount := tnsFilterCountForWindow(ch.tnsData, w)
		for filt := 0; filt < filterCount; filt++ {
			topBand := bottomBand
			length := tnsFilterLength(ch.tnsData, w, filt)
			order := tnsFilterOrder(ch.tnsData, w, filt)
			bottomBand -= length
			if bottomBand < 0 {
				bottomBand = 0
			}
			if order == 0 {
				continue
			}

			coeffs, err := d.tnsCoefficients(ch.tnsData, w, filt, order)
			if err != nil {
				return err
			}
			lpc := d.reflectionToPredictor(coeffs)
			startBand := minInt3(bottomBand, maxTNSBands, ch.meta.maxSFB)
			endBand := minInt3(topBand, maxTNSBands, ch.meta.maxSFB)
			if startBand < 0 || endBand < startBand || endBand > swbLimit {
				return malformedf("invalid TNS band range")
			}

			startLine := int(ch.meta.swbOffset[startBand])
			endLine := int(ch.meta.swbOffset[endBand])
			if endLine < startLine {
				return malformedf("invalid TNS spectral line range")
			}

			windowBase := 0
			if short {
				windowBase = w * shortWindowLength
			}
			start := windowBase + startLine
			end := windowBase + endLine
			if start < 0 || end < start || end > len(ch.spec) {
				return malformedf("invalid TNS spectral bounds")
			}
			if end == start {
				continue
			}

			index := start
			step := 1
			if tnsFilterDirection(ch.tnsData, w, filt) {
				index = end - 1
				step = -1
			}
			if err := d.applyTNSARFilter(ch.spec, index, end-start, step, lpc); err != nil {
				return err
			}
		}
	}
	return nil
}

func tnsMaxSFB(sampleRateIndex int, short bool) int {
	if sampleRateIndex < 0 {
		return 0
	}
	if short {
		if sampleRateIndex >= len(tnsMaxBandsShort) {
			return 0
		}
		return tnsMaxBandsShort[sampleRateIndex]
	}
	if sampleRateIndex >= len(tnsMaxBandsLong) {
		return 0
	}
	return tnsMaxBandsLong[sampleRateIndex]
}

func tnsFilterCountForWindow(t *gaad.TNSData, window int) int {
	if t == nil || window < 0 || window >= len(t.N_filt) {
		return 0
	}
	return int(t.N_filt[window])
}

func tnsFilterLength(t *gaad.TNSData, window, filt int) int {
	if t == nil || window < 0 || window >= len(t.Len) || filt < 0 || filt >= len(t.Len[window]) {
		return 0
	}
	return int(t.Len[window][filt])
}

func tnsFilterOrder(t *gaad.TNSData, window, filt int) int {
	if t == nil || window < 0 || window >= len(t.Order) || filt < 0 || filt >= len(t.Order[window]) {
		return 0
	}
	return int(t.Order[window][filt])
}

func tnsFilterDirection(t *gaad.TNSData, window, filt int) bool {
	if t == nil || window < 0 || window >= len(t.Direction) || filt < 0 || filt >= len(t.Direction[window]) {
		return false
	}
	return t.Direction[window][filt]
}

func (d *synthDecoder) tnsCoefficients(t *gaad.TNSData, window, filt, order int) ([]float64, error) {
	if t == nil || window < 0 || window >= len(t.Coef) || filt < 0 || filt >= len(t.Coef[window]) {
		return nil, malformedf("invalid TNS filter metadata")
	}

	baseBits := 3
	if window < len(t.Coef_res) && t.Coef_res[window] > 0 {
		baseBits = 4
	}
	compress := 0
	if window < len(t.Coef_compress) && filt < len(t.Coef_compress[window]) {
		compress = int(t.Coef_compress[window][filt])
	}
	coefBits := baseBits - compress
	if coefBits < 2 || coefBits > 4 {
		return nil, unvalidatedFeature("AAC TNS coefficient width", fmt.Sprintf("%d", coefBits))
	}

	raw := t.Coef[window][filt]
	if order > len(raw) {
		return nil, malformedf("invalid TNS coefficient count")
	}

	if cap(d.tnsCoeffs) < order {
		d.tnsCoeffs = make([]float64, order)
	}
	out := d.tnsCoeffs[:order]
	steps := 1 << (baseBits - 1)
	positiveScale := (float64(steps) - 0.5) / (math.Pi / 2)
	negativeScale := (float64(steps) + 0.5) / (math.Pi / 2)
	for i := 0; i < order; i++ {
		value := signExtend(int(raw[i]), coefBits)
		if value >= 0 {
			out[i] = math.Sin(float64(value) / positiveScale)
			continue
		}
		out[i] = math.Sin(float64(value) / negativeScale)
	}
	return out, nil
}

func (d *synthDecoder) reflectionToPredictor(reflection []float64) []float64 {
	need := len(reflection) + 1
	if cap(d.tnsPredictor) < need {
		d.tnsPredictor = make([]float64, need)
	}
	predictor := d.tnsPredictor[:need]
	clear(predictor)
	predictor[0] = 1
	if cap(d.tnsWork) < need {
		d.tnsWork = make([]float64, need)
	}
	work := d.tnsWork[:need]
	clear(work)
	for m := 1; m <= len(reflection); m++ {
		predictor[m] = reflection[m-1]
		for i := 1; i < m; i++ {
			work[i] = predictor[i] + predictor[m]*predictor[m-i]
		}
		for i := 1; i < m; i++ {
			predictor[i] = work[i]
		}
	}
	return predictor[1:]
}

func (d *synthDecoder) applyTNSARFilter(spec []float64, index, size, step int, lpc []float64) error {
	if step == 0 {
		return malformedf("invalid TNS filter direction")
	}
	if len(lpc) == 0 || size <= 0 {
		return nil
	}

	if cap(d.tnsState) < len(lpc) {
		d.tnsState = make([]float64, len(lpc))
	}
	state := d.tnsState[:len(lpc)]
	clear(state)
	for i := 0; i < size; i++ {
		if index < 0 || index >= len(spec) {
			return malformedf("invalid TNS filter bounds")
		}

		y := spec[index]
		for j := 0; j < len(lpc); j++ {
			y -= lpc[j] * state[j]
		}
		for j := len(state) - 1; j > 0; j-- {
			state[j] = state[j-1]
		}
		state[0] = y
		spec[index] = y
		index += step
	}
	return nil
}

func signExtend(value, bits int) int {
	if bits <= 0 {
		return 0
	}
	signBit := 1 << (bits - 1)
	mask := (1 << bits) - 1
	value &= mask
	if value&signBit != 0 {
		value -= 1 << bits
	}
	return value
}

func minInt3(a, b, c int) int {
	if a > b {
		a = b
	}
	if a > c {
		a = c
	}
	return a
}
