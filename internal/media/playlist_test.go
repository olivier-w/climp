package media

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseLocalPlaylistM3U(t *testing.T) {
	dir := t.TempDir()
	playlist := filepath.Join(dir, "list.m3u")
	content := "\uFEFF#EXTM3U\n\nsong1.mp3\n#comment\n\"https://example.com/stream\"\nsub/song2.wav\n"
	if err := os.WriteFile(playlist, []byte(content), 0o644); err != nil {
		t.Fatalf("write playlist: %v", err)
	}

	got, err := ParseLocalPlaylist(playlist)
	if err != nil {
		t.Fatalf("ParseLocalPlaylist() error = %v", err)
	}

	want := []PlaylistEntry{
		{Path: filepath.Join(dir, "song1.mp3")},
		{URL: "https://example.com/stream", Title: "https://example.com/stream"},
		{Path: filepath.Join(dir, "sub", "song2.wav")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseLocalPlaylist() = %#v, want %#v", got, want)
	}
}

func TestParseLocalPlaylistPLS(t *testing.T) {
	dir := t.TempDir()
	playlist := filepath.Join(dir, "list.pls")
	content := "[playlist]\n file1 = one.flac \nTitle1=One\nLength1=120\nFile2=https://example.com/live\nFileX=bad.mp3\nFile3=\n"
	if err := os.WriteFile(playlist, []byte(content), 0o644); err != nil {
		t.Fatalf("write playlist: %v", err)
	}

	got, err := ParseLocalPlaylist(playlist)
	if err != nil {
		t.Fatalf("ParseLocalPlaylist() error = %v", err)
	}

	want := []PlaylistEntry{
		{Path: filepath.Join(dir, "one.flac")},
		{URL: "https://example.com/live", Title: "https://example.com/live"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseLocalPlaylist() = %#v, want %#v", got, want)
	}
}

func TestFilterPlayablePlaylistEntries(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "ok.mp3")
	if err := os.WriteFile(valid, []byte("x"), 0o644); err != nil {
		t.Fatalf("write valid file: %v", err)
	}
	validM4A := filepath.Join(dir, "chapter.m4a")
	if err := os.WriteFile(validM4A, []byte("x"), 0o644); err != nil {
		t.Fatalf("write valid m4a file: %v", err)
	}
	validM4B := filepath.Join(dir, "book.m4b")
	if err := os.WriteFile(validM4B, []byte("x"), 0o644); err != nil {
		t.Fatalf("write valid m4b file: %v", err)
	}
	unsupported := filepath.Join(dir, "nope.txt")
	if err := os.WriteFile(unsupported, []byte("x"), 0o644); err != nil {
		t.Fatalf("write unsupported file: %v", err)
	}
	subdir := filepath.Join(dir, "folder")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}

	input := []PlaylistEntry{
		{Path: valid},
		{Path: validM4A},
		{Path: validM4B},
		{Path: filepath.Join(dir, "missing.mp3")},
		{Path: unsupported},
		{Path: subdir},
		{URL: "https://example.com/track.mp3"},
	}

	got, skipped := FilterPlayablePlaylistEntries(input)
	want := []PlaylistEntry{
		{Path: valid, Title: "ok"},
		{Path: validM4A, Title: "chapter"},
		{Path: validM4B, Title: "book"},
		{URL: "https://example.com/track.mp3", Title: "https://example.com/track.mp3"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FilterPlayablePlaylistEntries() = %#v, want %#v", got, want)
	}
	if skipped != 3 {
		t.Fatalf("FilterPlayablePlaylistEntries() skipped=%d, want %d", skipped, 3)
	}
}
