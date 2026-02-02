package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olivier-w/climp/internal/downloader"
	"github.com/olivier-w/climp/internal/player"
	"github.com/olivier-w/climp/internal/queue"
	"github.com/olivier-w/climp/internal/util"
	"github.com/olivier-w/climp/internal/visualizer"
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
	height     int
	quitting   bool
	repeatMode RepeatMode

	sourcePath  string    // temp file path (empty for local files)
	sourceTitle string    // title for saved filename
	saveMsg     string    // transient status message
	saveMsgTime time.Time // when saveMsg was set
	saving      bool      // conversion in progress

	visualizers []visualizer.Visualizer
	vizIndex    int
	vizEnabled  bool

	// Queue fields
	queue         *queue.Queue              // nil for single-track playback
	queueList     list.Model                // bubbles list for upcoming tracks display
	downloading   int                       // queue index being downloaded, -1 if none
	dlProgress    downloader.DownloadStatus // progress of background download
	transitioning bool                      // waiting for next track to finish downloading

	originalURL string // original URL for deferred playlist extraction

	// View caches — avoid re-rendering expensive sections every vizTick frame.
	headerCache    string // header + title + subtitle (changes on track change)
	vizCache       string // indented visualizer output (changes on vizTick)
	bottomCache    string // progress bar + status + queue + help (changes on tickMsg / discrete events)
	queueViewCache string // rendered list.Model.View() (changes on queue mutations / key navigation)
	dotsCache      string // pagination dots
}

// trackItem implements list.DefaultItem for queue display.
type trackItem struct {
	title string
	desc  string
}

func (t trackItem) FilterValue() string { return t.title }
func (t trackItem) Title() string       { return t.title }
func (t trackItem) Description() string { return t.desc }

// newQueueList creates a configured bubbles list for the queue display.
func newQueueList(width int) list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#FFFFFF"}).
		BorderLeftForeground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#AAAAAA"})
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#888888"}).
		BorderLeftForeground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#AAAAAA"})
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.
		Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#AAAAAA"})
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.
		Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})
	l := list.New(nil, delegate, width, 14)
	l.Title = "Up Next"
	l.Styles.Title = lipgloss.NewStyle().
		Background(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#AAAAAA"}).
		Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1A1A1A"}).
		Padding(0, 1)
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 1, 2)
	l.SetStatusBarItemName("track", "tracks")
	l.Styles.StatusBar = lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"}).
		Padding(0, 0, 0, 2)

	l.SetShowPagination(false)
	l.Styles.PaginationStyle = lipgloss.NewStyle().PaddingLeft(2)

	l.KeyMap.PrevPage.SetKeys("pgup")
	l.KeyMap.NextPage.SetKeys("pgdown")
	l.SetShowHelp(false)
	l.SetShowFilter(false)
	l.SetFilteringEnabled(false)
	return l
}

// syncQueueList rebuilds queueList items from the current queue state.
// It preserves the cursor position and only updates when items change.
func (m *Model) syncQueueList() {
	if m.queue == nil {
		return
	}
	currentIdx := m.queue.CurrentIndex()
	totalTracks := m.queue.Len()

	var items []list.Item
	for i := currentIdx + 1; i < totalTracks; i++ {
		t := m.queue.Track(i)
		if t == nil {
			continue
		}
		desc := fmt.Sprintf("track %d of %d", i+1, totalTracks)
		switch t.State {
		case queue.Downloading:
			desc = "downloading..."
		case queue.Failed:
			desc = "failed"
		case queue.Ready:
			desc = "ready"
		}
		items = append(items, trackItem{title: t.Title, desc: desc})
	}

	// Only update items if something changed, to preserve cursor/pagination.
	old := m.queueList.Items()
	changed := len(old) != len(items)
	if !changed {
		for i := range old {
			oi, ni := old[i].(trackItem), items[i].(trackItem)
			if oi.title != ni.title || oi.desc != ni.desc {
				changed = true
				break
			}
		}
	}
	if changed {
		sel := m.queueList.Index()
		m.queueList.SetItems(items)
		if sel < len(items) {
			m.queueList.Select(sel)
		}
	}
}

