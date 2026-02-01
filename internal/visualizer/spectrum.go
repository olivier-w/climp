package visualizer

import (
	"math"
	"strings"
)

const (
	spectrumBands = 16
	fftSize       = 1024
)

var barChars = []rune(" ▁▂▃▄▅▆▇█")

// Spectrum renders a frequency spectrum as vertical bars.
type Spectrum struct {
	bands  [spectrumBands]float64
	output string
}

// NewSpectrum creates a new spectrum visualizer.
func NewSpectrum() *Spectrum {
	return &Spectrum{}
}

func (s *Spectrum) Name() string { return "spectrum" }

func (s *Spectrum) Update(samples []int16, width, height int) {
	if len(samples) < fftSize {
		return
	}

	// Mix to mono and window
	real := make([]float64, fftSize)
	imag := make([]float64, fftSize)
	for i := range fftSize {
		idx := i * 2 // stereo: take every other pair
		if idx+1 < len(samples) {
			real[i] = float64(samples[idx]+samples[idx+1]) / 65536.0
		} else if idx < len(samples) {
			real[i] = float64(samples[idx]) / 32768.0
		}
		// Hann window
		w := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(fftSize-1)))
		real[i] *= w
	}

	fft(real, imag)

	// Group into logarithmic frequency bands
	var bandMag [spectrumBands]float64
	maxBin := fftSize / 2

	for b := range spectrumBands {
		// Logarithmic band boundaries
		lo := int(math.Pow(float64(maxBin), float64(b)/float64(spectrumBands)))
		hi := int(math.Pow(float64(maxBin), float64(b+1)/float64(spectrumBands)))
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
			mag := math.Sqrt(real[i]*real[i] + imag[i]*imag[i])
			sum += mag
			count++
		}
		if count > 0 {
			bandMag[b] = sum / float64(count)
		}
	}

	// Exponential smoothing
	const decay = 0.3
	for b := range spectrumBands {
		s.bands[b] = s.bands[b]*decay + bandMag[b]*(1-decay)
	}

	// Render
	if height < 1 {
		height = 1
	}
	cols := spectrumBands
	if width > 4 {
		cols = spectrumBands
	}

	// Find max for normalization
	maxVal := 0.01
	for _, v := range s.bands {
		if v > maxVal {
			maxVal = v
		}
	}

	// Calculate column width so bars span the available width
	colWidth := (width - 2) / cols
	if colWidth < 1 {
		colWidth = 1
	}
	gap := 1
	if colWidth <= 1 {
		gap = 0
	}

	rows := make([]string, height)
	for row := range height {
		var line strings.Builder
		for b := range cols {
			if b > 0 && gap > 0 {
				line.WriteByte(' ')
			}
			level := s.bands[b] / maxVal * float64(height)
			rowFromBottom := float64(height - 1 - row)
			charIdx := 0
			if level > rowFromBottom+1 {
				charIdx = len(barChars) - 1
			} else if level > rowFromBottom {
				frac := level - rowFromBottom
				charIdx = int(frac * float64(len(barChars)-1))
			}
			ch := barChars[charIdx]
			for range colWidth - gap {
				line.WriteRune(ch)
			}
		}
		rows[row] = line.String()
	}

	s.output = strings.Join(rows, "\n")
}

func (s *Spectrum) View() string {
	return s.output
}
