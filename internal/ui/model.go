package ui

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/olivier-w/climp/internal/downloader"
	"github.com/olivier-w/climp/internal/player"
	"github.com/olivier-w/climp/internal/queue"
	"github.com/olivier-w/climp/internal/util"
	"github.com/olivier-w/climp/internal/visualizer"
)

// Dirty flags for cache invalidation.
type dirtyFlags uint8

const (
	dirtyHeader dirtyFlags = 1 << iota
	dirtyMid
	dirtyQueue
	dirtyBottom
)

const maxVizHeight = 8 // maximum lines for the visualizer

// Model is the Bubbletea model for the climp TUI.
type Model struct {
	player      *player.Player
	metadata    player.Metadata
	elapsed     time.Duration
	duration    time.Duration
	volume      float64
	paused      bool
	width       int
	height      int
	quitting    bool
	repeatMode  RepeatMode
	shuffleMode ShuffleMode
	speed       player.SpeedMode

	sourcePath  string    // temp file path (empty for local files)
	sourceTitle string    // title for saved filename
	saveMsg     string    // transient status message
	saveMsgTime time.Time // when saveMsg was set
	saving      bool      // conversion in progress
	cleanup     func()    // optional cleanup for single-track temp files

	visualizers []visualizer.Visualizer
	vizIndex    int
	vizEnabled  bool

	// Queue fields
	queue            *queue.Queue // nil for single-track playback
	queueList        list.Model   // bubbles list for upcoming tracks display
	downloading      int          // queue index being downloaded, -1 if none
	transitioning    bool         // waiting for a track to finish downloading
	transitionTarget int          // queue index we're waiting to play (-1 if not jumping)

	originalURL  string // original URL for deferred playlist extraction
	playlistName string // queue label shown in header for playlist mode

	keys keyMap
	help help.Model

	// View caches — avoid re-rendering expensive sections every vizTick frame.
	headerCache    string // title + subtitle (changes on track change)
	midCache       string // progress bar + status line (changes on tickMsg)
	vizCache       string // indented visualizer output (changes on vizTick)
	bottomCache    string // queue + help text (changes on discrete events)
	queueViewCache string // rendered list.Model.View() (changes on queue mutations / key navigation)
	dotsCache      string // pagination dots

	dirty dirtyFlags // tracks which caches need rebuilding
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
	l.SetShowStatusBar(false)

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
// Items are ordered: tracks after current first, then tracks before current
// (wrapping around), so the "up next" track is always first.
func (m *Model) syncQueueList() {
	if m.queue == nil {
		return
	}
	currentIdx := m.queue.CurrentIndex()
	totalTracks := m.queue.Len()

	var items []list.Item
	// Tracks after current
	for i := currentIdx + 1; i < totalTracks; i++ {
		t := m.queue.Track(i)
		if t == nil {
			continue
		}
		items = append(items, m.trackToItem(t, i, totalTracks))
	}
	// Tracks before current (wrap-around)
	for i := 0; i < currentIdx; i++ {
		t := m.queue.Track(i)
		if t == nil {
			continue
		}
		items = append(items, m.trackToItem(t, i, totalTracks))
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

// trackToItem converts a queue track to a list item for display.
func (m *Model) trackToItem(t *queue.Track, i, totalTracks int) trackItem {
	desc := fmt.Sprintf("track %d of %d", i+1, totalTracks)
	switch t.State {
	case queue.Downloading:
		desc = "downloading..."
	case queue.Failed:
		desc = "failed"
	case queue.Ready:
		desc = "ready"
	case queue.Done:
		desc = "played"
	}
	title := t.Title
	if title == "" {
		title = fmt.Sprintf("Track %d", i+1)
	}
	return trackItem{title: title, desc: desc}
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
	statusBarStyle := lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"})

	queueView := m.queueList.View()

	// Build custom header: playlist label (bold) + track count (dim), single line
	label := normalizePlaylistLabel(m.playlistName)
	w := m.effectiveWidth()
	label = truncateLabel(label, w)

	n := m.queue.Len()
	trackWord := "tracks"
	if n == 1 {
		trackWord = "track"
	}
	headerLine := "  " + headerStyle.Render(label) + "  " + statusBarStyle.Render(fmt.Sprintf("%d %s", n, trackWord))

	// Insert below the "Up Next" title bar (first 2 lines: title + blank padding).
	// Add a blank line after header to separate from the list items.
	lines := strings.SplitN(queueView, "\n", 3)
	if len(lines) >= 3 {
		m.queueViewCache = lines[0] + "\n" + lines[1] + "\n" + headerLine + "\n\n" + lines[2]
	} else {
		m.queueViewCache = queueView + "\n" + headerLine
	}
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

// rebuildHeaderCache rebuilds the cached title+subtitle section.
func (m *Model) rebuildHeaderCache() {
	var sb strings.Builder
	sb.WriteString("\n  ")
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

// rebuildMidCache rebuilds the cached progress bar and status line section.
func (m *Model) rebuildMidCache() {
	w := m.width
	if w < 30 {
		w = 50
	}
	// Right-aligned rows in this section should share the same visual edge
	// (computed from w) so progress-duration, LIVE, and volume stay flush.

	var sb strings.Builder
	sb.Grow(256)

	// Progress bar or transitioning message
	if m.transitioning {
		sb.WriteString("  ")
		sb.WriteString(statusStyle.Render("Loading next track..."))
		sb.WriteByte('\n')
	} else {
		elapsedStr := timeStyle.Render(util.FormatDuration(m.elapsed))
		if m.player != nil && !m.player.CanSeek() {
			liveStr := statusStyle.Render("LIVE")
			// Right-align LIVE to the row edge, matching the seek row's right anchor.
			gap := w - lipgloss.Width(util.FormatDuration(m.elapsed)) - lipgloss.Width("LIVE") - 4
			if gap < 2 {
				gap = 2
			}
			sb.WriteString("  ")
			sb.WriteString(elapsedStr)
			sb.WriteString(spaces(gap))
			sb.WriteString(liveStr)
			sb.WriteByte('\n')
		} else {
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
	}

	sb.WriteByte('\n')

	statusIcon := "▶"
	statusText := "playing"
	if m.paused {
		statusIcon = "❚❚"
		statusText = "paused"
	}
	repeatIcon := m.repeatMode.Icon()
	speedLabel := m.speed.Label()
	shuffleIcon := m.shuffleMode.Icon()
	volStr := renderVolumePercent(m.volume)

	leftText := fmt.Sprintf("%s  %s", statusIcon, statusText)
	if repeatIcon != "" {
		leftText += "  " + repeatIcon
	}
	if speedLabel != "" {
		leftText += "  " + speedLabel
	}
	if shuffleIcon != "" {
		leftText += "  " + shuffleIcon
	}
	if m.vizEnabled && m.vizIndex < len(m.visualizers) {
		leftText += "  viz:" + m.visualizers[m.vizIndex].Name()
	}
	statusLeft := statusStyle.Render(leftText)
	statusRight := statusStyle.Render(volStr)
	gap := w - lipgloss.Width(leftText) - lipgloss.Width(volStr) - 4
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

	sb.WriteByte('\n')
	if m.queue != nil && m.queue.Len() > 1 {
		sb.WriteByte('\n')
	}
	m.midCache = sb.String()
}

// rebuildBottomCache rebuilds the cached queue display and help text section.
func (m *Model) rebuildBottomCache() {
	var sb strings.Builder
	sb.Grow(256)

	// Queue display — always show full queue list
	if m.queue != nil && m.queue.Len() > 1 {
		sb.WriteString(m.queueViewCache)
		sb.WriteByte('\n')
		if m.dotsCache != "" {
			sb.WriteString("  ")
			sb.WriteString(m.dotsCache)
			sb.WriteByte('\n')
		}
	}

	canSeek := m.player != nil && m.player.CanSeek()
	m.keys.updateEnabled(m.sourcePath != "", m.queue != nil, canSeek)
	sb.WriteByte('\n')
	helpView := m.help.View(m.keys)
	for i, line := range strings.Split(helpView, "\n") {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString("  ")
		sb.WriteString(line)
	}
	sb.WriteByte('\n')

	m.bottomCache = sb.String()
}

// invalidate marks the given caches as needing rebuild.
func (m *Model) invalidate(flags dirtyFlags) {
	m.dirty |= flags
}

// flushCaches rebuilds any caches marked dirty, then clears the flags.
func (m *Model) flushCaches() {
	if m.dirty == 0 {
		return
	}
	if m.dirty&dirtyQueue != 0 {
		m.syncQueueList()
		m.rebuildQueueViewCache() // also rebuilds bottom
		m.dirty &^= dirtyBottom   // bottom was rebuilt by rebuildQueueViewCache
	}
	if m.dirty&dirtyHeader != 0 {
		m.rebuildHeaderCache()
	}
	if m.dirty&dirtyMid != 0 {
		m.rebuildMidCache()
	}
	if m.dirty&dirtyBottom != 0 {
		m.rebuildBottomCache()
	}
	m.dirty = 0
}

// New creates a new Model. sourcePath is the temp file path for URL downloads
// (pass "" for local files to disable saving). originalURL is the URL passed on
// the command line (used for deferred playlist extraction; pass "" for local files).
func New(p *player.Player, meta player.Metadata, sourcePath, originalURL string, cleanup func()) Model {
	keys := newKeyMap()
	canSeek := p != nil && p.CanSeek()
	keys.updateEnabled(sourcePath != "", false, canSeek)
	h := help.New()
	h.ShortSeparator = "  "
	h.Styles.ShortKey = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})
	h.Styles.ShortDesc = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})
	h.Styles.FullKey = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})
	h.Styles.FullDesc = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})
	h.Styles.FullSeparator = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})
	h.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#999999", Dark: "#666666"})
	m := Model{
		player:           p,
		metadata:         meta,
		duration:         p.Duration(),
		volume:           p.Volume(),
		sourcePath:       sourcePath,
		sourceTitle:      meta.Title,
		cleanup:          cleanup,
		visualizers:      visualizer.Modes(),
		downloading:      -1,
		transitionTarget: -1,
		originalURL:      originalURL,
		keys:             keys,
		help:             h,
	}
	m.rebuildHeaderCache()
	m.rebuildMidCache()
	m.rebuildBottomCache()
	return m
}