// rebuildQueueViewCache re-renders the queue list view and pagination dots,
// then rebuilds the bottom cache since the queue is embedded in it.
func (m *Model) rebuildQueueViewCache() {
	if m.queue == nil || m.queue.Len() <= 1 {
		m.queueViewCache = ""
		m.dotsCache = ""
		m.rebuildBottomCache()
		return
	}
	m.queueViewCache = m.queueList.View()
	p := m.queueList.Paginator
	if p.TotalPages > 1 {
		var sb strings.Builder
		for i := 0; i < p.TotalPages; i++ {
			if i == p.Page {
				sb.WriteString(activeDotStyle.Render("•"))
			} else {
				sb.WriteString(inactiveDotStyle.Render("•"))
			}
		}
		m.dotsCache = sb.String()
	} else {
		m.dotsCache = ""
	}
	m.rebuildBottomCache()
}

// rebuildHeaderCache rebuilds the cached header+title+subtitle section.
func (m *Model) rebuildHeaderCache() {
	var sb strings.Builder
	sb.WriteString("\n  ")
	sb.WriteString(headerStyle.Render("climp"))
	sb.WriteString("\n\n  ")
	sb.WriteString(titleStyle.Render(m.metadata.Title))
	sb.WriteByte('\n')

	if m.metadata.Artist != "" && m.metadata.Album != "" {
		sb.WriteString("  ")
		sb.WriteString(artistStyle.Render(fmt.Sprintf("%s - %s", m.metadata.Artist, m.metadata.Album)))
		sb.WriteByte('\n')
	} else if m.metadata.Artist != "" {
		sb.WriteString("  ")
		sb.WriteString(artistStyle.Render(m.metadata.Artist))
		sb.WriteByte('\n')
	} else if m.metadata.Album != "" {
		sb.WriteString("  ")
		sb.WriteString(artistStyle.Render(m.metadata.Album))
		sb.WriteByte('\n')
	}

	sb.WriteByte('\n')
	m.headerCache = sb.String()
}

// rebuildBottomCache rebuilds the cached section below the visualizer:
// progress bar, status line, queue display, and help text.
func (m *Model) rebuildBottomCache() {
	w := m.width
	if w < 30 {
		w = 50
	}

	var sb strings.Builder
	sb.Grow(256)

	// Progress bar or transitioning message
	if m.transitioning {
		sb.WriteString("  ")
		sb.WriteString(statusStyle.Render("Loading next track..."))
		sb.WriteByte('\n')
	} else {
		elapsedStr := timeStyle.Render(util.FormatDuration(m.elapsed))
		durationStr := timeStyle.Render(util.FormatDuration(m.duration))
		barWidth := w - len(util.FormatDuration(m.elapsed)) - len(util.FormatDuration(m.duration)) - 6
		if barWidth < 10 {
			barWidth = 10
		}
		bar := renderProgressBar(m.elapsed.Seconds(), m.duration.Seconds(), barWidth)
		sb.WriteString("  ")
		sb.WriteString(fmt.Sprintf("%s %s %s", elapsedStr, bar, durationStr))
		sb.WriteByte('\n')
	}

	sb.WriteByte('\n')

	statusIcon := "▶"
	statusText := "playing"
	if m.paused {
		statusIcon = "❚❚"
		statusText = "paused"
	}
	repeatIcon := m.repeatMode.Icon()
	volStr := renderVolumePercent(m.volume)

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

	sb.WriteString("  ")
	sb.WriteString(statusLeft)
	sb.WriteString(spaces(gap))
	sb.WriteString(statusRight)
	sb.WriteByte('\n')

	if m.saveMsg != "" {
		sb.WriteString("  ")
		sb.WriteString(helpStyle.Render(m.saveMsg))
		sb.WriteByte('\n')
	}

	// Queue display — use compact summary when visualizer is active to avoid
	// large terminal output that causes frame drops.
	if m.queue != nil && m.queue.Len() > 1 {
		sb.WriteByte('\n')
		if m.vizEnabled {
			next := m.queue.CurrentIndex() + 1
			if next < m.queue.Len() {
				t := m.queue.Track(next)
				title := "..."
				if t != nil {
					title = t.Title
				}
				sb.WriteString("  ")
				sb.WriteString(helpStyle.Render(fmt.Sprintf("Up next: %s  (%d/%d)", title, next+1, m.queue.Len())))
				sb.WriteByte('\n')
			}
		} else {
			sb.WriteString(m.queueViewCache)
			sb.WriteByte('\n')
			if m.dotsCache != "" {
				sb.WriteString("  ")
				sb.WriteString(m.dotsCache)
				sb.WriteByte('\n')
			}
		}
	}

	sb.WriteByte('\n')
	sb.WriteString("  ")
	sb.WriteString(helpStyle.Render(helpText(m.sourcePath != "", m.queue != nil)))
	sb.WriteByte('\n')

	m.bottomCache = sb.String()
}

