package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type tickMsg time.Time
type playbackEndedMsg struct{}
type fileSavedMsg struct {
	destName string
	err      error
}
type vizTickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func vizTickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return vizTickMsg(t)
	})
}
