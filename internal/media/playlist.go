package media

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// PlaylistEntry represents one playable candidate from a local playlist file.
// Exactly one of URL or Path is expected to be set.
type PlaylistEntry struct {
	Title string
	URL   string
	Path  string
}

// ParseLocalPlaylist parses a local .m3u/.m3u8/.pls file into playlist entries.
// Relative path entries are resolved against the playlist file directory.
func ParseLocalPlaylist(path string) ([]PlaylistEntry, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if !IsPlaylistExt(ext) {
		return nil, fmt.Errorf("unsupported playlist format %s", ext)
	}

	absPlaylistPath, err := filepath.Abs(path)
	if err != nil {
		absPlaylistPath = path
	}

	data, err := os.ReadFile(absPlaylistPath)
	if err != nil {
		return nil, fmt.Errorf("reading playlist: %w", err)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("playlist is not valid UTF-8")
	}

	baseDir := filepath.Dir(absPlaylistPath)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))

	switch ext {
	case ".pls":
		return parsePLS(scanner, baseDir), nil
	default:
		return parseM3U(scanner, baseDir), nil
	}
}

// FilterPlayablePlaylistEntries keeps only entries that can be attempted:
// http(s) URLs and existing supported local media files.
// Returns filtered entries and the number of skipped entries.
func FilterPlayablePlaylistEntries(entries []PlaylistEntry) ([]PlaylistEntry, int) {
	out := make([]PlaylistEntry, 0, len(entries))
	skipped := 0
	for _, e := range entries {
		if e.URL != "" {
			if e.Title == "" {
				e.Title = e.URL
			}
			out = append(out, e)
			continue
		}
		if e.Path == "" {
			skipped++
			continue
		}

		info, err := os.Stat(e.Path)
		if err != nil || info.IsDir() {
			skipped++
			continue
		}
		if !IsSupportedExt(filepath.Ext(e.Path)) {
			skipped++
			continue
		}
		abs, err := filepath.Abs(e.Path)
		if err == nil {
			e.Path = abs
		}
		if e.Title == "" {
			e.Title = strings.TrimSuffix(filepath.Base(e.Path), filepath.Ext(e.Path))
		}
		out = append(out, e)
	}
	return out, skipped
}

func parseM3U(scanner *bufio.Scanner, baseDir string) []PlaylistEntry {
	entries := make([]PlaylistEntry, 0)
	firstLine := true
	for scanner.Scan() {
		line := normalizeEntryText(scanner.Text(), firstLine)
		firstLine = false
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if entry, ok := parseEntry(line, baseDir); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

func parsePLS(scanner *bufio.Scanner, baseDir string) []PlaylistEntry {
	entries := make([]PlaylistEntry, 0)
	firstLine := true
	for scanner.Scan() {
		line := normalizeEntryText(scanner.Text(), firstLine)
		firstLine = false
		if line == "" {
			continue
		}

		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := normalizeEntryText(line[eq+1:], false)
		if val == "" || !isPLSFileKey(key) {
			continue
		}

		if entry, ok := parseEntry(val, baseDir); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

func isPLSFileKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if !strings.HasPrefix(lower, "file") {
		return false
	}
	rest := lower[len("file"):]
	if rest == "" {
		return false
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return false
		}
	}
	return true
}

func parseEntry(raw, baseDir string) (PlaylistEntry, bool) {
	raw = normalizeEntryText(raw, false)
	if raw == "" {
		return PlaylistEntry{}, false
	}
	if isHTTPURL(raw) {
		return PlaylistEntry{URL: raw, Title: raw}, true
	}
	return PlaylistEntry{Path: resolvePlaylistEntryPath(raw, baseDir)}, true
}

func normalizeEntryText(s string, stripBOM bool) string {
	if stripBOM {
		s = strings.TrimPrefix(s, "\uFEFF")
	}
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		first := s[0]
		last := s[len(s)-1]
		if (first == '"' || first == '\'') && last == first {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}

func isHTTPURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func resolvePlaylistEntryPath(raw, baseDir string) string {
	p := filepath.Clean(strings.TrimSpace(raw))
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Clean(filepath.Join(baseDir, p))
}