// New creates a new Model. sourcePath is the temp file path for URL downloads
// (pass "" for local files to disable saving). originalURL is the URL passed on
// the command line (used for deferred playlist extraction; pass "" for local files).
func New(p *player.Player, meta player.Metadata, sourcePath, originalURL string) Model {
	m := Model{
		player:      p,
		metadata:    meta,
		duration:    p.Duration(),
		volume:      p.Volume(),
		sourcePath:  sourcePath,
		sourceTitle: meta.Title,
		visualizers: visualizer.Modes(),
		downloading: -1,
		originalURL: originalURL,
	}
	m.rebuildHeaderCache()
	m.rebuildBottomCache()
	return m
}

// NewWithQueue creates a Model with playlist queue support.
func NewWithQueue(p *player.Player, meta player.Metadata, sourcePath string, q *queue.Queue) Model {
	m := New(p, meta, sourcePath, "")
	m.queue = q
	m.queueList = newQueueList(50)
	m.syncQueueList()
	m.rebuildQueueViewCache()
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd(), checkDone(m.player), tea.SetWindowTitle(windowTitle(m.metadata.Title, false))}
	if m.queue != nil {
		next := m.queue.Next()
		if next != nil {
			idx := m.queue.CurrentIndex() + 1
			cmds = append(cmds, m.downloadTrackCmd(idx))
		}
	}
	if m.originalURL != "" && m.queue == nil {
		cmds = append(cmds, extractPlaylistCmd(m.originalURL))
	}
	return tea.Batch(cmds...)
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
			m.rebuildBottomCache()
			return m, tea.SetWindowTitle(windowTitle(m.metadata.Title, m.paused))
		case "left", "h":
			m.player.Seek(-5 * time.Second)
		case "right", "l":
			m.player.Seek(5 * time.Second)
		case "+", "=":
			m.player.AdjustVolume(0.05)
			m.volume = m.player.Volume()
			m.rebuildBottomCache()
		case "-":
			m.player.AdjustVolume(-0.05)
			m.volume = m.player.Volume()
			m.rebuildBottomCache()
		case "r":
			m.repeatMode = m.repeatMode.Next()
			m.rebuildBottomCache()
			return m, nil
		case "v":
			if !m.vizEnabled {
				m.vizEnabled = true
				m.vizIndex = 0
				m.rebuildBottomCache()
				return m, vizTickCmd()
			}
			m.vizIndex++
			if m.vizIndex >= len(m.visualizers) {
				m.vizEnabled = false
				m.vizIndex = 0
				m.vizCache = ""
				m.rebuildBottomCache()
			}
			return m, nil
		case "s":
			if m.sourcePath != "" && !m.saving {
				m.saving = true
				m.saveMsg = "Saving..."
				m.saveMsgTime = time.Now()
				m.rebuildBottomCache()
				src, title := m.sourcePath, m.sourceTitle
				return m, func() tea.Msg {
					destName, err := downloader.SaveFile(src, title)
					return fileSavedMsg{destName: destName, err: err}
				}
			}
			return m, nil
		case "n":
			if m.queue != nil {
				return m.skipToNext()
			}
		case "N", "p":
			if m.queue != nil {
				return m.skipToPrevious()
			}
		}
		// Forward navigation keys to queue list
		if m.queue != nil && m.queue.Len() > 1 {
			var cmd tea.Cmd
			m.queueList, cmd = m.queueList.Update(msg)
			m.rebuildQueueViewCache()
			return m, cmd
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
		m.rebuildBottomCache()
		return m, nil

	case tickMsg:
		m.elapsed = m.player.Position()
		m.volume = m.player.Volume()
		m.paused = m.player.Paused()
		if m.saveMsg != "" && time.Since(m.saveMsgTime) > 5*time.Second {
			m.saveMsg = ""
		}
		m.rebuildBottomCache()
		return m, tickCmd()

	case vizTickMsg:
		if m.vizEnabled && m.vizIndex < len(m.visualizers) {
			samples := m.player.Samples(2048)
			vizHeight := m.vizHeight()
			m.visualizers[m.vizIndex].Update(samples, m.effectiveWidth(), vizHeight)
			vizView := m.visualizers[m.vizIndex].View()
			if vizView != "" {
				var sb strings.Builder
				for _, line := range strings.Split(vizView, "\n") {
					sb.WriteString("  ")
					sb.WriteString(line)
					sb.WriteByte('\n')
				}
				sb.WriteByte('\n')
				m.vizCache = sb.String()
			} else {
				m.vizCache = ""
			}
			return m, vizTickCmd()
		}
		return m, nil

	case playbackEndedMsg:
		if m.repeatMode == RepeatOne {
			m.player.Restart()
			m.elapsed = 0
			return m, checkDone(m.player)
		}
		if m.queue != nil {
			return m.handleQueuePlaybackEnd()
		}
		m.elapsed = m.duration
		m.quitting = true
		m.player.Close()
		return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)

	case playlistExtractedMsg:
		return m.handlePlaylistExtracted(msg)

	case trackDownloadedMsg:
		return m.handleTrackDownloaded(msg)

	case trackDownloadProgressMsg:
		if msg.index == m.downloading {
			m.dlProgress = msg.status
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.queue != nil {
			m.queueList.SetWidth(msg.Width - 4)
			h := msg.Height - 14 // reserve: 8 lines above queue + 4 below (dots, help) + 2 margin
			if h < 6 {
				h = 6
			}
			m.queueList.SetHeight(h)
			m.rebuildQueueViewCache()
		}
		m.rebuildHeaderCache()
		m.rebuildBottomCache()
		return m, nil
	}

	return m, nil
}

