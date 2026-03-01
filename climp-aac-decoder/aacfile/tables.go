package aacfile

import (
	"math"
	"sync"
)

const (
	longBlockLength   = 2048
	shortBlockLength  = 256
	longWindowLength  = longBlockLength / 2
	shortWindowLength = shortBlockLength / 2

	windowOnlyLong   = 0
	windowLongStart  = 1
	windowEightShort = 2
	windowLongStop   = 3

	longStopLead  = 448
	longStartTail = 1472
	longStartZero = 1600
)

var (
	decodeTablesOnce sync.Once

	longIMDCTPlan  *imdctPlan
	shortIMDCTPlan *imdctPlan

	sineLongWindow  []float64
	sineShortWindow []float64
	kbdLongWindow   []float64
	kbdShortWindow  []float64

	pow43Table []float64

	tnsMaxBandsLong = []int{
		31, 31, 34, 40, 42, 51, 46, 46, 42, 42, 42, 39, 39, 0, 0, 0,
	}
	tnsMaxBandsShort = []int{
		9, 9, 10, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 0, 0, 0,
	}
)

func initDecodeTables() {
	decodeTablesOnce.Do(func() {
		longIMDCTPlan = newIMDCTPlan(longWindowLength)
		shortIMDCTPlan = newIMDCTPlan(shortWindowLength)

		sineLongWindow = buildSineWindow(longWindowLength)
		sineShortWindow = buildSineWindow(shortWindowLength)
		kbdLongWindow = buildKBDWindow(longWindowLength, 4.0)
		kbdShortWindow = buildKBDWindow(shortWindowLength, 6.0)

		pow43Table = make([]float64, 8192)
		for i := range pow43Table {
			pow43Table[i] = math.Pow(float64(i), 4.0/3.0)
		}
	})
}

func buildSineWindow(halfLen int) []float64 {
	window := make([]float64, halfLen)
	fullLen := float64(halfLen * 2)
	for i := range window {
		window[i] = math.Sin(math.Pi / (2 * fullLen) * float64(2*i+1))
	}
	return window
}

func buildKBDWindow(halfLen int, alpha float64) []float64 {
	weights := make([]float64, halfLen)
	total := 0.0
	for i := 0; i < halfLen; i++ {
		x := (2 * float64(i) / float64(halfLen)) - 1
		weights[i] = besselI0(math.Pi * alpha * math.Sqrt(1-x*x))
		total += weights[i]
	}

	window := make([]float64, halfLen)
	acc := 0.0
	for i := range window {
		acc += weights[i]
		window[i] = math.Sqrt(acc / total)
	}
	return window
}

func besselI0(x float64) float64 {
	sum := 1.0
	term := 1.0
	y := x * x / 4
	for k := 1; k < 64; k++ {
		term *= y / (float64(k) * float64(k))
		sum += term
		if term < 1e-14*sum {
			break
		}
	}
	return sum
}

func pow43(v int) float64 {
	if v < 0 {
		return -pow43(-v)
	}
	if v < len(pow43Table) {
		return pow43Table[v]
	}
	return math.Pow(float64(v), 4.0/3.0)
}

func longWindow(shape uint8) []float64 {
	if shape == 1 {
		return kbdLongWindow
	}
	return sineLongWindow
}

func shortWindow(shape uint8) []float64 {
	if shape == 1 {
		return kbdShortWindow
	}
	return sineShortWindow
}
