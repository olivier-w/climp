package ui

import tea "github.com/charmbracelet/bubbletea"

func isQuit(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return true
	}
	return false
}

func helpText(canSave bool, hasQueue bool) string {
	s := "space pause  ←/→ seek  +/- volume  v viz  r repeat"
	if hasQueue {
		s += "  n/p track  j/k scroll  enter play"
	}
	if canSave {
		s += "  s save"
	}
	s += "  q quit"
	return s
}
