package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olivier-w/climp/internal/downloader"
)

// DownloadResult holds the outcome of a download operation.
type DownloadResult struct {
	Path    string
	Title   string
	Cleanup func()
	Err     error
}

// downloadStatusMsg wraps a DownloadStatus for the Bubbletea message loop.
type downloadStatusMsg downloader.DownloadStatus

// downloadDoneMsg signals the download goroutine finished.
type downloadDoneMsg struct {
	path    string
	title   string
	cleanup func()
	err     error
}

// DownloadModel is the Bubbletea model for the download screen.
type DownloadModel struct {
	url      string
	spinner  spinner.Model
	progress progress.Model
	status   downloader.DownloadStatus
	result   *DownloadResult
	width    int
	quitting bool
	statusCh chan downloader.DownloadStatus
}

// NewDownload creates a new download model for the given URL.
func NewDownload(url string) DownloadModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#AAAAAA"})

	p := progress.New(
		progress.WithScaledGradient("#FF8C00", "#FF5F1F"),
		progress.WithoutPercentage(),
	)

	return DownloadModel{
		url:      url,
		spinner:  s,
		progress: p,
		status:   downloader.DownloadStatus{Phase: "fetching", Percent: -1},
		statusCh: make(chan downloader.DownloadStatus, 64),
	}
}

// Result returns the download result after the program finishes.
func (m DownloadModel) Result() DownloadResult {
	if m.result != nil {
		return *m.result
	}
	return DownloadResult{Err: fmt.Errorf("download was cancelled")}
}

func (m DownloadModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.startDownload(),
		m.waitForStatus(),
	)
}

func (m DownloadModel) startDownload() tea.Cmd {
	return func() tea.Msg {
		path, title, cleanup, err := downloader.Download(m.url, func(s downloader.DownloadStatus) {
			m.statusCh <- s
		})
		close(m.statusCh)
		return downloadDoneMsg{path: path, title: title, cleanup: cleanup, err: err}
	}
}

func (m DownloadModel) waitForStatus() tea.Cmd {
	return func() tea.Msg {
		s, ok := <-m.statusCh
		if !ok {
			return nil
		}
		return downloadStatusMsg(s)
	}
}

func (m DownloadModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if isQuit(msg) {
			m.quitting = true
			m.result = &DownloadResult{Err: fmt.Errorf("download was cancelled")}
			return m, tea.Quit
		}

	case downloadStatusMsg:
		m.status = downloader.DownloadStatus(msg)
		return m, m.waitForStatus()

	case downloadDoneMsg:
		m.result = &DownloadResult{
			Path:    msg.path,
			Title:   msg.title,
			Cleanup: msg.cleanup,
			Err:     msg.err,
		}
		m.quitting = true
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.progress.Width = msg.Width - 8
		if m.progress.Width < 20 {
			m.progress.Width = 20
		}
		if m.progress.Width > 60 {
			m.progress.Width = 60
		}
		return m, nil
	}

	return m, nil
}

func (m DownloadModel) View() string {
	if m.quitting {
		return ""
	}

	header := headerStyle.Render("climp")

	lines := "\n"
	lines += "  " + header + "\n"
	lines += "\n"

	switch m.status.Phase {
	case "downloading":
		lines += "  " + statusStyle.Render("Downloading...") + "\n"
		if m.status.Percent >= 0 {
			lines += "  " + m.progress.ViewAs(m.status.Percent) + fmt.Sprintf("  %.0f%%", m.status.Percent*100) + "\n"
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
				lines += "  " + helpStyle.Render(detail) + "\n"
			}
		} else {
			lines += "  " + m.spinner.View() + " " + helpStyle.Render("Downloading...") + "\n"
		}

	case "converting":
		lines += "  " + m.spinner.View() + " " + statusStyle.Render("Converting...") + "\n"

	default: // "fetching" or initial
		lines += "  " + m.spinner.View() + " " + statusStyle.Render("Fetching info...") + "\n"
	}

	lines += "\n"
	return lines
}
