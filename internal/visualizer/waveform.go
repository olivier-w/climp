package visualizer

import (
	"math"
	"strings"
)

// Waveform renders dual stereo traces with spring smoothing.
type Waveform struct {
	left    springField
	right   springField
	output  string
	profile colorProfile
}

// NewWaveform creates a new waveform visualizer.
func NewWaveform() *Waveform {
	return &Waveform{
		left:    newSpringField(20, 14.0, 0.8),
		right:   newSpringField(20, 14.0, 0.8),
		profile: currentColorProfile(),
	}
}

func (w *Waveform) Name() string { return "waveform" }

func (w *Waveform) Update(samples []int16, width, height int) {
	if len(samples) < 4 || width < 4 || height < 1 {
		w.output = ""
		return
	}

	cols := width - 2
	if cols < 8 {
		cols = 8
	}

	w.left.resize(cols)
	w.right.resize(cols)

	frames := len(samples) / 2
	spf := float64(frames) / float64(cols)
	for c := range cols {
		lo := int(float64(c) * spf)
		hi := int(float64(c+1) * spf)
		if lo < 0 {
			lo = 0
		}
		if hi > frames {
			hi = frames
		}
		if hi <= lo {
			continue
		}

		var leftSum, rightSum float64
		count := 0
		for i := lo; i < hi; i++ {
			idx := i * 2
			leftSum += float64(samples[idx]) / 32768.0
			rightSum += float64(samples[idx+1]) / 32768.0
			count++
		}
		if count == 0 {
			continue
		}
		leftTarget := leftSum / float64(count)
		rightTarget := rightSum / float64(count)
		w.left.step(c, leftTarget)
		w.right.step(c, rightTarget)
	}

	mask := make([][]uint8, height)
	for r := range height {
		mask[r] = make([]uint8, cols)
	}

	mid := height / 2
	if mid >= 0 && mid < height {
		for c := range cols {
			mask[mid][c] = 4
		}
	}

	prevLY := ampToRow(w.left.pos[0], height)
	prevRY := ampToRow(w.right.pos[0], height)
	for c := 1; c < cols; c++ {
		ly := ampToRow(w.left.pos[c], height)
		ry := ampToRow(w.right.pos[c], height)
		drawLineMask(mask, c-1, prevLY, c, ly, 1)
		drawLineMask(mask, c-1, prevRY, c, ry, 2)
		prevLY, prevRY = ly, ry
	}

	var out strings.Builder
	color := newANSIState()
	den := cols - 1
	if den < 1 {
		den = 1
	}

	for r := range height {
		if r > 0 {
			out.WriteByte('\n')
		}
		for c := range cols {
			m := mask[r][c]
			switch m {
			case 1:
				if w.profile != colorNone {
					col := rgbFromHSV(0.53+0.04*math.Sin(float64(c)*0.22), 0.7, 0.95)
					color.set(&out, col)
				}
				out.WriteRune('●')
			case 2:
				if w.profile != colorNone {
					col := rgbFromHSV(0.88+0.05*math.Cos(float64(c)*0.19), 0.75, 0.95)
					color.set(&out, col)
				}
				out.WriteRune('●')
			case 3:
				if w.profile != colorNone {
					color.set(&out, colorRGB{R: 255, G: 248, B: 190})
				}
				out.WriteRune('✦')
			case 4:
				if w.profile != colorNone {
					fade := 0.15 + 0.15*float64(c)/float64(den)
					color.set(&out, rgbFromHSV(0.6, 0.2, fade))
				}
				out.WriteRune('·')
			default:
				out.WriteByte(' ')
			}
		}
		color.reset(&out)
	}

	w.output = out.String()
}

func ampToRow(amp float64, height int) int {
	if height <= 1 {
		return 0
	}
	amp = clamp01((amp + 1) / 2)
	span := height - 1
	row := int(math.Round((1 - amp) * float64(span)))
	if row < 0 {
		row = 0
	}
	if row >= height {
		row = height - 1
	}
	return row
}

func drawLineMask(mask [][]uint8, x0, y0, x1, y1 int, bit uint8) {
	maxY := len(mask)
	if maxY == 0 {
		return
	}
	maxX := len(mask[0])

	dx := absInt(x1 - x0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	dy := -absInt(y1 - y0)
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy

	for {
		if y0 >= 0 && y0 < maxY && x0 >= 0 && x0 < maxX {
			cur := mask[y0][x0]
			switch {
			case cur == 0 || cur == 4 || cur == bit:
				mask[y0][x0] = bit
			case cur != bit:
				mask[y0][x0] = 3
			}
		}

		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (w *Waveform) View() string {
	return w.output
}
