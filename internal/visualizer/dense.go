package visualizer

import (
	"math"
	"strings"
)

var densityRamp = []byte(" .:-=+*#%@")

// Dense renders a filled area chart using ASCII density characters,
// creating a smooth gradient from dense at the baseline to sparse at the top.
type Dense struct {
	fft    *FFTBands
	output string
}

func NewDense() *Dense {
	return &Dense{fft: NewFFTBands(defaultBands)}
}

func (d *Dense) Name() string { return "dense" }

func (d *Dense) Update(samples []int16, width, height int) {
	d.fft.Process(samples)
	norm := d.fft.NormalizedBands()

	if height < 1 {
		height = 1
	}
	cols := width - 2
	if cols < 4 {
		cols = 4
	}
	numBands := d.fft.numBands

	// Interpolate band values across all columns for a smooth curve
	colLevels := make([]float64, cols)
	for c := range cols {
		// Map column to fractional band position
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
	}

	rampLen := len(densityRamp)
	rows := make([]string, height)
	for row := range height {
		var line strings.Builder
		rowFromBottom := float64(height - 1 - row)
		for c := range cols {
			level := colLevels[c] * float64(height)
			dist := level - rowFromBottom

			var ch byte
			if dist <= 0 {
				ch = ' '
			} else if dist >= 1 {
				// Below the surface: density based on depth
				depth := dist / float64(height)
				idx := int(depth * float64(rampLen-1))
				if idx >= rampLen {
					idx = rampLen - 1
				}
				ch = densityRamp[idx]
			} else {
				// At the surface edge: partial fill
				idx := int(dist * float64(rampLen-1))
				if idx >= rampLen {
					idx = rampLen - 1
				}
				ch = densityRamp[idx]
			}
			line.WriteByte(ch)
		}
		rows[row] = line.String()
	}

	d.output = strings.Join(rows, "\n")
}

func (d *Dense) View() string {
	return d.output
}
