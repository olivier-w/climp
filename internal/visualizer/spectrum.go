package visualizer

import "strings"

var spectrumChars = []rune{' ', '░', '▒', '▓', '█'}

// Spectrum renders a spring-smoothed frequency spectrum with peak hold.
type Spectrum struct {
	fft     *FFTBands
	smooth  springField
	peaks   []float64
	output  string
	profile colorProfile
}

// NewSpectrum creates a new spectrum visualizer.
func NewSpectrum() *Spectrum {
	return &Spectrum{
		fft:     NewFFTBands(24),
		smooth:  newSpringField(20, 10.0, 0.75),
		profile: currentColorProfile(),
	}
}

func (s *Spectrum) Name() string { return "spectrum" }

func (s *Spectrum) Update(samples []int16, width, height int) {
	s.fft.Process(samples)
	norm := s.fft.NormalizedBands()

	if height < 1 {
		height = 1
	}
	cols := width - 2
	if cols < 8 {
		cols = 8
	}

	s.smooth.resize(cols)
	if len(s.peaks) != cols {
		s.peaks = make([]float64, cols)
	}

	bands := s.fft.numBands
	den := cols - 1
	if den < 1 {
		den = 1
	}

	for c := range cols {
		frac := float64(c) / float64(den) * float64(bands-1)
		lo := int(frac)
		hi := lo + 1
		if hi >= bands {
			hi = bands - 1
		}
		t := frac - float64(lo)
		target := norm[lo]*(1-t) + norm[hi]*t
		level := clamp01(s.smooth.step(c, target))
		if level >= s.peaks[c] {
			s.peaks[c] = level
		} else {
			s.peaks[c] -= 0.03
			if s.peaks[c] < 0 {
				s.peaks[c] = 0
			}
		}
	}

	var out strings.Builder
	color := newANSIState()
	span := height - 1
	if span < 1 {
		span = 1
	}

	for row := range height {
		if row > 0 {
			out.WriteByte('\n')
		}
		rowFromBottom := float64(height - 1 - row)
		for c := range cols {
			level := s.smooth.pos[c] * float64(height)
			fill := level - rowFromBottom
			charIdx := 0
			if fill >= 0.95 {
				charIdx = len(spectrumChars) - 1
			} else if fill > 0 {
				charIdx = int(fill * float64(len(spectrumChars)-1))
				if charIdx >= len(spectrumChars) {
					charIdx = len(spectrumChars) - 1
				}
			}

			ch := spectrumChars[charIdx]
			peakRow := height - 1 - int(s.peaks[c]*float64(span))
			if row == peakRow && s.peaks[c] > 0.02 {
				ch = '▀'
				if s.profile != colorNone {
					color.set(&out, colorRGB{R: 250, G: 250, B: 250})
				}
				out.WriteRune(ch)
				continue
			}

			if ch == ' ' || s.profile == colorNone {
				out.WriteRune(ch)
				continue
			}

			hue := 0.62 - 0.58*float64(c)/float64(den)
			rowFactor := float64(height-1-row) / float64(span)
			col := rgbFromHSV(hue, 0.85, 0.35+0.65*rowFactor)
			color.set(&out, col)
			out.WriteRune(ch)
		}
		color.reset(&out)
	}

	s.output = out.String()
}

func (s *Spectrum) View() string {
	return s.output
}