// NewWithQueue creates a Model with playlist queue support.
func NewWithQueue(p *player.Player, meta player.Metadata, sourcePath string, q *queue.Queue, playlistName string) Model {
	m := New(p, meta, sourcePath, "", nil)
	m.queue = q
	m.playlistName = normalizePlaylistLabel(playlistName)
	m.queueList = newQueueList(50)
	m.syncQueueList()
	m.rebuildQueueViewCache()
	m.rebuildHeaderCache()
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd(), checkDone(m.player), waitForLiveTitle(m.player), tea.SetWindowTitle(windowTitle(m.metadata.Title, false))}
	if m.queue != nil {
		next := m.queue.Next()
		if next != nil && next.State == queue.Pending {
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
		return playbackEndedMsg{player: p}
	}
}

func waitForLiveTitle(p *player.Player) tea.Cmd {
	if p == nil {
		return nil
	}
	updates := p.TitleUpdates()
	if updates == nil {
		return nil
	}
	return func() tea.Msg {
		title, ok := <-updates
		if !ok {
			return nil
		}
		return liveTitleUpdatedMsg{player: p, title: title}
	}
}

func (m *Model) shutdown() tea.Cmd {
	if m.player != nil {
		m.player.Close()
		m.player = nil
	}
	if m.cleanup != nil {
		m.cleanup()
		m.cleanup = nil
	}
	if m.queue != nil {
		m.queue.CleanupAll()
	}
	return tea.Sequence(tea.SetWindowTitle(""), tea.Quit)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := m.handleMsg(msg)
	m.flushCaches()
	return m, cmd
}

