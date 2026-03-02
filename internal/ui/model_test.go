package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/olivier-w/climp/internal/player"
	"github.com/olivier-w/climp/internal/queue"
)

func TestHandleLiveTitleUpdatedMsgUpdatesCurrentMetadata(t *testing.T) {
	p := new(player.Player)
	m := Model{
		player:   p,
		metadata: player.Metadata{Title: "Original"},
	}

	next, cmd := m.handleMsg(liveTitleUpdatedMsg{player: p, title: "Updated"})
	if next.metadata.Title != "Updated" {
		t.Fatalf("expected updated title, got %q", next.metadata.Title)
	}
	if next.dirty&dirtyHeader == 0 {
		t.Fatal("expected header cache to be invalidated")
	}
	if cmd == nil {
		t.Fatal("expected command for live title update")
	}
}

func TestHandleLiveTitleUpdatedMsgIgnoresStalePlayer(t *testing.T) {
	current := new(player.Player)
	stale := new(player.Player)
	m := Model{
		player:   current,
		metadata: player.Metadata{Title: "Original"},
	}

	next, cmd := m.handleMsg(liveTitleUpdatedMsg{player: stale, title: "Updated"})
	if next.metadata.Title != "Original" {
		t.Fatalf("expected original title, got %q", next.metadata.Title)
	}
	if cmd != nil {
		t.Fatal("expected no command for stale player update")
	}
}

func TestHandleLiveTitleUpdatedMsgLeavesQueueUntouched(t *testing.T) {
	p := new(player.Player)
	q := queue.New([]queue.Track{
		{Title: "Live Station", State: queue.Playing},
		{Title: "Next Track", State: queue.Ready},
	})
	q.SetCurrentIndex(0)

	m := Model{
		player:   p,
		metadata: player.Metadata{Title: "Live Station"},
		queue:    q,
	}

	next, _ := m.handleMsg(liveTitleUpdatedMsg{player: p, title: "Artist - Song"})
	if next.metadata.Title != "Artist - Song" {
		t.Fatalf("expected metadata title update, got %q", next.metadata.Title)
	}
	if got := next.queue.Track(0).Title; got != "Live Station" {
		t.Fatalf("expected queue title unchanged, got %q", got)
	}
}

func TestViewPadsToWindowHeight(t *testing.T) {
	m := Model{
		height:      8,
		headerCache: "\n  title\n\n",
		midCache:    "  status\n\n",
		bottomCache: "\n  help\n",
	}

	view := m.View()
	if lipgloss.Height(view) < 8 {
		t.Fatalf("expected padded view height >= 8, got %d", lipgloss.Height(view))
	}
	if !strings.Contains(view, "  help\n") {
		t.Fatalf("expected help content in padded view, got %q", view)
	}
}

func TestBeginSeekPreviewUpdatesElapsedImmediately(t *testing.T) {
	p := new(player.Player)
	m := Model{
		player:   p,
		duration: 30 * time.Second,
	}

	cmd := m.beginSeekPreview(10*time.Second, 5*time.Second, true)
	if cmd == nil {
		t.Fatal("expected debounce command")
	}
	if !m.seekPending {
		t.Fatal("expected pending seek state")
	}
	if got := m.seekTarget; got != 15*time.Second {
		t.Fatalf("expected seek target 15s, got %v", got)
	}
	if got := m.elapsed; got != 15*time.Second {
		t.Fatalf("expected elapsed preview 15s, got %v", got)
	}
	if !m.paused {
		t.Fatal("expected preview to force paused state")
	}
	if !m.seekResume {
		t.Fatal("expected resume intent to be preserved")
	}
	if m.seekSeq != 1 {
		t.Fatalf("expected seek seq 1, got %d", m.seekSeq)
	}
}

func TestSeekDebounceIgnoresStaleSeq(t *testing.T) {
	p := new(player.Player)
	m := Model{
		player:      p,
		seekPending: true,
		seekTarget:  12 * time.Second,
		seekSeq:     2,
	}

	next, cmd := m.handleMsg(seekDebounceMsg{player: p, seq: 1})
	if next.seekApplying {
		t.Fatal("expected stale debounce to leave seekApplying false")
	}
	if cmd != nil {
		t.Fatal("expected no command for stale debounce")
	}
}

func TestSeekAppliedMsgRequeuesNewestTarget(t *testing.T) {
	p := new(player.Player)
	m := Model{
		player:       p,
		seekPending:  true,
		seekApplying: true,
		seekTarget:   12 * time.Second,
		seekResume:   true,
		seekSeq:      3,
	}

	next, cmd := m.handleMsg(seekAppliedMsg{
		player: p,
		seq:    2,
		target: 10 * time.Second,
	})
	if !next.seekPending || !next.seekApplying {
		t.Fatal("expected seek session to stay active for newer target")
	}
	if cmd == nil {
		t.Fatal("expected requeued apply command")
	}

	msg, ok := cmd().(seekAppliedMsg)
	if !ok {
		t.Fatal("expected seekAppliedMsg from requeued command")
	}
	if msg.seq != 3 {
		t.Fatalf("expected requeued seq 3, got %d", msg.seq)
	}
	if msg.target != 12*time.Second {
		t.Fatalf("expected requeued target 12s, got %v", msg.target)
	}
}

func TestTickMsgDoesNotOverwriteElapsedDuringSeekPreview(t *testing.T) {
	p := new(player.Player)
	m := Model{
		player:      p,
		elapsed:     18 * time.Second,
		seekPending: true,
	}

	next, _ := m.handleMsg(tickMsg(time.Now()))
	if got := next.elapsed; got != 18*time.Second {
		t.Fatalf("expected elapsed to stay at preview target, got %v", got)
	}
	if !next.paused {
		t.Fatal("expected seek preview tick to remain paused")
	}
}

func TestSeekAppliedMsgClearsStateOnLatestSuccess(t *testing.T) {
	p := new(player.Player)
	m := Model{
		player:       p,
		seekPending:  true,
		seekApplying: true,
		seekTarget:   22 * time.Second,
		seekResume:   true,
		seekSeq:      4,
	}

	next, cmd := m.handleMsg(seekAppliedMsg{
		player: p,
		seq:    4,
		target: 22 * time.Second,
	})
	if cmd != nil {
		t.Fatal("expected no follow-up command on latest seek success")
	}
	if next.seekPending || next.seekApplying {
		t.Fatal("expected seek state to clear")
	}
	if got := next.elapsed; got != 22*time.Second {
		t.Fatalf("expected elapsed 22s, got %v", got)
	}
	if next.paused {
		t.Fatal("expected resumed state after successful seek")
	}
}
