package ui

import tea "github.com/charmbracelet/bubbletea"

func isQuit(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return true
	}
	return false
}

func helpText() string {
	return "space pause  ←/→ seek  ↑/↓ volume  r repeat  q quit"
}
