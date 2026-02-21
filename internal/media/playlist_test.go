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
	content := "#EXTM3U\n\nsong1.mp3\n#comment\nsub/song2.wav\n"
	if err := os.WriteFile(playlist, []byte(content), 0o644); err != nil {
		t.Fatalf("write playlist: %v", err)
	}

	got, err := ParseLocalPlaylist(playlist)
	if err != nil {
		t.Fatalf("ParseLocalPlaylist() error = %v", err)
	}

	want := []string{
		filepath.Join(dir, "song1.mp3"),
		filepath.Join(dir, "sub", "song2.wav"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseLocalPlaylist() = %#v, want %#v", got, want)
	}
}

func TestParseLocalPlaylistPLS(t *testing.T) {
	dir := t.TempDir()
	playlist := filepath.Join(dir, "list.pls")
	content := "[playlist]\nFile1=one.flac\nTitle1=One\nLength1=120\nFile2=two.ogg\nFileX=bad.mp3\nFile3=\n"
	if err := os.WriteFile(playlist, []byte(content), 0o644); err != nil {
		t.Fatalf("write playlist: %v", err)
	}

	got, err := ParseLocalPlaylist(playlist)
	if err != nil {
		t.Fatalf("ParseLocalPlaylist() error = %v", err)
	}

	want := []string{
		filepath.Join(dir, "one.flac"),
		filepath.Join(dir, "two.ogg"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseLocalPlaylist() = %#v, want %#v", got, want)
	}
}

func TestFilterPlayableLocalPaths(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "ok.mp3")
	if err := os.WriteFile(valid, []byte("x"), 0o644); err != nil {
		t.Fatalf("write valid file: %v", err)
	}
	unsupported := filepath.Join(dir, "nope.txt")
	if err := os.WriteFile(unsupported, []byte("x"), 0o644); err != nil {
		t.Fatalf("write unsupported file: %v", err)
	}
	subdir := filepath.Join(dir, "folder")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}

	input := []string{
		valid,
		filepath.Join(dir, "missing.mp3"),
		unsupported,
		subdir,
		"https://example.com/track.mp3",
	}

	got := FilterPlayableLocalPaths(input)
	want := []string{valid}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FilterPlayableLocalPaths() = %#v, want %#v", got, want)
	}
}