func (m Model) handleMsg(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionRelease && m.player != nil {
			m.player.TogglePause()
			m.paused = m.player.Paused()
			m.invalidate(dirtyMid)
			return m, tea.SetWindowTitle(windowTitle(m.metadata.Title, m.paused))
		}
		return m, nil
	case tea.KeyMsg:
		if isQuit(msg) {
			m.quitting = true
			return m, m.shutdown()
		}
		if m.player == nil {
			return m, nil
		}
		switch msg.String() {
		case " ":
			m.player.TogglePause()
			m.paused = m.player.Paused()
			m.invalidate(dirtyMid)
			return m, tea.SetWindowTitle(windowTitle(m.metadata.Title, m.paused))
		case "left", "h":
			if m.player.CanSeek() {
				m.player.Seek(-5 * time.Second)
			}
		case "right", "l":
			if m.player.CanSeek() {
				m.player.Seek(5 * time.Second)
			}
		case "+", "=":
			m.player.AdjustVolume(0.05)
			m.volume = m.player.Volume()
			m.invalidate(dirtyMid)
		case "-":
			m.player.AdjustVolume(-0.05)
			m.volume = m.player.Volume()
			m.invalidate(dirtyMid)
		case "r":
			m.repeatMode = m.repeatMode.Next()
			m.invalidate(dirtyMid)
			return m, nil
		case "x":
			m.speed = m.player.CycleSpeed()
			m.invalidate(dirtyMid)
			return m, nil
		case "v":
			if !m.vizEnabled {
				m.vizEnabled = true
				m.vizIndex = 0
				m.updateQueueHeight()
				m.invalidate(dirtyQueue)
				return m, vizTickCmd()
			}
			m.vizIndex++
			if m.vizIndex >= len(m.visualizers) {
				m.vizEnabled = false
				m.vizIndex = 0
				m.vizCache = ""
				m.updateQueueHeight()
				m.invalidate(dirtyQueue)
			}
			return m, nil
		case "s":
			if m.sourcePath != "" && !m.saving {
				m.saving = true
				m.saveMsg = "Saving..."
				m.saveMsgTime = time.Now()
				m.invalidate(dirtyMid)
				src, title := m.sourcePath, m.sourceTitle
				return m, func() tea.Msg {
					destName, err := downloader.SaveFile(src, title)
					return fileSavedMsg{destName: destName, err: err}
				}
			}
			return m, nil
		case "z":
			if m.queue != nil && m.queue.Len() > 1 {
				m.shuffleMode = m.shuffleMode.Toggle()
				if m.shuffleMode == ShuffleOn {
					m.queue.EnableShuffle()
				} else {
					m.queue.DisableShuffle()
				}
				m.invalidate(dirtyMid)
				return m, nil
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
		case "enter":
			if m.queue != nil && m.queue.Len() > 1 {
				return m.jumpToSelected()
			}
		case "backspace", "delete":
			if m.queue != nil && m.queue.Len() > 1 {
				return m.removeSelected()
			}
		case "?":
			m.help.ShowAll = !m.help.ShowAll
			m.invalidate(dirtyBottom)
			return m, nil

		}
		// Forward navigation keys to queue list
		if m.queue != nil && m.queue.Len() > 1 {
			var cmd tea.Cmd
			m.queueList, cmd = m.queueList.Update(msg)
			m.invalidate(dirtyQueue)
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
		m.invalidate(dirtyMid | dirtyBottom)
		return m, nil

	case liveTitleUpdatedMsg:
		if msg.player != m.player {
			return m, nil
		}
		next := waitForLiveTitle(m.player)
		if msg.title == "" || msg.title == m.metadata.Title {
			return m, next
		}
		m.metadata.Title = msg.title
		m.invalidate(dirtyHeader)
		return m, tea.Batch(next, tea.SetWindowTitle(windowTitle(m.metadata.Title, m.paused)))

	case tickMsg:
		if m.player == nil {
			return m, nil
		}
		m.elapsed = m.player.Position()
		m.volume = m.player.Volume()
		m.paused = m.player.Paused()
		if m.saveMsg != "" && time.Since(m.saveMsgTime) > 5*time.Second {
			m.saveMsg = ""
		}
		m.invalidate(dirtyMid)
		return m, tickCmd()

	case vizTickMsg:
		if m.player == nil {
			return m, nil
		}
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
		// Ignore stale done notifications from a player instance that's no longer current.
		if msg.player != m.player {
			return m, nil
		}
		if m.player == nil {
			return m, nil
		}
		if m.repeatMode == RepeatOne && m.player.CanSeek() {
			m.player.Restart()
			m.elapsed = 0
			return m, checkDone(m.player)
		}
		if m.queue != nil {
			return m.handleQueuePlaybackEnd()
		}
		m.elapsed = m.duration
		m.quitting = true
		return m, m.shutdown()

	case trackFailedMsg:
		if msg.err != nil {
			m.saveMsg = fmt.Sprintf("Track failed: %v", msg.err)
		} else {
			m.saveMsg = "Track failed"
		}
		m.saveMsgTime = time.Now()
		m.invalidate(dirtyMid)
		if m.queue != nil {
			return m.skipToNext()
		}
		return m, nil

	case playlistExtractedMsg:
		return m.handlePlaylistExtracted(msg)

	case trackDownloadedMsg:
		return m.handleTrackDownloaded(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width
		if m.queue != nil {
			m.queueList.SetWidth(msg.Width - 4)
			m.updateQueueHeight()
		}
		m.invalidate(dirtyHeader | dirtyMid | dirtyQueue | dirtyBottom)
		return m, nil
	}

	return m, nil
}

// findNextPlayable scans forward from the current position for the next non-Failed track.
// It advances the queue position past any Failed tracks. If wrap is true (RepeatAll),
// it wraps around and re-shuffles if needed. Returns the track, its original index,
// and whether one was found.
func (m *Model) findNextPlayable(wrap bool) (*queue.Track, int, bool) {
	for range m.queue.Len() {
		var next *queue.Track
		var nextIdx int
		if m.queue.IsShuffled() {
			next = m.queue.NextShuffled()
			nextIdx = m.queue.NextDownloadIndex()
		} else {
			next = m.queue.Next()
			nextIdx = m.queue.CurrentIndex() + 1
		}

		if next == nil {
			if !wrap {
				return nil, -1, false
			}
			// Wrap around — re-shuffle or scan from index 0
			wrap = false // only wrap once
			// Reset cleaned-up URL tracks to Pending so they can be re-downloaded
			for i := 0; i < m.queue.Len(); i++ {
				t := m.queue.Track(i)
				if t != nil && t.State == queue.Done && t.URL != "" && t.Cleanup == nil && !downloader.IsLiveURL(t.URL) {
					m.queue.SetTrackState(i, queue.Pending)
					m.queue.SetTrackPath(i, "")
				}
			}
			if m.queue.IsShuffled() {
				m.queue.EnableShuffle()
			} else {
				m.queue.WrapToStart()
			}
			continue
		}

		if next.State == queue.Failed {
			// Skip past this failed track
			if m.queue.IsShuffled() {
				m.queue.AdvanceShuffle()
			} else {
				m.queue.Advance()
			}
			continue
		}

		return next, nextIdx, true
	}
	return nil, -1, false
}

// advanceAndPlay marks the current track Done, advances the queue (shuffle-aware),
// marks the new track Playing, and switches playback to it.
func (m Model) advanceAndPlay() (Model, tea.Cmd) {
	m.cleanupOldTracks()
	prevIdx := m.queue.CurrentIndex()
	if prevIdx < 0 && !m.queue.IsShuffled() && m.queue.Len() > 0 {
		// Repeat-all wrap sets current to -1 so Next() can return index 0.
		// Treat the previous track as the last queue item.
		prevIdx = m.queue.Len() - 1
	}
	if prevIdx >= 0 {
		m.queue.SetTrackState(prevIdx, queue.Done)
	}
	if m.queue.IsShuffled() {
		m.queue.AdvanceShuffle()
	} else {
		m.queue.Advance()
	}
	m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Playing)
	return m.advanceToTrack(m.queue.Current())
}

// enterTransitioning stops playback and waits for the track at nextIdx to finish downloading.
func (m Model) enterTransitioning(nextIdx int) (Model, tea.Cmd) {
	m.transitioning = true
	m.transitionTarget = nextIdx
	if m.player != nil {
		m.player.Close()
	}
	m.invalidate(dirtyQueue)
	return m, nil
}

// skipToNext advances to the next playable track, skipping over Failed tracks.
func (m Model) skipToNext() (Model, tea.Cmd) {
	next, nextIdx, found := m.findNextPlayable(m.repeatMode == RepeatAll)
	if !found {
		return m, nil
	}

	if next.State == queue.Ready || (next.State == queue.Done && (next.Path != "" || downloader.IsLiveURL(next.URL))) {
		return m.advanceAndPlay()
	}
	if next.State == queue.Downloading || next.State == queue.Pending {
		m, cmd := m.enterTransitioning(nextIdx)
		if next.State == queue.Pending {
			return m, m.downloadTrackCmd(nextIdx)
		}
		return m, cmd
	}
	return m, nil
}

// jumpToSelected jumps to the track currently highlighted in the queue list.
func (m Model) jumpToSelected() (Model, tea.Cmd) {
	sel := m.queueList.Index()
	targetIdx := m.listIndexToQueueIndex(sel)
	if targetIdx < 0 || targetIdx >= m.queue.Len() || targetIdx == m.queue.CurrentIndex() {
		return m, nil
	}
	target := m.queue.Track(targetIdx)
	if target == nil || target.State == queue.Failed {
		return m, nil
	}

	// Track is ready to play immediately.
	if target.State == queue.Ready || (target.State == queue.Done && (target.Path != "" || downloader.IsLiveURL(target.URL))) {
		m.cleanupOldTracks()
		m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Done)
		m.queue.SetCurrentIndex(targetIdx)
		m.queue.SetTrackState(targetIdx, queue.Playing)
		return m.advanceToTrack(m.queue.Current())
	}

	// Track needs downloading — enter transitioning state.
	if target.State == queue.Pending || target.State == queue.Downloading {
		m.transitioning = true
		m.transitionTarget = targetIdx
		m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Done)
		if m.player != nil {
			m.player.Close()
		}
		m.invalidate(dirtyQueue)
		// If pending, kick off its download now.
		if target.State == queue.Pending {
			return m, m.downloadTrackCmd(targetIdx)
		}
		return m, nil
	}

	return m, nil
}