// skipToNext advances to the next track if available.
func (m Model) skipToNext() (tea.Model, tea.Cmd) {
	next := m.queue.Next()
	if next == nil {
		if m.repeatMode == RepeatAll {
			// Wrap to beginning
			first := m.queue.Track(0)
			if first != nil && first.State == queue.Ready {
				m.cleanupOldTracks()
				m.queue.SetCurrentIndex(0)
				m.queue.SetTrackState(0, queue.Playing)
				return m.advanceToTrack(first)
			}
		}
		return m, nil
	}
	if next.State == queue.Ready || (next.State == queue.Done && next.Path != "") {
		m.cleanupOldTracks()
		m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Done)
		m.queue.Advance()
		m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Playing)
		return m.advanceToTrack(m.queue.Current())
	}
	if next.State == queue.Downloading {
		m.transitioning = true
		m.player.Close()
		m.syncQueueList()
		m.rebuildQueueViewCache()
		return m, nil
	}
	return m, nil
}

// skipToPrevious goes back to the previous track if it's still ready.
func (m Model) skipToPrevious() (tea.Model, tea.Cmd) {
	idx := m.queue.CurrentIndex()
	if idx <= 0 {
		return m, nil
	}
	prev := m.queue.Track(idx - 1)
	if prev == nil || prev.Path == "" {
		return m, nil
	}
	m.queue.SetTrackState(idx, queue.Done)
	m.queue.Previous()
	m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Playing)
	return m.advanceToTrack(m.queue.Current())
}

