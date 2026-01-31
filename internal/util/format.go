package util

import (
	"fmt"
	"time"
)

// FormatDuration formats a duration as m:ss.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	m := total / 60
	s := total % 60
	return fmt.Sprintf("%d:%02d", m, s)
}