// listIndexToQueueIndex maps a queue list selection index back to the real queue index.
// The list is ordered: tracks after current, then tracks before current.
func (m Model) listIndexToQueueIndex(sel int) int {
	currentIdx := m.queue.CurrentIndex()
	afterCount := m.queue.Len() - currentIdx - 1
	if sel < afterCount {
		return currentIdx + 1 + sel
	}
	return sel - afterCount
}

// removeSelected removes the track currently highlighted in the queue list.
func (m Model) removeSelected() (Model, tea.Cmd) {
	sel := m.queueList.Index()
	targetIdx := m.listIndexToQueueIndex(sel)
	if targetIdx < 0 || targetIdx >= m.queue.Len() {
		return m, nil
	}
	if targetIdx == m.queue.CurrentIndex() {
		m.saveMsg = "Cannot remove currently playing track"
		m.saveMsgTime = time.Now()
		m.invalidate(dirtyMid)
		return m, nil
	}
	if !m.queue.Remove(targetIdx) {
		return m, nil
	}
	// Sync immediately so cursor adjustment below sees updated items.
	m.syncQueueList()
	if m.queue.Len() > 1 {
		// Adjust cursor if it's now past the end of the list
		if sel >= len(m.queueList.Items()) && sel > 0 {
			m.queueList.Select(sel - 1)
		}
	}
	m.invalidate(dirtyHeader | dirtyQueue)
	return m, nil
}

