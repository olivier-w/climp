package media

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ParseLocalPlaylist parses a local .m3u/.m3u8/.pls file into local path entries.
// Relative entries are resolved against the playlist file directory.
func ParseLocalPlaylist(path string) ([]string, error) {
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

// FilterPlayableLocalPaths keeps only existing, non-directory, supported media files.
func FilterPlayableLocalPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		if !IsSupportedExt(filepath.Ext(p)) {
			continue
		}
		abs, err := filepath.Abs(p)
		if err == nil {
			p = abs
		}
		out = append(out, p)
	}
	return out
}

func parseM3U(scanner *bufio.Scanner, baseDir string) []string {
	entries := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, resolvePlaylistEntryPath(line, baseDir))
	}
	return entries
}

func parsePLS(scanner *bufio.Scanner, baseDir string) []string {
	entries := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if val == "" || !isPLSFileKey(key) {
			continue
		}

		entries = append(entries, resolvePlaylistEntryPath(val, baseDir))
	}
	return entries
}

func isPLSFileKey(key string) bool {
	if !strings.HasPrefix(key, "File") {
		return false
	}
	rest := key[len("File"):]
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

func resolvePlaylistEntryPath(raw, baseDir string) string {
	p := filepath.Clean(raw)
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Clean(filepath.Join(baseDir, p))
}