// handleQueuePlaybackEnd handles playback end when a queue is present.
func (m Model) handleQueuePlaybackEnd() (tea.Model, tea.Cmd) {
	next := m.queue.Next()
	if next != nil {
		if next.State == queue.Ready {
			m.cleanupOldTracks()
			m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Done)
			m.queue.Advance()
			m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Playing)
			return m.advanceToTrack(m.queue.Current())
		}
		if next.State == queue.Downloading {
			m.transitioning = true
			m.player.Close()
			m.syncQueueList()
			m.rebuildQueueViewCache()
			return m, nil
		}
	}

	// No next track
	if m.repeatMode == RepeatAll && m.queue.Len() > 0 {
		first := m.queue.Track(0)
		if first != nil && first.Path != "" {
			m.cleanupOldTracks()
			m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Done)
			m.queue.SetCurrentIndex(0)
			m.queue.SetTrackState(0, queue.Playing)
			return m.advanceToTrack(m.queue.Current())
		}
	}

	m.elapsed = m.duration
	m.quitting = true
	m.player.Close()
	return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)
}

// handleTrackDownloaded processes a completed background download.
func (m Model) handleTrackDownloaded(msg trackDownloadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.queue.SetTrackState(msg.index, queue.Failed)
		if msg.index == m.downloading {
			m.downloading = -1
		}
		// If we were waiting for this track, move on
		if m.transitioning {
			m.transitioning = false
			// Try to skip past the failed track
			m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Done)
			if m.queue.Advance() {
				m.syncQueueList()
				m.rebuildQueueViewCache()
				// Start downloading this new next track
				return m, m.startNextDownload()
			}
			m.quitting = true
			return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)
		}
		m.syncQueueList()
		m.rebuildQueueViewCache()
		return m, m.startNextDownload()
	}

	m.queue.SetTrackPath(msg.index, msg.path)
	m.queue.SetTrackCleanup(msg.index, msg.cleanup)
	m.queue.SetTrackState(msg.index, queue.Ready)
	if msg.index == m.downloading {
		m.downloading = -1
	}

	var cmds []tea.Cmd

	if m.transitioning && msg.index == m.queue.CurrentIndex()+1 {
		m.transitioning = false
		m.cleanupOldTracks()
		m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Done)
		m.queue.Advance()
		m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Playing)
		track := m.queue.Current()
		m.metadata = player.Metadata{Title: track.Title}
		m.sourceTitle = track.Title
		m.sourcePath = track.Path

		var err error
		m.player, err = player.New(track.Path)
		if err != nil {
			m.quitting = true
			return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)
		}
		m.elapsed = 0
		m.duration = m.player.Duration()
		m.volume = m.player.Volume()
		m.paused = false
		m.rebuildHeaderCache()

		cmds = append(cmds, checkDone(m.player), tickCmd(), tea.SetWindowTitle(windowTitle(m.metadata.Title, false)))
	}

	// Start downloading next undownloaded track
	cmds = append(cmds, m.startNextDownload())

	m.syncQueueList()
	m.rebuildQueueViewCache()

	return m, tea.Batch(cmds...)
}

