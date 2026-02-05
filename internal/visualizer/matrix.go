package visualizer

import (
	"math"
	"math/rand"
	"strings"
)

const matrixTrailLen = 8

// Matrix renders a Matrix rain effect modulated by audio.
// Column activity and fall speed are driven by frequency bands.
type Matrix struct {
	fft      *FFTBands
	energy   springField
	columns  []matrixCol
	output   string
	rng      *rand.Rand
	profile  colorProfile
	trailAge [][]int
	trailChr [][]rune
}

type matrixCol struct {
	active bool
	headY  float64
	speed  float64
	chars  []rune
}

func NewMatrix() *Matrix {
	return &Matrix{
		fft:     NewFFTBands(defaultBands),
		energy:  newSpringField(20, 9.0, 0.78),
		rng:     rand.New(rand.NewSource(42)),
		profile: currentColorProfile(),
	}
}

func (m *Matrix) Name() string { return "matrix" }

func (m *Matrix) randomChar() rune {
	n := m.rng.Intn(36)
	if n < 10 {
		return rune('0' + n)
	}
	return rune('A' + n - 10)
}

func (m *Matrix) Update(samples []int16, width, height int) {
	m.fft.Process(samples)
	norm := m.fft.NormalizedBands()

	if height < 1 {
		height = 1
	}
	cols := width - 2
	if cols < 4 {
		cols = 4
	}

	if len(m.columns) != cols {
		m.columns = make([]matrixCol, cols)
		for i := range m.columns {
			m.columns[i].chars = make([]rune, matrixTrailLen)
			for j := range m.columns[i].chars {
				m.columns[i].chars[j] = m.randomChar()
			}
		}
	}

	m.energy.resize(cols)
	if len(m.trailAge) != height || (height > 0 && len(m.trailAge[0]) != cols) {
		m.trailAge = make([][]int, height)
		m.trailChr = make([][]rune, height)
		for r := range height {
			m.trailAge[r] = make([]int, cols)
			m.trailChr[r] = make([]rune, cols)
		}
	}

	numBands := m.fft.numBands
	for c := range cols {
		bandIdx := c * numBands / cols
		if bandIdx >= numBands {
			bandIdx = numBands - 1
		}
		magnitude := clamp01(m.energy.step(c, norm[bandIdx]))

		col := &m.columns[c]
		if !col.active {
			if m.rng.Float64() < magnitude*0.22 {
				col.active = true
				col.headY = 0
				col.speed = 0.22 + magnitude*1.95
				for j := range col.chars {
					col.chars[j] = m.randomChar()
				}
			}
		} else {
			col.headY += col.speed
			col.chars[0] = m.randomChar()
			if int(col.headY)-matrixTrailLen > height {
				col.active = false
			}
		}
	}

	for r := range height {
		for c := range cols {
			m.trailAge[r][c] = -1
			m.trailChr[r][c] = ' '
		}
	}

	for c := range cols {
		col := &m.columns[c]
		if !col.active {
			continue
		}
		headRow := int(col.headY)
		for t := range matrixTrailLen {
			row := headRow - t
			if row < 0 || row >= height {
				continue
			}
			m.trailAge[row][c] = t
			m.trailChr[row][c] = col.chars[t%len(col.chars)]
		}
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
			age := m.trailAge[r][c]
			if age < 0 {
				out.WriteByte(' ')
				continue
			}

			ch := m.trailChr[r][c]
			if m.profile == colorNone {
				out.WriteRune(ch)
				continue
			}

			energy := clamp01(m.energy.pos[c])
			if age == 0 {
				color.set(&out, colorRGB{R: 234, G: 255, B: 240})
				out.WriteRune(ch)
				continue
			}

			fade := 1 - float64(age)/float64(matrixTrailLen)
			hue := 0.31 + 0.12*math.Sin(float64(c)/float64(den)*math.Pi)
			col := rgbFromHSV(hue, 0.72, 0.2+0.65*fade+0.15*energy)
			color.set(&out, col)
			out.WriteRune(ch)
		}
		color.reset(&out)
	}

	m.output = out.String()
}

func (m *Matrix) View() string {
	return m.output
}
