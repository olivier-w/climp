package aacfile

import (
	"fmt"
	"math"
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
		filterCount := ch.tnsData.filterCountForWindow(w)
		for filt := 0; filt < filterCount; filt++ {
			topBand := bottomBand
			length := ch.tnsData.filterLength(w, filt)
			order := ch.tnsData.filterOrder(w, filt)
			bottomBand -= length
			if bottomBand < 0 {
				bottomBand = 0
			}
			if order == 0 {
				continue
			}

			coeffs, err := ch.tnsData.coefficients(w, filt, order)
			if err != nil {
				return err
			}
			lpc := reflectionToPredictor(coeffs)
			startBand := minInt3(bottomBand, maxTNSBands, ch.meta.maxSFB)
			endBand := minInt3(topBand, maxTNSBands, ch.meta.maxSFB)
			if startBand < 0 || endBand < startBand || endBand > swbLimit {
				return malformedf("invalid TNS band range")
			}

			startLine := ch.meta.swbOffset[startBand]
			endLine := ch.meta.swbOffset[endBand]
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
			if ch.tnsData.filterDirection(w, filt) {
				index = end - 1
				step = -1
			}
			if err := applyTNSARFilter(ch.spec, index, end-start, step, lpc); err != nil {
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

func (t *tnsFilterData) filterCountForWindow(window int) int {
	if t == nil || window < 0 || window >= len(t.nFilt) {
		return 0
	}
	return t.nFilt[window]
}

func (t *tnsFilterData) filterLength(window, filt int) int {
	if t == nil || window < 0 || window >= len(t.length) || filt < 0 || filt >= len(t.length[window]) {
		return 0
	}
	return t.length[window][filt]
}

func (t *tnsFilterData) filterOrder(window, filt int) int {
	if t == nil || window < 0 || window >= len(t.order) || filt < 0 || filt >= len(t.order[window]) {
		return 0
	}
	return t.order[window][filt]
}

func (t *tnsFilterData) filterDirection(window, filt int) bool {
	if t == nil || window < 0 || window >= len(t.direction) || filt < 0 || filt >= len(t.direction[window]) {
		return false
	}
	return t.direction[window][filt]
}

func (t *tnsFilterData) coefficients(window, filt, order int) ([]float64, error) {
	if t == nil || window < 0 || window >= len(t.coef) || filt < 0 || filt >= len(t.coef[window]) {
		return nil, malformedf("invalid TNS filter metadata")
	}

	baseBits := 3
	if window < len(t.coefRes) && t.coefRes[window] > 0 {
		baseBits = 4
	}
	compress := 0
	if window < len(t.coefCompress) && filt < len(t.coefCompress[window]) {
		compress = t.coefCompress[window][filt]
	}
	coefBits := baseBits - compress
	if coefBits < 2 || coefBits > 4 {
		return nil, unvalidatedFeature("AAC TNS coefficient width", fmt.Sprintf("%d", coefBits))
	}

	raw := t.coef[window][filt]
	if order > len(raw) {
		return nil, malformedf("invalid TNS coefficient count")
	}

	out := make([]float64, order)
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

func reflectionToPredictor(reflection []float64) []float64 {
	predictor := make([]float64, len(reflection)+1)
	predictor[0] = 1
	work := make([]float64, len(reflection)+1)
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

func applyTNSARFilter(spec []float64, index, size, step int, lpc []float64) error {
	if step == 0 {
		return malformedf("invalid TNS filter direction")
	}
	if len(lpc) == 0 || size <= 0 {
		return nil
	}

	state := make([]float64, len(lpc))
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