// skipToPrevious goes back to the previous track if it's still ready.
func (m Model) skipToPrevious() (Model, tea.Cmd) {
	if m.queue.IsShuffled() {
		if !m.queue.PreviousShuffle() {
			return m, nil
		}
		prev := m.queue.Current()
		if prev == nil || (prev.Path == "" && !downloader.IsLiveURL(prev.URL)) {
			return m, nil
		}
		// Mark old track as done (the one we just left — it's now at shufflePos+1)
		// We already moved back, so the track after us in shuffle order is the old one.
		m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Playing)
		return m.advanceToTrack(m.queue.Current())
	}
	idx := m.queue.CurrentIndex()
	if idx <= 0 {
		return m, nil
	}
	prev := m.queue.Track(idx - 1)
	if prev == nil || (prev.Path == "" && !downloader.IsLiveURL(prev.URL)) {
		return m, nil
	}
	m.queue.SetTrackState(idx, queue.Done)
	m.queue.Previous()
	m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Playing)
	return m.advanceToTrack(m.queue.Current())
}

// handleQueuePlaybackEnd handles playback end when a queue is present.
func (m Model) handleQueuePlaybackEnd() (Model, tea.Cmd) {
	next, nextIdx, found := m.findNextPlayable(m.repeatMode == RepeatAll)
	if found {
		if next.State == queue.Ready || (next.State == queue.Done && (next.Path != "" || downloader.IsLiveURL(next.URL))) {
			return m.advanceAndPlay()
		}
		if next.State == queue.Downloading || next.State == queue.Pending {
			m, cmd := m.enterTransitioning(nextIdx)
			if next.State == queue.Pending {
				return m, m.downloadTrackCmd(nextIdx)
			}
			return m, cmd
		}
	}

	m.elapsed = m.duration
	m.quitting = true
	return m, m.shutdown()
}

