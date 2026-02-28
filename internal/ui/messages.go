package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/olivier-w/climp/internal/downloader"
	"github.com/olivier-w/climp/internal/player"
)

type tickMsg time.Time
type playbackEndedMsg struct {
	player *player.Player
}
type liveTitleUpdatedMsg struct {
	player *player.Player
	title  string
}
type fileSavedMsg struct {
	destName string
	err      error
}
type vizTickMsg time.Time

type trackDownloadedMsg struct {
	index   int
	path    string
	title   string
	cleanup func()
	err     error
}

type playlistExtractedMsg struct {
	entries []downloader.PlaylistEntry
	err     error
}

type trackFailedMsg struct{}

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
