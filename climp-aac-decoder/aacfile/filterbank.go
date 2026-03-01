package aacfile

func (d *synthDecoder) synthChannel(ch int, meta *icsMeta, spec []float64, currentShape uint8) ([]float64, channelSynthStats) {
	windowed, imdctPeak := d.windowSequence(ch, meta.windowSequence, d.state[ch].prevWindowShape, currentShape, spec)

	pcm := d.scratch[ch].pcm[:longWindowLength]
	for i := 0; i < longWindowLength; i++ {
		pcm[i] = d.state[ch].overlap[i] + windowed[i]
	}
	copy(d.state[ch].overlap, windowed[longWindowLength:])
	d.state[ch].prevWindowShape = currentShape
	return pcm, channelSynthStats{
		imdctPeak:   imdctPeak,
		overlapPeak: peakAbs(pcm),
		pcmPeak:     peakAbs(pcm),
	}
}

func (d *synthDecoder) windowSequence(ch int, sequence, prevShape, currentShape uint8, spec []float64) ([]float64, float64) {
	switch sequence {
	case windowOnlyLong:
		block := longIMDCTPlan.transform(spec, &d.longIMDCTWork)
		imdctPeak := peakAbs(block)
		return applyOnlyLong(prevShape, currentShape, block), imdctPeak
	case windowLongStart:
		block := longIMDCTPlan.transform(spec, &d.longIMDCTWork)
		imdctPeak := peakAbs(block)
		return applyLongStart(prevShape, currentShape, block), imdctPeak
	case windowLongStop:
		block := longIMDCTPlan.transform(spec, &d.longIMDCTWork)
		imdctPeak := peakAbs(block)
		return applyLongStop(prevShape, currentShape, block), imdctPeak
	case windowEightShort:
		return d.applyEightShort(ch, prevShape, currentShape, spec)
	default:
		block := longIMDCTPlan.transform(spec, &d.longIMDCTWork)
		imdctPeak := peakAbs(block)
		return applyOnlyLong(prevShape, currentShape, block), imdctPeak
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

func (d *synthDecoder) applyEightShort(ch int, prevShape, currentShape uint8, spec []float64) ([]float64, float64) {
	out := d.scratch[ch].windowed[:longBlockLength]
	clear(out)
	prev := shortWindow(prevShape)
	curr := shortWindow(currentShape)
	imdctPeak := 0.0
	for wnd := 0; wnd < 8; wnd++ {
		block := shortIMDCTPlan.transform(spec[wnd*shortWindowLength:(wnd+1)*shortWindowLength], &d.shortIMDCTWork)
		imdctPeak = maxFloat64(imdctPeak, peakAbs(block))
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
	return out, imdctPeak
}