// handleTrackDownloaded processes a completed background download.
func (m Model) handleTrackDownloaded(msg trackDownloadedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.queue.SetTrackState(msg.index, queue.Failed)
		if msg.index == m.downloading {
			m.downloading = -1
		}
		m.saveMsg = downloadErrorSummary(msg.err)
		m.saveMsgTime = time.Now()
		m.invalidate(dirtyMid)
		// If we were waiting for this track, try to find another playable track
		if m.transitioning {
			m.transitioning = false
			next, _, found := m.findNextPlayable(m.repeatMode == RepeatAll)
			if found && (next.State == queue.Ready || (next.State == queue.Done && (next.Path != "" || downloader.IsLiveURL(next.URL)))) {
				m.invalidate(dirtyQueue)
				return m.advanceAndPlay()
			}
			// No playable track found — quit
			m.quitting = true
			return m, m.shutdown()
		}
		m.invalidate(dirtyQueue)
		return m, m.startNextDownload()
	}

	m.queue.SetTrackPath(msg.index, msg.path)
	m.queue.SetTrackCleanup(msg.index, msg.cleanup)
	if msg.title != "" {
		m.queue.SetTrackTitle(msg.index, msg.title)
	}
	m.queue.SetTrackState(msg.index, queue.Ready)
	if msg.index == m.downloading {
		m.downloading = -1
	}

	var cmds []tea.Cmd

	if m.transitioning && msg.index == m.transitionTarget {
		m.transitioning = false
		m.cleanupOldTracks()
		m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Done)
		m.queue.SetCurrentIndex(m.transitionTarget)
		m.queue.SetTrackState(m.transitionTarget, queue.Playing)
		m.transitionTarget = -1
		track := m.queue.Current()
		m.metadata = player.Metadata{Title: track.Title}
		m.sourceTitle = track.Title
		m.sourcePath = track.Path

		var err error
		m.player, err = player.New(track.Path)
		if err != nil {
			m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Failed)
			m.invalidate(dirtyQueue)
			return m, func() tea.Msg { return trackFailedMsg{err: err} }
		}
		m.elapsed = 0
		m.duration = m.player.Duration()
		m.volume = m.player.Volume()
		m.paused = false
		if m.speed != player.Speed1x {
			m.player.SetSpeed(m.speed)
		}
		m.invalidate(dirtyHeader)

		cmds = append(cmds, checkDone(m.player), tickCmd(), waitForLiveTitle(m.player), tea.SetWindowTitle(windowTitle(m.metadata.Title, false)))
	}

	// Start downloading next undownloaded track
	cmds = append(cmds, m.startNextDownload())

	m.invalidate(dirtyQueue)

	return m, tea.Batch(cmds...)
}

