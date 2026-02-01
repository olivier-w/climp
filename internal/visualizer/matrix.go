package visualizer

import (
	"math/rand"
	"strings"
)

const matrixTrailLen = 8

// Matrix renders a Matrix rain effect modulated by audio.
// Column activity and fall speed are driven by frequency bands.
type Matrix struct {
	fft     *FFTBands
	columns []matrixCol
	output  string
	rng     *rand.Rand
}

type matrixCol struct {
	active bool
	headY  float64 // fractional row position of the falling head
	speed  float64
	chars  []rune // trail characters
}

func NewMatrix() *Matrix {
	return &Matrix{
		fft: NewFFTBands(defaultBands),
		rng: rand.New(rand.NewSource(42)),
	}
}

func (m *Matrix) Name() string { return "matrix" }

func (m *Matrix) randomChar() rune {
	// Mix of digits and uppercase letters
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

	// Resize columns array if needed
	if len(m.columns) != cols {
		m.columns = make([]matrixCol, cols)
		for i := range m.columns {
			m.columns[i].chars = make([]rune, matrixTrailLen)
			for j := range m.columns[i].chars {
				m.columns[i].chars[j] = m.randomChar()
			}
		}
	}

	numBands := m.fft.numBands

	// Update each column based on its corresponding frequency band
	for c := range cols {
		bandIdx := c * numBands / cols
		if bandIdx >= numBands {
			bandIdx = numBands - 1
		}
		magnitude := norm[bandIdx]

		col := &m.columns[c]

		if !col.active {
			// Activation probability driven by magnitude
			if m.rng.Float64() < magnitude*0.15 {
				col.active = true
				col.headY = 0
				col.speed = 0.3 + magnitude*1.2
				for j := range col.chars {
					col.chars[j] = m.randomChar()
				}
			}
		} else {
			// Advance the head
			col.headY += col.speed
			// Randomize head character each tick
			col.chars[0] = m.randomChar()

			// Deactivate when trail has fully passed the bottom
			if int(col.headY)-matrixTrailLen > height {
				col.active = false
			}
		}
	}

	// Render grid
	grid := make([][]rune, height)
	for r := range height {
		grid[r] = make([]rune, cols)
		for c := range cols {
			grid[r][c] = ' '
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
			charIdx := t % len(col.chars)
			grid[row][c] = col.chars[charIdx]
		}
	}

	rows := make([]string, height)
	for r := range height {
		rows[r] = string(grid[r])
	}
	m.output = strings.Join(rows, "\n")
}

func (m *Matrix) View() string {
	return m.output
}
