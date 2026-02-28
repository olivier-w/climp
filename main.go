package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/olivier-w/climp/internal/downloader"
	"github.com/olivier-w/climp/internal/media"
	"github.com/olivier-w/climp/internal/player"
	"github.com/olivier-w/climp/internal/ui"
)

const maxRemotePlaylistDepth = 2

var version = "dev"

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "-h", "--help":
			printHelp()
			return
		case "-v", "--version":
			printVersion()
			return
		}
	}

	if len(os.Args) < 2 {
		program := tea.NewProgram(newStartupModel(), tea.WithAltScreen(), tea.WithMouseCellMotion())
		if _, err := program.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	model, err := buildPlaybackModel(os.Args[1], downloadURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// scanAudioFiles returns all supported audio files in the same directory as path,
// sorted alphabetically (case-insensitive). Returns nil if fewer than 2 files found.
func scanAudioFiles(path string) []string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	dir := filepath.Dir(absPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if media.IsSupportedExt(ext) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}

	if len(files) < 2 {
		return nil
	}

	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(filepath.Base(files[i])) < strings.ToLower(filepath.Base(files[j]))
	})

	return files
}

func playlistNameFromFile(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "Playlist"
	}
	return name
}

func playlistNameFromDirectoryOfFile(path string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	dir := filepath.Dir(absPath)
	name := strings.TrimSpace(filepath.Base(dir))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "Playlist"
	}
	return name
}

func playlistNameFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "Playlist"
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return "Playlist"
	}
	return host
}

type playlistStart struct {
	player     *player.Player
	path       string
	sourcePath string
	meta       player.Metadata
	metaSet    bool
	startIdx   int
	cleanup    func()
}

type urlDownloadFunc func(string) (ui.DownloadResult, error)

func openFirstPlayablePlaylistEntry(entries []media.PlaylistEntry, downloadURL urlDownloadFunc) ([]media.PlaylistEntry, playlistStart, error) {
	start := playlistStart{startIdx: -1}
	for i := range entries {
		e := &entries[i]
		if e.Path != "" && e.URL == "" {
			start.path = e.Path
			start.startIdx = i
			return entries, start, nil
		}
		if e.URL == "" {
			continue
		}

		if downloader.IsLiveURL(e.URL) {
			sp, err := player.NewStream(e.URL)
			if err == nil {
				start.player = sp
				start.meta = player.Metadata{Title: e.Title}
				if start.meta.Title == "" {
					start.meta.Title = e.URL
				}
				start.metaSet = true
				start.startIdx = i
				return entries, start, nil
			}
		}

		result, err := downloadURL(e.URL)
		if err != nil {
			continue
		}
		if result.Err != nil {
			if result.Cleanup != nil {
				result.Cleanup()
			}
			continue
		}
		e.Path = result.Path
		if result.Title != "" {
			e.Title = result.Title
		}
		start.path = e.Path
		start.sourcePath = e.Path
		start.cleanup = result.Cleanup
		start.meta = player.Metadata{Title: e.Title}
		if start.meta.Title == "" {
			start.meta = player.ReadMetadata(start.path)
		}
		start.metaSet = true
		start.startIdx = i
		return entries, start, nil
	}

	return entries, start, fmt.Errorf("playlist contains no playable entries")
}

func expandRemotePlaylistEntries(entries []media.PlaylistEntry, depth int) []media.PlaylistEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]media.PlaylistEntry, 0, len(entries))
	for _, e := range entries {
		if e.URL == "" {
			out = append(out, e)
			continue
		}

		route, err := downloader.ResolveURLRoute(e.URL)
		if err != nil {
			out = append(out, e)
			continue
		}
		if route.FinalURL != "" {
			e.URL = route.FinalURL
			if e.Title == "" {
				e.Title = e.URL
			}
		}

		if route.Kind != downloader.RouteRemotePlaylist {
			out = append(out, e)
			continue
		}
		if len(route.Playlist) == 0 {
			continue
		}
		if depth <= 0 {
			out = append(out, route.Playlist...)
			continue
		}
		out = append(out, expandRemotePlaylistEntries(route.Playlist, depth-1)...)
	}
	return out
}

func downloadURL(url string) (ui.DownloadResult, error) {
	dlModel := ui.NewDownload(url)
	dlProgram := tea.NewProgram(dlModel, tea.WithAltScreen())
	finalModel, err := dlProgram.Run()
	if err != nil {
		return ui.DownloadResult{}, err
	}

	dm, ok := finalModel.(ui.DownloadModel)
	if !ok {
		return ui.DownloadResult{}, fmt.Errorf("unexpected model type from downloader")
	}
	return dm.Result(), nil
}

func printHelp() {
	fmt.Println("climp - Minimal CLI media player for local files, URLs, and playlists.")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  climp")
	fmt.Println("  climp <file|playlist|url>")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  -h, --help")
	fmt.Println("  -v, --version")
}

func printVersion() {
	fmt.Printf("climp %s\n", displayVersion())
}

func displayVersion() string {
	if version != "dev" {
		return version
	}
	if gitVersion, ok := gitTaggedDevVersion(); ok {
		return gitVersion
	}
	return version
}

func gitTaggedDevVersion() (string, bool) {
	exePath, err := os.Executable()
	if err != nil {
		return "", false
	}

	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
	cmd.Dir = filepath.Dir(exePath)
	output, err := cmd.Output()
	if err != nil {
		return "", false
	}

	tag := strings.TrimSpace(string(output))
	if tag == "" {
		return "", false
	}
	return tag + "-dev", true
}
