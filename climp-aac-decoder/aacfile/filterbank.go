package aacfile

func (d *synthDecoder) synthChannel(ch int, meta *icsMeta, spec []float64, currentShape uint8) []float64 {
	windowed := d.windowSequence(meta.windowSequence, d.state[ch].prevWindowShape, currentShape, spec)

	pcm := make([]float64, longWindowLength)
	for i := 0; i < longWindowLength; i++ {
		pcm[i] = d.state[ch].overlap[i] + windowed[i]
	}
	copy(d.state[ch].overlap, windowed[longWindowLength:])
	d.state[ch].prevWindowShape = currentShape
	return pcm
}

func (d *synthDecoder) windowSequence(sequence, prevShape, currentShape uint8, spec []float64) []float64 {
	switch sequence {
	case windowOnlyLong:
		return applyOnlyLong(prevShape, currentShape, longIMDCTPlan.transform(spec, &d.longIMDCTWork))
	case windowLongStart:
		return applyLongStart(prevShape, currentShape, longIMDCTPlan.transform(spec, &d.longIMDCTWork))
	case windowLongStop:
		return applyLongStop(prevShape, currentShape, longIMDCTPlan.transform(spec, &d.longIMDCTWork))
	case windowEightShort:
		return d.applyEightShort(prevShape, currentShape, spec)
	default:
		return applyOnlyLong(prevShape, currentShape, longIMDCTPlan.transform(spec, &d.longIMDCTWork))
	}
}

func applyOnlyLong(prevShape, currentShape uint8, block []float64) []float64 {
	prev := longWindow(prevShape)
	curr := longWindow(currentShape)
	for i := 0; i < longWindowLength; i++ {
		block[i] *= prev[i]
		block[longBlockLength-1-i] *= curr[i]
	}
	return block
}

func applyLongStart(prevShape, currentShape uint8, block []float64) []float64 {
	prev := longWindow(prevShape)
	curr := shortWindow(currentShape)
	for i := 0; i < longWindowLength; i++ {
		block[i] *= prev[i]
	}
	for i := 0; i < shortWindowLength; i++ {
		block[longStartTail+i] *= curr[shortWindowLength-1-i]
	}
	for i := longStartZero; i < longBlockLength; i++ {
		block[i] = 0
	}
	return block
}

func applyLongStop(prevShape, currentShape uint8, block []float64) []float64 {
	prev := shortWindow(prevShape)
	curr := longWindow(currentShape)
	for i := 0; i < longStopLead; i++ {
		block[i] = 0
	}
	for i := 0; i < shortWindowLength; i++ {
		block[longStopLead+i] *= prev[i]
	}
	for i := 0; i < longWindowLength; i++ {
		block[longWindowLength+i] *= curr[longWindowLength-1-i]
	}
	return block
}

func (d *synthDecoder) applyEightShort(prevShape, currentShape uint8, spec []float64) []float64 {
	out := make([]float64, longBlockLength)
	prev := shortWindow(prevShape)
	curr := shortWindow(currentShape)
	for wnd := 0; wnd < 8; wnd++ {
		block := shortIMDCTPlan.transform(spec[wnd*shortWindowLength:(wnd+1)*shortWindowLength], &d.shortIMDCTWork)
		left := curr
		if wnd == 0 {
			left = prev
		}
		for i := 0; i < shortWindowLength; i++ {
			block[i] *= left[i]
			block[shortBlockLength-1-i] *= curr[i]
		}

		start := longStopLead + wnd*shortWindowLength
		for i := 0; i < shortBlockLength; i++ {
			out[start+i] += block[i]
		}
	}
	return out
}