// handlePlaylistExtracted builds the queue from background extraction results.
func (m Model) handlePlaylistExtracted(msg playlistExtractedMsg) (Model, tea.Cmd) {
	if msg.err != nil || len(msg.entries) <= 1 {
		// Single video or extraction failed — stay in single-track mode.
		return m, nil
	}

	// Build queue tracks from playlist entries.
	tracks := make([]queue.Track, len(msg.entries))
	for i, e := range msg.entries {
		state := queue.Pending
		if downloader.IsLiveURL(e.URL) {
			state = queue.Ready
		}
		tracks[i] = queue.Track{
			ID:    e.ID,
			Title: e.Title,
			URL:   e.URL,
			State: state,
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
	m.updateQueueHeight()
	m.playlistName = playlistLabelFromURL(m.originalURL)
	m.invalidate(dirtyHeader | dirtyQueue)
	m.originalURL = "" // extraction done

	// Start downloading the next track.
	return m, m.startNextDownload()
}

// advanceToTrack switches playback to the given track.
func (m Model) advanceToTrack(track *queue.Track) (Model, tea.Cmd) {
	if m.player != nil {
		m.player.Close()
	}
	isLiveURL := track.URL != "" && downloader.IsLiveURL(track.URL)

	// Local files (no URL) have full metadata on disk; URL downloads only have a title.
	if track.URL == "" && track.Path != "" {
		m.metadata = player.ReadMetadata(track.Path)
	} else {
		m.metadata = player.Metadata{Title: track.Title}
		if m.metadata.Title == "" {
			m.metadata.Title = track.URL
		}
	}
	m.sourceTitle = track.Title
	if track.URL != "" && !isLiveURL {
		m.sourcePath = track.Path
	} else {
		m.sourcePath = ""
	}

	var err error
	if isLiveURL {
		m.player, err = player.NewStream(track.URL)
	} else {
		m.player, err = player.New(track.Path)
	}
	if err != nil {
		// For queue playback, mark the track as failed and try the next one.
		if m.queue != nil {
			m.queue.SetTrackState(m.queue.CurrentIndex(), queue.Failed)
			m.invalidate(dirtyQueue)
			return m, func() tea.Msg { return trackFailedMsg{err: err} }
		}
		m.quitting = true
		return m, m.shutdown()
	}

	m.elapsed = 0
	m.duration = m.player.Duration()
	m.volume = m.player.Volume()
	m.paused = false
	m.transitioning = false
	if m.speed != player.Speed1x {
		m.player.SetSpeed(m.speed)
	}
	m.invalidate(dirtyHeader | dirtyQueue)

	cmds := []tea.Cmd{
		checkDone(m.player),
		tickCmd(),
		waitForLiveTitle(m.player),
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

	trackURL := track.URL
	return func() tea.Msg {
		path, title, cleanup, err := downloader.Download(trackURL, nil)
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

// startNextDownload downloads only the immediate next track in playback order.
func (m Model) startNextDownload() tea.Cmd {
	if m.queue == nil || m.downloading >= 0 {
		return nil
	}
	next := m.queue.NextDownloadIndex()
	if next < 0 || next >= m.queue.Len() {
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
	if m.queue == nil {
		return
	}
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

// fixedLines returns the number of lines used by header, mid section, and help text.
// Top padding (2) + title (1) + artist (1) + gaps (3) + progress (1) + status (1)
// + queue gap (1) + help (~3) = ~13. Long titles may wrap for 1-2 extra lines.
func (m Model) fixedLines() int {
	return 13
}

func downloadErrorSummary(err error) string {
	switch {
	case errors.Is(err, downloader.ErrNoActivityTimeout):
		return "Download timed out (15s no activity)"
	case errors.Is(err, downloader.ErrLiveStreamNotSupported):
		return "Live stream download fallback timed out"
	case errors.Is(err, downloader.ErrUnsupportedScheme):
		return "Unsupported URL scheme (http/https only)"
	default:
		return "Download failed"
	}
}

func (m Model) vizHeight() int {
	avail := m.height - m.fixedLines()
	// When queue is present, give viz at most half the available space
	if m.queue != nil && m.queue.Len() > 1 {
		avail = avail / 2
	}
	if avail < 2 {
		avail = 2
	}
	if avail > maxVizHeight {
		avail = maxVizHeight
	}
	return avail
}

// updateQueueHeight recalculates the queue list height based on current layout.
func (m *Model) updateQueueHeight() {
	if m.queue == nil {
		return
	}
	avail := m.height - m.fixedLines()
	if m.vizEnabled {
		// Subtract the visualizer height + 1 blank line
		avail -= m.vizHeight() + 1
	}
	avail -= 2 // reserve lines for playlist header + blank line
	if avail < 6 {
		avail = 6
	}
	m.queueList.SetHeight(avail)
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	view := m.headerCache + m.midCache + m.vizCache + m.bottomCache
	if m.height <= 0 {
		return view
	}
	lines := lipgloss.Height(view)
	if lines >= m.height {
		return view
	}
	return view + strings.Repeat("\n", m.height-lines)
}

func windowTitle(title string, paused bool) string {
	if paused {
		return "⏸ " + title + " — climp"
	}
	return "▶ " + title + " — climp"
}

func normalizePlaylistLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "Playlist"
	}
	var b strings.Builder
	b.Grow(len(label))
	for _, r := range label {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	clean := strings.TrimSpace(b.String())
	if clean == "" {
		return "Playlist"
	}
	return clean
}

func playlistLabelFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "Playlist"
	}
	host := normalizePlaylistLabel(u.Hostname())
	if host == "Playlist" {
		return "Playlist"
	}
	return "Playlist (" + host + ")"
}

func truncateLabel(label string, maxRunes int) string {
	if maxRunes < 1 {
		return ""
	}
	r := []rune(label)
	if len(r) <= maxRunes {
		return label
	}
	if maxRunes <= 3 {
		return string(r[:maxRunes])
	}
	return string(r[:maxRunes-3]) + "..."
}

func spaces(n int) string {
	if n < 0 {
		n = 0
	}
	return strings.Repeat(" ", n)
}
