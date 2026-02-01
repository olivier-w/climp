package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/olivier-w/climp/internal/downloader"
	"github.com/olivier-w/climp/internal/player"
	"github.com/olivier-w/climp/internal/util"
)

// Model is the Bubbletea model for the climp TUI.
type Model struct {
	player     *player.Player
	metadata   player.Metadata
	elapsed    time.Duration
	duration   time.Duration
	volume     float64
	paused     bool
	width      int
	quitting   bool
	repeatMode RepeatMode

	sourcePath  string    // temp file path (empty for local files)
	sourceTitle string    // title for saved filename
	saveMsg     string    // transient status message
	saveMsgTime time.Time // when saveMsg was set
	saving      bool      // conversion in progress
}

// New creates a new Model. sourcePath is the temp file path for URL downloads
// (pass "" for local files to disable saving).
func New(p *player.Player, meta player.Metadata, sourcePath string) Model {
	return Model{
		player:      p,
		metadata:    meta,
		duration:    p.Duration(),
		volume:      p.Volume(),
		sourcePath:  sourcePath,
		sourceTitle: meta.Title,
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
		case "r":
			m.repeatMode = m.repeatMode.Next()
			return m, nil
		case "s":
			if m.sourcePath != "" && !m.saving {
				m.saving = true
				m.saveMsg = "Saving..."
				m.saveMsgTime = time.Now()
				src, title := m.sourcePath, m.sourceTitle
				return m, func() tea.Msg {
					destName, err := downloader.SaveFile(src, title)
					return fileSavedMsg{destName: destName, err: err}
				}
			}
			return m, nil
		}
		return m, nil

	case fileSavedMsg:
		m.saving = false
		if msg.err != nil {
			m.saveMsg = fmt.Sprintf("Save failed: %v", msg.err)
		} else {
			m.saveMsg = fmt.Sprintf("Saved to %s", msg.destName)
			m.sourcePath = ""
		}
		m.saveMsgTime = time.Now()
		return m, nil

	case tickMsg:
		m.elapsed = m.player.Position()
		m.volume = m.player.Volume()
		m.paused = m.player.Paused()
		if m.saveMsg != "" && time.Since(m.saveMsgTime) > 5*time.Second {
			m.saveMsg = ""
		}
		return m, tickCmd()

	case playbackEndedMsg:
		if m.repeatMode == RepeatOne {
			m.player.Restart()
			m.elapsed = 0
			return m, checkDone(m.player)
		}
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
	repeatIcon := m.repeatMode.Icon()
	volStr := renderVolumePercent(m.volume)

	// Right-align volume
	leftText := fmt.Sprintf("%s  %s", statusIcon, statusText)
	if repeatIcon != "" {
		leftText += "  " + repeatIcon
	}
	statusLeft := statusStyle.Render(leftText)
	statusRight := statusStyle.Render(volStr)
	gap := w - len(leftText) - len(volStr) - 4
	if gap < 2 {
		gap = 2
	}
	statusLine := fmt.Sprintf("%s%s%s", statusLeft, spaces(gap), statusRight)

	help := helpStyle.Render(helpText(m.sourcePath != ""))

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
	if m.saveMsg != "" {
		lines += "  " + helpStyle.Render(m.saveMsg) + "\n"
	}
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
