package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/olivier-w/climp/internal/downloader"
)

type tickMsg time.Time
type playbackEndedMsg struct{}
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

type trackDownloadProgressMsg struct {
	index  int
	status downloader.DownloadStatus
}

type playlistExtractedMsg struct {
	entries []downloader.PlaylistEntry
	err     error
}

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
