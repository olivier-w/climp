package visualizer

import "math"

const (
	defaultFFTSize = 1024
	defaultBands   = 16
	defaultDecay   = 0.3
)

// FFTBands provides reusable FFT processing with logarithmic frequency banding
// and exponential smoothing. Multiple visualizers can embed this to share the
// same audio analysis pipeline.
type FFTBands struct {
	numBands int
	fftSize  int
	decay    float64
	bands    []float64
	real     []float64
	imag     []float64
	norm     []float64
}

// NewFFTBands creates an FFTBands processor with the given band count.
func NewFFTBands(numBands int) *FFTBands {
	return &FFTBands{
		numBands: numBands,
		fftSize:  defaultFFTSize,
		decay:    defaultDecay,
		bands:    make([]float64, numBands),
		real:     make([]float64, defaultFFTSize),
		imag:     make([]float64, defaultFFTSize),
		norm:     make([]float64, numBands),
	}
}

// Process runs the FFT pipeline on stereo int16 samples: mono mix, Hann window,
// FFT, logarithmic banding, and exponential smoothing.
func (f *FFTBands) Process(samples []int16) {
	if len(samples) < f.fftSize {
		return
	}

	for i := range f.fftSize {
		idx := i * 2
		if idx+1 < len(samples) {
			f.real[i] = float64(samples[idx]+samples[idx+1]) / 65536.0
		} else if idx < len(samples) {
			f.real[i] = float64(samples[idx]) / 32768.0
		} else {
			f.real[i] = 0
		}
		f.imag[i] = 0
		w := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(f.fftSize-1)))
		f.real[i] *= w
	}

	fft(f.real, f.imag)

	maxBin := f.fftSize / 2
	for b := range f.numBands {
		lo := int(math.Pow(float64(maxBin), float64(b)/float64(f.numBands)))
		hi := int(math.Pow(float64(maxBin), float64(b+1)/float64(f.numBands)))
		if lo < 1 {
			lo = 1
		}
		if hi <= lo {
			hi = lo + 1
		}
		if hi > maxBin {
			hi = maxBin
		}

		sum := 0.0
		count := 0
		for i := lo; i < hi; i++ {
			mag := math.Sqrt(f.real[i]*f.real[i] + f.imag[i]*f.imag[i])
			sum += mag
			count++
		}
		var bandMag float64
		if count > 0 {
			bandMag = sum / float64(count)
		}

		f.bands[b] = f.bands[b]*f.decay + bandMag*(1-f.decay)
	}
}

// Bands returns the current smoothed band magnitudes (raw values).
func (f *FFTBands) Bands() []float64 {
	return f.bands
}

// NormalizedBands returns band values scaled to 0.0–1.0 relative to the current max.
func (f *FFTBands) NormalizedBands() []float64 {
	maxVal := 0.01
	for _, v := range f.bands {
		if v > maxVal {
			maxVal = v
		}
	}
	for i, v := range f.bands {
		f.norm[i] = v / maxVal
	}
	return f.norm
}
