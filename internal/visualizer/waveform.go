package visualizer

import (
	"math"
	"strings"
)

var waveChars = []rune(" ░▒▓█")

// Waveform renders a scrolling waveform display.
type Waveform struct {
	output string
}

// NewWaveform creates a new waveform visualizer.
func NewWaveform() *Waveform {
	return &Waveform{}
}

func (w *Waveform) Name() string { return "waveform" }

func (w *Waveform) Update(samples []int16, width, height int) {
	if len(samples) < 2 || width < 4 || height < 1 {
		return
	}

	cols := width - 2
	if cols < 4 {
		cols = 4
	}

	// Downsample to fit width (mix stereo pairs to mono)
	mono := make([]float64, len(samples)/2)
	for i := 0; i+1 < len(samples); i += 2 {
		mono[i/2] = (float64(samples[i]) + float64(samples[i+1])) / 65536.0
	}

	// Map mono samples to columns
	colAmps := make([]float64, cols)
	samplesPerCol := float64(len(mono)) / float64(cols)
	for c := range cols {
		lo := int(float64(c) * samplesPerCol)
		hi := int(float64(c+1) * samplesPerCol)
		if hi > len(mono) {
			hi = len(mono)
		}
		if lo >= hi {
			continue
		}
		maxAmp := 0.0
		for i := lo; i < hi; i++ {
			a := math.Abs(mono[i])
			if a > maxAmp {
				maxAmp = a
			}
		}
		colAmps[c] = maxAmp
	}

	// Normalize
	maxVal := 0.01
	for _, a := range colAmps {
		if a > maxVal {
			maxVal = a
		}
	}
	for i := range colAmps {
		colAmps[i] /= maxVal
	}

	// Render: each column has a height proportional to amplitude, centered vertically
	halfH := float64(height) / 2.0
	grid := make([][]rune, height)
	for r := range height {
		grid[r] = make([]rune, cols)
		for c := range cols {
			grid[r][c] = ' '
		}
	}

	for c := range cols {
		amp := colAmps[c]
		barHalf := amp * halfH
		mid := height / 2
		// Draw symmetric from center
		for r := range height {
			dist := math.Abs(float64(r) - float64(mid))
			if dist < barHalf {
				// Full block
				charIdx := len(waveChars) - 1
				grid[r][c] = waveChars[charIdx]
			} else if dist < barHalf+1 {
				frac := barHalf + 1 - dist
				charIdx := int(frac * float64(len(waveChars)-1))
				if charIdx >= len(waveChars) {
					charIdx = len(waveChars) - 1
				}
				grid[r][c] = waveChars[charIdx]
			}
		}
	}

	rows := make([]string, height)
	for r := range height {
		rows[r] = string(grid[r])
	}
	w.output = strings.Join(rows, "\n")
}

func (w *Waveform) View() string {
	return w.output
}
