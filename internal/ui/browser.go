package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olivier-w/climp/internal/media"
)

// BrowserResult holds the outcome of the file browser.
type BrowserResult struct {
	Path      string
	Cancelled bool
}

type audioItem struct {
	name string
	ext  string
}

func (i audioItem) Title() string       { return i.name }
func (i audioItem) Description() string { return i.ext }
func (i audioItem) FilterValue() string { return i.name }

type urlItem struct{}

func (i urlItem) Title() string       { return "Play from URL..." }
func (i urlItem) Description() string { return "enter a URL to stream" }
func (i urlItem) FilterValue() string { return "url" }

// BrowserModel is the Bubbletea model for the file browser screen.
type BrowserModel struct {
	list    list.Model
	input   textinput.Model
	urlMode bool
	result  *BrowserResult
	err     error
}

// NewBrowser creates a new file browser model scanning the current directory.
func NewBrowser() BrowserModel {
	entries, err := os.ReadDir(".")
	if err != nil {
		return BrowserModel{err: fmt.Errorf("cannot read directory: %w", err)}
	}

	items := []list.Item{urlItem{}}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		switch ext {
		default:
			if !media.IsSupportedExt(ext) {
				continue
			}
			name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			items = append(items, audioItem{name: name, ext: ext})
		}
	}

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#FFFFFF"}).
		BorderLeftForeground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#AAAAAA"})
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"}).
		BorderLeftForeground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#AAAAAA"})

	l := list.New(items, delegate, 80, 20)
	l.Title = "climp"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.Styles.Title = headerStyle

	ti := textinput.New()
	ti.Placeholder = "https://..."
	ti.CharLimit = 2048
	ti.Width = 60

	return BrowserModel{list: l, input: ti}
}

// HasError returns true if the browser could not be initialized.
func (m BrowserModel) HasError() bool {
	return m.err != nil
}

// Error returns the initialization error, if any.
func (m BrowserModel) Error() error {
	return m.err
}

// Result returns the browser result after the program finishes.
func (m BrowserModel) Result() BrowserResult {
	if m.result != nil {
		return *m.result
	}
	return BrowserResult{Cancelled: true}
}

func (m BrowserModel) Init() tea.Cmd {
	return tea.SetWindowTitle("climp")
}

func (m BrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.urlMode {
		return m.updateURLInput(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Don't intercept keys when filtering
		if m.list.FilterState() == list.Filtering {
			break
		}

		switch msg.String() {
		case "enter":
			switch m.list.SelectedItem().(type) {
			case urlItem:
				m.urlMode = true
				m.input.Focus()
				return m, tea.Batch(textinput.Blink, tea.SetWindowTitle("climp — enter URL"))
			case audioItem:
				item := m.list.SelectedItem().(audioItem)
				path := item.name + item.ext
				m.result = &BrowserResult{Path: path}
				return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)
			}
		case "q", "esc", "ctrl+c":
			m.result = &BrowserResult{Cancelled: true}
			return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)
		}

	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height)
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m BrowserModel) updateURLInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			url := strings.TrimSpace(m.input.Value())
			if url != "" {
				m.result = &BrowserResult{Path: url}
				return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)
			}
		case "esc":
			m.urlMode = false
			m.input.Reset()
			m.input.Blur()
			return m, tea.SetWindowTitle("climp")
		case "ctrl+c":
			m.result = &BrowserResult{Cancelled: true}
			return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m BrowserModel) View() string {
	if m.urlMode {
		s := "\n"
		s += "  " + headerStyle.Render("climp") + "\n"
		s += "\n"
		s += "  " + statusStyle.Render("Enter URL:") + "\n"
		s += "  " + m.input.View() + "\n"
		s += "\n"
		s += "  " + helpStyle.Render("enter confirm  esc back  ctrl+c quit") + "\n"
		return s
	}
	return m.list.View()
}
