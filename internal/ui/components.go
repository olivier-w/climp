package ui

import (
	"fmt"
	"strings"
)

func renderProgressBar(elapsed, total float64, width int) string {
	if width < 10 {
		width = 10
	}
	barWidth := width - 2 // leave some margin

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

	bar := strings.Repeat("━", filled) + strings.Repeat("─", barWidth-filled)
	return bar
}

func renderVolumePercent(vol float64) string {
	return fmt.Sprintf("vol %d%%", int(vol*100))
}
