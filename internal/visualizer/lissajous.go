package visualizer

import (
	"math"
	"strings"

	"github.com/charmbracelet/harmonica"
)

var lissajousTrail = []rune{'·', '•', '✶', '✹'}

type lissajousPoint struct {
	x float64
	y float64
}

// Lissajous renders a stereo phase-space scope with a trailing path.
type Lissajous struct {
	trail    []lissajousPoint
	maxTrail int
	spring   harmonica.Spring
	cx       float64
	cy       float64
	vx       float64
	vy       float64
	output   string
	profile  colorProfile
}

func NewLissajous() *Lissajous {
	return &Lissajous{
		spring:  harmonica.NewSpring(harmonica.FPS(20), 10.0, 0.7),
		profile: currentColorProfile(),
	}
}

func (l *Lissajous) Name() string { return "lissajous" }

func (l *Lissajous) Update(samples []int16, width, height int) {
	if len(samples) < 4 || width < 6 || height < 2 {
		l.output = ""
		return
	}

	cols := width - 2
	if cols < 8 {
		cols = 8
	}
	rows := height

	maxTrail := cols * 4
	if maxTrail < 32 {
		maxTrail = 32
	}
	l.maxTrail = maxTrail

	frames := len(samples) / 2
	step := frames / (cols * 2)
	if step < 1 {
		step = 1
	}

	for i := 0; i < frames; i += step {
		idx := i * 2
		left := float64(samples[idx]) / 32768.0
		right := float64(samples[idx+1]) / 32768.0

		targetX := (left + 1) * 0.5
		targetY := (right + 1) * 0.5
		l.cx, l.vx = l.spring.Update(l.cx, l.vx, targetX)
		l.cy, l.vy = l.spring.Update(l.cy, l.vy, targetY)

		l.trail = append(l.trail, lissajousPoint{x: l.cx, y: l.cy})
	}

	if len(l.trail) > l.maxTrail {
		l.trail = l.trail[len(l.trail)-l.maxTrail:]
	}

	chars := make([][]rune, rows)
	ages := make([][]float64, rows)
	for r := range rows {
		chars[r] = make([]rune, cols)
		ages[r] = make([]float64, cols)
		for c := range cols {
			chars[r][c] = ' '
			ages[r][c] = 1
		}
	}

	for i, p := range l.trail {
		x := int(clamp01(p.x) * float64(cols-1))
		y := int((1 - clamp01(p.y)) * float64(rows-1))
		if x < 0 || x >= cols || y < 0 || y >= rows {
			continue
		}
		age := float64(len(l.trail)-1-i) / float64(max(1, len(l.trail)-1))
		chars[y][x] = lissajousTrail[minInt(len(lissajousTrail)-1, int((1-age)*float64(len(lissajousTrail)-1)))]
		if age < ages[y][x] {
			ages[y][x] = age
		}
	}

	var out strings.Builder
	color := newANSIState()
	for r := range rows {
		if r > 0 {
			out.WriteByte('\n')
		}
		for c := range cols {
			ch := chars[r][c]
			if ch == ' ' || l.profile == colorNone {
				out.WriteRune(ch)
				continue
			}
			age := clamp01(1 - ages[r][c])
			hue := math.Mod(0.08+float64(c)/float64(cols)*0.75+age*0.12, 1)
			col := rgbFromHSV(hue, 0.78, 0.3+0.7*age)
			color.set(&out, col)
			out.WriteRune(ch)
		}
		color.reset(&out)
	}

	l.output = out.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (l *Lissajous) View() string {
	return l.output
}
