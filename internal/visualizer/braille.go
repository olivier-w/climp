package visualizer

import (
	"math"
	"strings"
)

// Braille renders a high-resolution spectrum using Unicode Braille characters.
// Each cell is a 2x4 dot grid, giving 2x horizontal and 4x vertical resolution.
type Braille struct {
	fft    *FFTBands
	output string
}

func NewBraille() *Braille {
	return &Braille{fft: NewFFTBands(32)}
}

func (b *Braille) Name() string { return "braille" }

// Braille dot positions (col, row) → bit offset:
//
//	(0,0)=0  (1,0)=3
//	(0,1)=1  (1,1)=4
//	(0,2)=2  (1,2)=5
//	(0,3)=6  (1,3)=7
var brailleBits = [2][4]uint{
	{0, 1, 2, 6},
	{3, 4, 5, 7},
}

func (b *Braille) Update(samples []int16, width, height int) {
	b.fft.Process(samples)
	norm := b.fft.NormalizedBands()

	if height < 1 {
		height = 1
	}
	cols := width - 2
	if cols < 2 {
		cols = 2
	}

	// Each braille char covers 2 dot-columns and 4 dot-rows
	dotCols := cols * 2
	dotRows := height * 4
	numBands := b.fft.numBands

	// Interpolate band values to dot-column resolution
	dotLevels := make([]float64, dotCols)
	for dc := range dotCols {
		frac := float64(dc) / float64(dotCols) * float64(numBands)
		lo := int(math.Floor(frac))
		hi := lo + 1
		t := frac - float64(lo)
		if lo >= numBands {
			lo = numBands - 1
		}
		if hi >= numBands {
			hi = numBands - 1
		}
		dotLevels[dc] = norm[lo]*(1-t) + norm[hi]*t
	}

	rows := make([]string, height)
	for row := range height {
		var line strings.Builder
		for col := range cols {
			var pattern uint
			for dx := range 2 {
				dc := col*2 + dx
				if dc >= dotCols {
					continue
				}
				level := dotLevels[dc] * float64(dotRows)
				for dy := range 4 {
					dotRow := row*4 + dy
					dotFromBottom := float64(dotRows - 1 - dotRow)
					if level > dotFromBottom {
						pattern |= 1 << brailleBits[dx][dy]
					}
				}
			}
			line.WriteRune(rune(0x2800 + pattern))
		}
		rows[row] = line.String()
	}

	b.output = strings.Join(rows, "\n")
}

func (b *Braille) View() string {
	return b.output
}
