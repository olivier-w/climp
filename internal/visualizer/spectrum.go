package visualizer

import "strings"

var barChars = []rune(" ▁▂▃▄▅▆▇█")

// Spectrum renders a frequency spectrum as vertical bars.
type Spectrum struct {
	fft    *FFTBands
	output string
}

// NewSpectrum creates a new spectrum visualizer.
func NewSpectrum() *Spectrum {
	return &Spectrum{fft: NewFFTBands(defaultBands)}
}

func (s *Spectrum) Name() string { return "spectrum" }

func (s *Spectrum) Update(samples []int16, width, height int) {
	s.fft.Process(samples)
	norm := s.fft.NormalizedBands()

	if height < 1 {
		height = 1
	}
	cols := s.fft.numBands

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
			level := norm[b] * float64(height)
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
