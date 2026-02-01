package visualizer

import (
	"math"
	"strings"
)

// Hatching renders a spectrum with an engraving/etching aesthetic using
// directional line characters. Low frequencies use horizontal lines,
// mid frequencies use diagonals, high frequencies use verticals.
// Amplitude controls density from dots to cross-hatching.
type Hatching struct {
	fft    *FFTBands
	output string
}

func NewHatching() *Hatching {
	return &Hatching{fft: NewFFTBands(defaultBands)}
}

func (h *Hatching) Name() string { return "hatching" }

// Density layers from sparse to dense:
//
//	0: space
//	1: · (dot)
//	2: directional line (- / \ |)
//	3: + (cross)
//	4: # (dense cross-hatch)
//	5: @ (fill)
func (h *Hatching) Update(samples []int16, width, height int) {
	h.fft.Process(samples)
	norm := h.fft.NormalizedBands()

	if height < 1 {
		height = 1
	}
	cols := width - 2
	if cols < 4 {
		cols = 4
	}
	numBands := h.fft.numBands

	// Interpolate bands across columns
	colLevels := make([]float64, cols)
	colBandPos := make([]float64, cols) // 0.0=low freq, 1.0=high freq
	for c := range cols {
		frac := float64(c) / float64(cols) * float64(numBands)
		lo := int(math.Floor(frac))
		hi := lo + 1
		t := frac - float64(lo)
		if lo >= numBands {
			lo = numBands - 1
		}
		if hi >= numBands {
			hi = numBands - 1
		}
		colLevels[c] = norm[lo]*(1-t) + norm[hi]*t
		colBandPos[c] = float64(c) / float64(cols)
	}

	rows := make([]string, height)
	for row := range height {
		var line strings.Builder
		rowFromBottom := float64(height - 1 - row)
		for c := range cols {
			level := colLevels[c] * float64(height)
			dist := level - rowFromBottom

			if dist <= 0 {
				line.WriteByte(' ')
				continue
			}

			// Amplitude determines density layer (0–1 mapped to layers)
			density := dist / float64(height)
			if density > 1 {
				density = 1
			}

			freq := colBandPos[c]
			ch := hatchChar(density, freq, row, c)
			line.WriteRune(ch)
		}
		rows[row] = line.String()
	}

	h.output = strings.Join(rows, "\n")
}

func hatchChar(density, freq float64, row, col int) rune {
	switch {
	case density < 0.15:
		// Sparse dots
		if (row+col)%3 == 0 {
			return '·'
		}
		return ' '
	case density < 0.35:
		// Directional lines based on frequency band
		return dirChar(freq, row, col)
	case density < 0.55:
		// Alternating directions for cross-hatch effect
		if (row+col)%2 == 0 {
			return dirChar(freq, row, col)
		}
		return '+'
	case density < 0.75:
		return '#'
	default:
		return '@'
	}
}

func dirChar(freq float64, row, col int) rune {
	switch {
	case freq < 0.33:
		return '-'
	case freq < 0.66:
		if (row+col)%2 == 0 {
			return '/'
		}
		return '\\'
	default:
		return '|'
	}
}

func (h *Hatching) View() string {
	return h.output
}
