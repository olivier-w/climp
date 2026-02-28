package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olivier-w/climp/internal/downloader"
	"github.com/olivier-w/climp/internal/ui"
)

type startupPhase uint8

const (
	phaseBrowse startupPhase = iota
	phaseOpening
)

type startupResolvedMsg struct {
	model ui.Model
	err   error
}

type startupDownloadStatusMsg downloader.DownloadStatus

type startupModel struct {
	browser   ui.BrowserModel
	phase     startupPhase
	errMsg    string
	width     int
	height    int
	spinner   spinner.Model
	progress  progress.Model
	status    downloader.DownloadStatus
	statusCh  chan downloader.DownloadStatus
	hasStatus bool
}

func newStartupModel() startupModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#AAAAAA"})

	p := progress.New(
		progress.WithScaledGradient("#FF8C00", "#FF5F1F"),
		progress.WithoutPercentage(),
	)

	return startupModel{
		browser:  ui.NewEmbeddedBrowser(),
		phase:    phaseBrowse,
		spinner:  s,
		progress: p,
		status:   downloader.DownloadStatus{Phase: "fetching", Percent: -1},
	}
}

func (m startupModel) Init() tea.Cmd {
	return tea.Batch(m.browser.Init(), m.spinner.Tick)
}

func (m startupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		barWidth := msg.Width - 8
		if barWidth < 20 {
			barWidth = 20
		}
		if barWidth > 60 {
			barWidth = 60
		}
		m.progress.Width = barWidth
		if m.phase == phaseBrowse {
			model, cmd := m.browser.Update(msg)
			if browser, ok := model.(ui.BrowserModel); ok {
				m.browser = browser
			}
			return m, cmd
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.phase == phaseOpening {
			return m, cmd
		}
		return m, nil

	case ui.BrowserCancelledMsg:
		return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)

	case ui.BrowserSelectedMsg:
		m.phase = phaseOpening
		m.errMsg = ""
		m.hasStatus = false
		m.status = downloader.DownloadStatus{Phase: "fetching", Percent: -1}
		m.statusCh = make(chan downloader.DownloadStatus, 16)
		return m, tea.Batch(
			m.spinner.Tick,
			m.waitForStatus(),
			openSelectionCmd(msg.Path, m.statusCh),
		)

	case startupDownloadStatusMsg:
		m.hasStatus = true
		m.status = downloader.DownloadStatus(msg)
		return m, m.waitForStatus()

	case startupResolvedMsg:
		if msg.err != nil {
			m.phase = phaseBrowse
			m.errMsg = msg.err.Error()
			m.hasStatus = false
			m.statusCh = nil
			return m, nil
		}

		cmds := []tea.Cmd{msg.model.Init()}
		if m.width > 0 || m.height > 0 {
			w, h := m.width, m.height
			cmds = append(cmds, func() tea.Msg {
				return tea.WindowSizeMsg{Width: w, Height: h}
			})
		}
		return msg.model, tea.Batch(cmds...)

	case tea.KeyMsg:
		if m.phase == phaseOpening && startupIsQuit(msg) {
			return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)
		}
	}

	if m.phase == phaseBrowse {
		model, cmd := m.browser.Update(msg)
		if browser, ok := model.(ui.BrowserModel); ok {
			m.browser = browser
		}
		return m, cmd
	}

	return m, nil
}

func (m startupModel) waitForStatus() tea.Cmd {
	if m.statusCh == nil {
		return nil
	}
	statusCh := m.statusCh
	return func() tea.Msg {
		status, ok := <-statusCh
		if !ok {
			return nil
		}
		return startupDownloadStatusMsg(status)
	}
}

func (m startupModel) View() string {
	if m.phase == phaseBrowse {
		if m.browser.HasError() {
			return "\n  climp\n\n  " + m.browser.Error().Error() + "\n"
		}
		if m.errMsg == "" {
			return m.browser.View()
		}
		return "\n  climp\n\n  " + m.renderError() + "\n\n" + indentBlock(m.browser.View(), "  ")
	}

	return m.renderOpeningView()
}

func (m startupModel) renderOpeningView() string {
	var b strings.Builder
	b.WriteString("\n  ")
	b.WriteString(startupHeaderStyle.Render("climp"))
	b.WriteString("\n\n")

	switch m.status.Phase {
	case "downloading":
		if m.status.Percent >= 0 {
			b.WriteString("  ")
			b.WriteString(startupStatusStyle.Render("Downloading..."))
			b.WriteString("\n")
			b.WriteString("  ")
			b.WriteString(m.progress.ViewAs(m.status.Percent))
			b.WriteString(fmt.Sprintf("  %.0f%%\n", m.status.Percent*100))

			detail := ""
			if m.status.TotalSize != "" {
				detail += m.status.TotalSize
			}
			if m.status.Speed != "" {
				if detail != "" {
					detail += "  ·  "
				}
				detail += m.status.Speed
			}
			if m.status.ETA != "" {
				if detail != "" {
					detail += "  ·  "
				}
				detail += "ETA " + m.status.ETA
			}
			if detail != "" {
				b.WriteString("  ")
				b.WriteString(startupHelpStyle.Render(detail))
				b.WriteString("\n")
			}
			break
		}
		fallthrough
	case "fetching":
		label := "Opening..."
		if m.hasStatus {
			label = "Fetching info..."
		}
		b.WriteString("  ")
		b.WriteString(m.spinner.View())
		b.WriteString(" ")
		b.WriteString(startupStatusStyle.Render(label))
		b.WriteString("\n")
	case "converting":
		b.WriteString("  ")
		b.WriteString(m.spinner.View())
		b.WriteString(" ")
		b.WriteString(startupStatusStyle.Render("Converting..."))
		b.WriteString("\n")
	default:
		b.WriteString("  ")
		b.WriteString(m.spinner.View())
		b.WriteString(" ")
		b.WriteString(startupStatusStyle.Render("Opening..."))
		b.WriteString("\n")
	}

	b.WriteString("\n  ")
	b.WriteString(startupHelpStyle.Render("q quit"))
	b.WriteString("\n")
	return b.String()
}

func (m startupModel) renderError() string {
	return startupErrorStyle.Render(m.errMsg)
}

func openSelectionCmd(path string, statusCh chan downloader.DownloadStatus) tea.Cmd {
	return func() tea.Msg {
		defer close(statusCh)
		model, err := buildPlaybackModel(path, func(rawURL string) (ui.DownloadResult, error) {
			return downloadURLInline(rawURL, statusCh)
		})
		return startupResolvedMsg{model: model, err: err}
	}
}

func downloadURLInline(rawURL string, statusCh chan downloader.DownloadStatus) (ui.DownloadResult, error) {
	path, title, cleanup, err := downloader.Download(rawURL, func(status downloader.DownloadStatus) {
		select {
		case statusCh <- status:
		default:
		}
	})
	return ui.DownloadResult{
		Path:    path,
		Title:   title,
		Cleanup: cleanup,
		Err:     err,
	}, nil
}

func indentBlock(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		if lines[i] != "" {
			lines[i] = prefix + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

func startupIsQuit(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return true
	}
	return false
}

var (
	startupHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#888888"})
	startupStatusStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#BBBBBB"})
	startupHelpStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})
	startupErrorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#A00000", Dark: "#FF8080"})
)