// handlePlaylistExtracted builds the queue from background extraction results.
func (m Model) handlePlaylistExtracted(msg playlistExtractedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil || len(msg.entries) <= 1 {
		// Single video or extraction failed — stay in single-track mode.
		return m, nil
	}

	// Build queue tracks from playlist entries.
	tracks := make([]queue.Track, len(msg.entries))
	for i, e := range msg.entries {
		tracks[i] = queue.Track{
			ID:    e.ID,
			Title: e.Title,
			URL:   downloader.VideoURL(e.ID),
			State: queue.Pending,
		}
	}

	// Mark track 0 as Playing with the current playback info.
	tracks[0].State = queue.Playing
	tracks[0].Path = m.sourcePath
	tracks[0].Title = m.sourceTitle

	m.queue = queue.New(tracks)
	w := m.width
	if w < 30 {
		w = 50
	}
	m.queueList = newQueueList(w - 4)
	h := m.height - 14
	if h < 6 {
		h = 6
	}
	m.queueList.SetHeight(h)
	m.syncQueueList()
	m.rebuildQueueViewCache()
	m.originalURL = "" // extraction done

	// Start downloading the next track.
	return m, m.startNextDownload()
}

// advanceToTrack switches playback to the given track.
func (m Model) advanceToTrack(track *queue.Track) (tea.Model, tea.Cmd) {
	m.player.Close()

	m.metadata = player.Metadata{Title: track.Title}
	m.sourceTitle = track.Title
	m.sourcePath = track.Path

	var err error
	m.player, err = player.New(track.Path)
	if err != nil {
		m.quitting = true
		return m, tea.Sequence(tea.SetWindowTitle(""), tea.Quit)
	}

	m.elapsed = 0
	m.duration = m.player.Duration()
	m.volume = m.player.Volume()
	m.paused = false
	m.transitioning = false
	m.rebuildHeaderCache()
	m.syncQueueList()
	m.rebuildQueueViewCache()

	cmds := []tea.Cmd{
		checkDone(m.player),
		tickCmd(),
		tea.SetWindowTitle(windowTitle(m.metadata.Title, false)),
		m.startNextDownload(),
	}

	return m, tea.Batch(cmds...)
}

// extractPlaylistCmd runs playlist extraction in the background.
func extractPlaylistCmd(url string) tea.Cmd {
	return func() tea.Msg {
		entries, err := downloader.ExtractPlaylist(url)
		return playlistExtractedMsg{entries: entries, err: err}
	}
}

// downloadTrackCmd creates a command to download a track by queue index.
func (m Model) downloadTrackCmd(index int) tea.Cmd {
	track := m.queue.Track(index)
	if track == nil {
		return nil
	}
	m.queue.SetTrackState(index, queue.Downloading)
	m.downloading = index
	m.dlProgress = downloader.DownloadStatus{}

	url := downloader.VideoURL(track.ID)
	return func() tea.Msg {
		path, title, cleanup, err := downloader.Download(url, nil)
		if title == "" {
			title = track.Title
		}
		return trackDownloadedMsg{
			index:   index,
			path:    path,
			title:   title,
			cleanup: cleanup,
			err:     err,
		}
	}
}

// startNextDownload downloads only the immediate next track after current.
func (m Model) startNextDownload() tea.Cmd {
	if m.queue == nil || m.downloading >= 0 {
		return nil
	}
	next := m.queue.CurrentIndex() + 1
	if next >= m.queue.Len() {
		return nil
	}
	t := m.queue.Track(next)
	if t != nil && t.State == queue.Pending {
		return m.downloadTrackCmd(next)
	}
	return nil
}

// cleanupOldTracks frees disk space for tracks 2+ positions behind current.
func (m Model) cleanupOldTracks() {
	cur := m.queue.CurrentIndex()
	for i := 0; i < cur-1; i++ {
		t := m.queue.Track(i)
		if t != nil && t.Cleanup != nil {
			t.Cleanup()
			m.queue.SetTrackCleanup(i, nil)
		}
	}
}

func (m Model) effectiveWidth() int {
	w := m.width
	if w < 30 {
		w = 50
	}
	return w - 4 // account for left margin
}

func (m Model) vizHeight() int {
	h := m.height - 14 // reserve space for other UI elements
	if h < 2 {
		h = 2
	}
	if h > 8 {
		h = 8
	}
	return h
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	return m.headerCache + m.vizCache + m.bottomCache
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
