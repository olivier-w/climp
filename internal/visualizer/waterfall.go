package visualizer

import "strings"

var waterfallChars = []rune{' ', '.', ':', '-', '=', '+', '*', '#', '%', '@'}

// Waterfall renders a scrolling spectrogram heatmap.
type Waterfall struct {
	fft     *FFTBands
	smooth  springField
	history [][]float64
	output  string
	profile colorProfile
}

func NewWaterfall() *Waterfall {
	return &Waterfall{
		fft:     NewFFTBands(36),
		smooth:  newSpringField(20, 8.5, 0.72),
		profile: currentColorProfile(),
	}
}

func (w *Waterfall) Name() string { return "waterfall" }

func (w *Waterfall) Update(samples []int16, width, height int) {
	w.fft.Process(samples)
	norm := w.fft.NormalizedBands()

	if height < 1 {
		height = 1
	}
	cols := width - 2
	if cols < 8 {
		cols = 8
	}

	w.smooth.resize(cols)
	bands := w.fft.numBands
	den := cols - 1
	if den < 1 {
		den = 1
	}

	line := make([]float64, cols)
	for c := range cols {
		frac := float64(c) / float64(den) * float64(bands-1)
		lo := int(frac)
		hi := lo + 1
		if hi >= bands {
			hi = bands - 1
		}
		t := frac - float64(lo)
		target := norm[lo]*(1-t) + norm[hi]*t
		line[c] = clamp01(w.smooth.step(c, target))
	}

	if len(w.history) != height || (height > 0 && len(w.history[0]) != cols) {
		w.history = make([][]float64, height)
		for r := range height {
			w.history[r] = make([]float64, cols)
		}
	}

	for r := height - 1; r > 0; r-- {
		copy(w.history[r], w.history[r-1])
	}
	copy(w.history[0], line)

	var out strings.Builder
	color := newANSIState()

	for r := range height {
		if r > 0 {
			out.WriteByte('\n')
		}
		age := float64(r) / float64(height)
		for c := range cols {
			v := clamp01(w.history[r][c])
			idx := int(v * float64(len(waterfallChars)-1))
			if idx >= len(waterfallChars) {
				idx = len(waterfallChars) - 1
			}
			ch := waterfallChars[idx]
			if ch == ' ' || w.profile == colorNone {
				out.WriteRune(ch)
				continue
			}
			col := heatColor(v)
			col = lerpColor(col, colorRGB{R: 18, G: 22, B: 32}, age*0.65)
			color.set(&out, col)
			out.WriteRune(ch)
		}
		color.reset(&out)
	}

	w.output = out.String()
}

func (w *Waterfall) View() string {
	return w.output
}
