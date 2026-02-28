package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEmbeddedBrowserFileSelectionReturnsMessage(t *testing.T) {
	restore := chdirTemp(t, map[string]string{
		"song.mp3": "data",
	})
	defer restore()

	m := NewEmbeddedBrowser()

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = model.(BrowserModel)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected selection command")
	}

	msg := cmd()
	selected, ok := msg.(BrowserSelectedMsg)
	if !ok {
		t.Fatalf("expected BrowserSelectedMsg, got %T", msg)
	}
	if selected.Path != "song.mp3" {
		t.Fatalf("expected song.mp3, got %q", selected.Path)
	}
}

func TestEmbeddedBrowserURLSelectionReturnsMessage(t *testing.T) {
	restore := chdirTemp(t, map[string]string{})
	defer restore()

	m := NewEmbeddedBrowser()
	m.urlMode = true
	m.input.SetValue("https://example.com")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected URL selection command")
	}

	msg := cmd()
	selected, ok := msg.(BrowserSelectedMsg)
	if !ok {
		t.Fatalf("expected BrowserSelectedMsg, got %T", msg)
	}
	if selected.Path != "https://example.com" {
		t.Fatalf("expected URL path, got %q", selected.Path)
	}
}

func TestEmbeddedBrowserCancelReturnsMessage(t *testing.T) {
	restore := chdirTemp(t, map[string]string{})
	defer restore()

	m := NewEmbeddedBrowser()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected cancel command")
	}

	if _, ok := cmd().(BrowserCancelledMsg); !ok {
		t.Fatalf("expected BrowserCancelledMsg, got %T", cmd())
	}
}

func TestStandaloneBrowserSelectionStoresResult(t *testing.T) {
	restore := chdirTemp(t, map[string]string{
		"song.mp3": "data",
	})
	defer restore()

	m := NewBrowser()

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = model.(BrowserModel)

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(BrowserModel)

	result := m.Result()
	if result.Path != "song.mp3" || result.Cancelled {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestBrowserShowsAACFamilyFiles(t *testing.T) {
	restore := chdirTemp(t, map[string]string{
		"track.m4a": "data",
		"book.m4b":  "data",
		"clip.aac":  "data",
	})
	defer restore()

	m := NewEmbeddedBrowser()

	for _, name := range []string{"book.m4b", "clip.aac", "track.m4a"} {
		found := false
		for _, item := range m.list.Items() {
			file, ok := item.(fileItem)
			if ok && file.name+file.ext == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected browser to include %s", name)
		}
	}
}

func chdirTemp(t *testing.T, files map[string]string) func() {
	t.Helper()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	dir := t.TempDir()
	for name, contents := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}

	return func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}
}
