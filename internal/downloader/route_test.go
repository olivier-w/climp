package downloader

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestResolveURLRouteRemotePLS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/listen.pls" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "audio/x-scpls")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("[playlist]\nNumberOfEntries=1\nFile1=http://" + r.Host + "/stream;\nTitle1=Demo\nLength1=-1\nVersion=2\n"))
	}))
	defer srv.Close()

	got, err := ResolveURLRoute(srv.URL + "/listen.pls")
	if err != nil {
		t.Fatalf("ResolveURLRoute() error = %v", err)
	}
	if got.Kind != RouteRemotePlaylist {
		t.Fatalf("ResolveURLRoute() kind = %v, want %v", got.Kind, RouteRemotePlaylist)
	}
	if len(got.Playlist) != 1 {
		t.Fatalf("ResolveURLRoute() playlist len = %d, want 1", len(got.Playlist))
	}
	wantURL := srv.URL + "/stream"
	if got.Playlist[0].URL != wantURL {
		t.Fatalf("ResolveURLRoute() playlist URL = %q, want %q", got.Playlist[0].URL, wantURL)
	}
}

func TestResolveURLRouteRemoteM3UWithRelativeEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/radio/listen.m3u" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "audio/x-mpegurl")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:-1,Station\nstream\n"))
	}))
	defer srv.Close()

	got, err := ResolveURLRoute(srv.URL + "/radio/listen.m3u")
	if err != nil {
		t.Fatalf("ResolveURLRoute() error = %v", err)
	}
	if got.Kind != RouteRemotePlaylist {
		t.Fatalf("ResolveURLRoute() kind = %v, want %v", got.Kind, RouteRemotePlaylist)
	}
	if len(got.Playlist) != 1 {
		t.Fatalf("ResolveURLRoute() playlist len = %d, want 1", len(got.Playlist))
	}
	wantURL := srv.URL + "/radio/stream"
	if got.Playlist[0].URL != wantURL {
		t.Fatalf("ResolveURLRoute() playlist URL = %q, want %q", got.Playlist[0].URL, wantURL)
	}
}

func TestResolveURLRouteLiveICYNoExtension(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stream" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Icy-Name", "Demo")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake-audio-bytes"))
	}))
	defer srv.Close()

	url := srv.URL + "/stream"
	got, err := ResolveURLRoute(url)
	if err != nil {
		t.Fatalf("ResolveURLRoute() error = %v", err)
	}
	if got.Kind != RouteLiveStream {
		t.Fatalf("ResolveURLRoute() kind = %v, want %v", got.Kind, RouteLiveStream)
	}
	if !IsLiveURL(url) {
		t.Fatalf("IsLiveURL(%q) = false, want true", url)
	}
}

func TestResolveURLRouteLiveMP3AndOGG(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/radio.mp3":
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Header().Set("Icy-Name", "MP3 Station")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("mp3-stream"))
		case "/radio.ogg":
			w.Header().Set("Content-Type", "application/ogg")
			w.Header().Set("Icy-Name", "OGG Station")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ogg-stream"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	for _, path := range []string{"/radio.mp3", "/radio.ogg"} {
		url := srv.URL + path
		got, err := ResolveURLRoute(url)
		if err != nil {
			t.Fatalf("ResolveURLRoute(%q) error = %v", url, err)
		}
		if got.Kind != RouteLiveStream {
			t.Fatalf("ResolveURLRoute(%q) kind = %v, want %v", url, got.Kind, RouteLiveStream)
		}
	}
}

func TestResolveURLRouteFiniteAudioFile(t *testing.T) {
	data := []byte("1234567890")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file.mp3" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	url := srv.URL + "/file.mp3"
	got, err := ResolveURLRoute(url)
	if err != nil {
		t.Fatalf("ResolveURLRoute() error = %v", err)
	}
	if got.Kind != RouteFiniteDownload {
		t.Fatalf("ResolveURLRoute() kind = %v, want %v", got.Kind, RouteFiniteDownload)
	}
	if IsLiveURL(url) {
		t.Fatalf("IsLiveURL(%q) = true, want false", url)
	}
}

func TestResolveURLRouteHLSBodyIsLive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/live.m3u8" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\nsegment1.ts\n"))
	}))
	defer srv.Close()

	got, err := ResolveURLRoute(srv.URL + "/live.m3u8")
	if err != nil {
		t.Fatalf("ResolveURLRoute() error = %v", err)
	}
	if got.Kind != RouteLiveStream {
		t.Fatalf("ResolveURLRoute() kind = %v, want %v", got.Kind, RouteLiveStream)
	}
}
