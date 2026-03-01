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
type seekDebounceMsg struct {
	player *player.Player
	seq    uint64
}
type seekAppliedMsg struct {
	player *player.Player
	seq    uint64
	target time.Duration
	err    error
}

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

type trackFailedMsg struct {
	err error
}

const seekDebounceDelay = 200 * time.Millisecond

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

func seekDebounceCmd(p *player.Player, seq uint64) tea.Cmd {
	return tea.Tick(seekDebounceDelay, func(time.Time) tea.Msg {
		return seekDebounceMsg{player: p, seq: seq}
	})
}

func applySeekCmd(p *player.Player, seq uint64, target time.Duration, resume bool) tea.Cmd {
	if p == nil {
		return nil
	}
	return func() tea.Msg {
		err := p.SeekTo(target, resume)
		return seekAppliedMsg{
			player: p,
			seq:    seq,
			target: target,
			err:    err,
		}
	}
}
