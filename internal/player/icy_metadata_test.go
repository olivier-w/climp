package player

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseICYMetaInt(t *testing.T) {
	got, err := parseICYMetaInt(" 16000 ")
	if err != nil {
		t.Fatalf("parseICYMetaInt returned error: %v", err)
	}
	if got != 16000 {
		t.Fatalf("expected 16000, got %d", got)
	}
}

func TestParseICYMetaIntRejectsMissingValue(t *testing.T) {
	if _, err := parseICYMetaInt(""); err == nil {
		t.Fatal("expected error for missing icy-metaint")
	}
}

func TestExtractICYStreamTitle(t *testing.T) {
	block := []byte("StreamTitle='Artist - Song';StreamUrl='';")
	got := extractICYStreamTitle(block)
	if got != "Artist - Song" {
		t.Fatalf("expected title, got %q", got)
	}
}

func TestExtractICYStreamTitleTrimsPadding(t *testing.T) {
	block := append([]byte("StreamTitle='Artist - Song';"), 0, 0, 0)
	got := extractICYStreamTitle(block)
	if got != "Artist - Song" {
		t.Fatalf("expected trimmed title, got %q", got)
	}
}

func TestExtractICYStreamTitleIgnoresMissingValue(t *testing.T) {
	block := []byte("StreamUrl='https://example.com';")
	if got := extractICYStreamTitle(block); got != "" {
		t.Fatalf("expected empty title, got %q", got)
	}
}

func TestExtractICYStreamTitleIgnoresEmptyValue(t *testing.T) {
	block := []byte("StreamTitle='';")
	if got := extractICYStreamTitle(block); got != "" {
		t.Fatalf("expected empty title, got %q", got)
	}
}

func TestICYTitleWatcherEmitsChangedTitles(t *testing.T) {
	const metaInt = 4

	titleBlocks := []string{
		padICYMetadata("StreamTitle='First Title';"),
		padICYMetadata("StreamTitle='First Title';"),
		padICYMetadata("StreamTitle='Second Title';"),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Icy-MetaData"); got != "1" {
			t.Fatalf("expected Icy-MetaData header, got %q", got)
		}
		w.Header().Set("icy-metaint", fmt.Sprintf("%d", metaInt))
		flusher, _ := w.(http.Flusher)
		for _, block := range titleBlocks {
			if _, err := w.Write([]byte("abcd")); err != nil {
				return
			}
			if _, err := w.Write([]byte{byte(len(block) / 16)}); err != nil {
				return
			}
			if _, err := w.Write([]byte(block)); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	watcher, err := newICYTitleWatcher(srv.URL)
	if err != nil {
		t.Fatalf("newICYTitleWatcher returned error: %v", err)
	}
	defer watcher.Close()

	first := waitForICYTitle(t, watcher.Updates())
	if first != "First Title" {
		t.Fatalf("expected first title, got %q", first)
	}

	second := waitForICYTitle(t, watcher.Updates())
	if second != "Second Title" {
		t.Fatalf("expected second title, got %q", second)
	}
}

func TestICYTitleWatcherCloseClosesUpdates(t *testing.T) {
	const metaInt = 4

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("icy-metaint", fmt.Sprintf("%d", metaInt))
		flusher, _ := w.(http.Flusher)
		if _, err := w.Write([]byte("abcd")); err != nil {
			return
		}
		if _, err := w.Write([]byte{0}); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	watcher, err := newICYTitleWatcher(srv.URL)
	if err != nil {
		t.Fatalf("newICYTitleWatcher returned error: %v", err)
	}

	if err := watcher.Close(); err != nil {
		t.Fatalf("watcher.Close returned error: %v", err)
	}

	select {
	case _, ok := <-watcher.Updates():
		if ok {
			t.Fatal("expected updates channel to be closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for updates channel close")
	}
}

func waitForICYTitle(t *testing.T, updates <-chan string) string {
	t.Helper()
	select {
	case title, ok := <-updates:
		if !ok {
			t.Fatal("updates channel closed unexpectedly")
		}
		return title
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for title update")
		return ""
	}
}

func padICYMetadata(value string) string {
	if rem := len(value) % 16; rem != 0 {
		value += strings.Repeat("\x00", 16-rem)
	}
	return value
}
