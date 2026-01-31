package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/olivier-w/climp/internal/player"
	"github.com/olivier-w/climp/internal/util"
)

// Model is the Bubbletea model for the climp TUI.
type Model struct {
	player   *player.Player
	metadata player.Metadata
	elapsed  time.Duration
	duration time.Duration
	volume   float64
	paused   bool
	width    int
	quitting bool
}

// New creates a new Model.
func New(p *player.Player, meta player.Metadata) Model {
	return Model{
		player:   p,
		metadata: meta,
		duration: p.Duration(),
		volume:   p.Volume(),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), checkDone(m.player), tea.SetWindowTitle(windowTitle(m.metadata.Title, false)))
}

func checkDone(p *player.Player) tea.Cmd {
	return func() tea.Msg {
		<-p.Done()
		return playbackEndedMsg{}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if isQuit(msg) {
			m.quitting = true
			m.player.Close()
			return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)
		}
		switch msg.String() {
		case " ":
			m.player.TogglePause()
			m.paused = m.player.Paused()
			return m, tea.SetWindowTitle(windowTitle(m.metadata.Title, m.paused))
		case "left", "h":
			m.player.Seek(-5 * time.Second)
		case "right", "l":
			m.player.Seek(5 * time.Second)
		case "up", "k":
			m.player.AdjustVolume(0.05)
			m.volume = m.player.Volume()
		case "down", "j":
			m.player.AdjustVolume(-0.05)
			m.volume = m.player.Volume()
		}
		return m, nil

	case tickMsg:
		m.elapsed = m.player.Position()
		m.volume = m.player.Volume()
		m.paused = m.player.Paused()
		return m, tickCmd()

	case playbackEndedMsg:
		m.elapsed = m.duration
		m.quitting = true
		m.player.Close()
		return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	}

	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	w := m.width
	if w < 30 {
		w = 50
	}

	header := headerStyle.Render("climp")

	title := titleStyle.Render(m.metadata.Title)

	subtitle := ""
	if m.metadata.Artist != "" && m.metadata.Album != "" {
		subtitle = artistStyle.Render(fmt.Sprintf("%s - %s", m.metadata.Artist, m.metadata.Album))
	} else if m.metadata.Artist != "" {
		subtitle = artistStyle.Render(m.metadata.Artist)
	} else if m.metadata.Album != "" {
		subtitle = artistStyle.Render(m.metadata.Album)
	}

	elapsedStr := timeStyle.Render(util.FormatDuration(m.elapsed))
	durationStr := timeStyle.Render(util.FormatDuration(m.duration))
	barWidth := w - len(util.FormatDuration(m.elapsed)) - len(util.FormatDuration(m.duration)) - 6
	if barWidth < 10 {
		barWidth = 10
	}
	bar := renderProgressBar(m.elapsed.Seconds(), m.duration.Seconds(), barWidth)
	progressLine := fmt.Sprintf("%s %s %s", elapsedStr, bar, durationStr)

	statusIcon := "▶"
	statusText := "playing"
	if m.paused {
		statusIcon = "❚❚"
		statusText = "paused"
	}
	volStr := renderVolumePercent(m.volume)

	// Right-align volume
	statusLeft := statusStyle.Render(fmt.Sprintf("%s  %s", statusIcon, statusText))
	statusRight := statusStyle.Render(volStr)
	gap := w - len(fmt.Sprintf("%s  %s", statusIcon, statusText)) - len(volStr) - 4
	if gap < 2 {
		gap = 2
	}
	statusLine := fmt.Sprintf("%s%s%s", statusLeft, spaces(gap), statusRight)

	help := helpStyle.Render(helpText())

	lines := "\n"
	lines += "  " + header + "\n"
	lines += "\n"
	lines += "  " + title + "\n"
	if subtitle != "" {
		lines += "  " + subtitle + "\n"
	}
	lines += "\n"
	lines += "  " + progressLine + "\n"
	lines += "\n"
	lines += "  " + statusLine + "\n"
	lines += "\n"
	lines += "  " + help + "\n"

	return lines
}

func windowTitle(title string, paused bool) string {
	if paused {
		return "⏸ " + title + " — climp"
	}
	return "▶ " + title + " — climp"
}

func spaces(n int) string {
	if n < 0 {
		n = 0
	}
	s := ""
	for range n {
		s += " "
	}
	return s
}
