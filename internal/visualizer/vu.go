package visualizer

import (
	"fmt"
	"math"
	"strings"
)

// VUMeter renders a stereo VU meter with peak hold.
type VUMeter struct {
	leftRMS   float64
	rightRMS  float64
	leftPeak  float64
	rightPeak float64
	output    string
}

// NewVUMeter creates a new VU meter visualizer.
func NewVUMeter() *VUMeter {
	return &VUMeter{}
}

func (v *VUMeter) Name() string { return "vu meter" }

func (v *VUMeter) Update(samples []int16, width, height int) {
	if len(samples) < 2 {
		return
	}

	// Calculate RMS for left and right channels
	var leftSum, rightSum float64
	count := 0
	for i := 0; i+1 < len(samples); i += 2 {
		l := float64(samples[i]) / 32768.0
		r := float64(samples[i+1]) / 32768.0
		leftSum += l * l
		rightSum += r * r
		count++
	}
	if count == 0 {
		return
	}

	leftRMS := math.Sqrt(leftSum / float64(count))
	rightRMS := math.Sqrt(rightSum / float64(count))

	// Smooth
	const attack = 0.6
	const release = 0.15
	if leftRMS > v.leftRMS {
		v.leftRMS = v.leftRMS*(1-attack) + leftRMS*attack
	} else {
		v.leftRMS = v.leftRMS*(1-release) + leftRMS*release
	}
	if rightRMS > v.rightRMS {
		v.rightRMS = v.rightRMS*(1-attack) + rightRMS*attack
	} else {
		v.rightRMS = v.rightRMS*(1-release) + rightRMS*release
	}

	// Peak hold with decay
	const peakDecay = 0.02
	if v.leftRMS > v.leftPeak {
		v.leftPeak = v.leftRMS
	} else {
		v.leftPeak -= peakDecay
		if v.leftPeak < 0 {
			v.leftPeak = 0
		}
	}
	if v.rightRMS > v.rightPeak {
		v.rightPeak = v.rightRMS
	} else {
		v.rightPeak -= peakDecay
		if v.rightPeak < 0 {
			v.rightPeak = 0
		}
	}

	// Render
	barWidth := width - 6 // "L  " prefix + margin
	if barWidth < 10 {
		barWidth = 10
	}

	leftBar := renderVUBar(v.leftRMS, v.leftPeak, barWidth)
	rightBar := renderVUBar(v.rightRMS, v.rightPeak, barWidth)

	var sb strings.Builder
	if height >= 4 {
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf(" L  %s", leftBar))
	sb.WriteString("\n")
	if height >= 3 {
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf(" R  %s", rightBar))
	if height >= 4 {
		sb.WriteString("\n")
	}

	v.output = sb.String()
}

func renderVUBar(rms, peak float64, width int) string {
	// Scale: RMS of 0.5 = full bar (most music won't exceed this)
	level := rms * 2.0
	if level > 1.0 {
		level = 1.0
	}
	peakLevel := peak * 2.0
	if peakLevel > 1.0 {
		peakLevel = 1.0
	}

	filled := int(level * float64(width))
	peakPos := int(peakLevel * float64(width))
	if peakPos >= width {
		peakPos = width - 1
	}

	bar := make([]rune, width)
	for i := range width {
		if i < filled {
			// Color zones: green, yellow, red
			if i < width*6/10 {
				bar[i] = '█'
			} else if i < width*8/10 {
				bar[i] = '█'
			} else {
				bar[i] = '█'
			}
		} else if i == peakPos && peakPos > 0 {
			bar[i] = '|'
		} else {
			bar[i] = '─'
		}
	}

	return string(bar)
}

func (v *VUMeter) View() string {
	return v.output
}
