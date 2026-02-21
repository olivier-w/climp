package ui

import (
	"fmt"
	"strings"
)

func renderProgressBar(elapsed, total float64, width int) string {
	if width < 10 {
		width = 10
	}
	barWidth := width

	var ratio float64
	if total > 0 {
		ratio = elapsed / total
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	filled := int(ratio * float64(barWidth))
	// Note: filled <= barWidth is guaranteed since ratio is clamped to [0,1].

	// Build bar with circle indicator at current position
	// The circle replaces one character slot to maintain total width
	var bar string
	if filled == 0 {
		// At start: circle at beginning, all unfilled after
		bar = "●" + strings.Repeat("─", barWidth-1)
	} else if filled >= barWidth {
		// At end: all filled before, circle at end
		bar = strings.Repeat("━", barWidth-1) + "●"
	} else {
		// Middle: filled before circle, unfilled after
		bar = strings.Repeat("━", filled) + "●" + strings.Repeat("─", barWidth-filled-1)
	}
	return bar
}

func renderVolumePercent(vol float64) string {
	return fmt.Sprintf("vol %d%%", int(vol*100))
}
