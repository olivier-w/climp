package ui

import (
	"testing"

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
