package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/olivier-w/climp/internal/downloader"
	"github.com/olivier-w/climp/internal/media"
	"github.com/olivier-w/climp/internal/player"
	"github.com/olivier-w/climp/internal/queue"
	"github.com/olivier-w/climp/internal/ui"
)

const maxRemotePlaylistDepth = 2

func main() {
	var arg string
	var playlistEntries []media.PlaylistEntry
	playlistStartIdx := -1
	var playlistStartCleanup func()
	var playlistSourcePath string
	playlistName := ""
	metaSet := false

	if len(os.Args) < 2 {
		browser := ui.NewBrowser()
		p := tea.NewProgram(browser, tea.WithAltScreen())
		finalModel, err := p.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		bm, ok := finalModel.(ui.BrowserModel)
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: unexpected model type from browser\n")
			os.Exit(1)
		}
		result := bm.Result()
		if result.Cancelled {
			os.Exit(0)
		}
		arg = result.Path
	} else {
		arg = os.Args[1]
	}

	var path string
	var sourcePath string
	var originalURL string
	var meta player.Metadata
	var p *player.Player
	if downloader.IsURL(arg) {
		route, err := downloader.ResolveURLRoute(arg)
		if err != nil {
			route = downloader.URLRouteResult{
				Kind:     downloader.RouteFiniteDownload,
				FinalURL: arg,
			}
		}
		if route.FinalURL == "" {
			route.FinalURL = arg
		}
		if route.Kind == downloader.RouteRemotePlaylist {
			playlistName = playlistNameFromURL(arg)
			playlistEntries = expandRemotePlaylistEntries(route.Playlist, maxRemotePlaylistDepth)
			if len(playlistEntries) == 0 {
				fmt.Fprintf(os.Stderr, "Error: playlist contains no playable entries\n")
				os.Exit(1)
			}
		} else {
			openedLive := false
			if route.Kind == downloader.RouteLiveStream {
				p, err = player.NewStream(route.FinalURL)
				if err == nil {
					openedLive = true
					meta = player.Metadata{Title: route.FinalURL}
					metaSet = true
				}
			}
			if !openedLive {
				result, err := downloadURL(route.FinalURL)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				if result.Err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", result.Err)
					os.Exit(1)
				}
				if result.Cleanup != nil {
					defer result.Cleanup()
				}
				path = result.Path
				sourcePath = result.Path
				originalURL = arg

				if result.Title != "" {
					meta = player.Metadata{Title: result.Title}
				} else {
					meta = player.ReadMetadata(path)
				}
				metaSet = true
			}
		}
	} else {
		path = arg

		// Check file exists
		info, err := os.Stat(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if info.IsDir() {
			fmt.Fprintf(os.Stderr, "Error: %s is a directory\n", path)
			os.Exit(1)
		}

		// Check extension
		ext := strings.ToLower(filepath.Ext(path))
		if media.IsPlaylistExt(ext) {
			playlistName = playlistNameFromFile(path)
			entries, err := media.ParseLocalPlaylist(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			playlistEntries, _ = media.FilterPlayablePlaylistEntries(entries)
			playlistEntries = expandRemotePlaylistEntries(playlistEntries, maxRemotePlaylistDepth)
			if len(playlistEntries) == 0 {
				fmt.Fprintf(os.Stderr, "Error: playlist contains no playable entries\n")
				os.Exit(1)
			}
		} else if !media.IsSupportedExt(ext) {
			fmt.Fprintf(os.Stderr, "Error: unsupported format %s (supported: %s)\n", ext, media.SupportedExtsList())
			os.Exit(1)
		}
	}

	if len(playlistEntries) > 0 {
		updatedEntries, start, err := openFirstPlayablePlaylistEntry(playlistEntries)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		playlistEntries = updatedEntries
		playlistStartIdx = start.startIdx
		playlistStartCleanup = start.cleanup
		playlistSourcePath = start.sourcePath
		if start.path != "" {
			path = start.path
		}
		if start.player != nil {
			p = start.player
			path = ""
		}
		if start.metaSet {
			meta = start.meta
			metaSet = true
		}
	}

	// Read metadata for local files
	if !metaSet {
		meta = player.ReadMetadata(path)
	}

	// Create audio player
	if p == nil {
		var err error
		p, err = player.New(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating player: %v\n", err)
			os.Exit(1)
		}
	}
	defer p.Close()

	// Create and run TUI
	var model ui.Model
	if len(playlistEntries) > 0 {
		// Build queue from playlist entries in file order.
		tracks := make([]queue.Track, len(playlistEntries))
		for i, e := range playlistEntries {
			title := e.Title
			if title == "" && e.Path != "" {
				title = strings.TrimSuffix(filepath.Base(e.Path), filepath.Ext(e.Path))
			}
			if title == "" && e.URL != "" {
				title = e.URL
			}

			tracks[i] = queue.Track{
				Title: title,
				URL:   e.URL,
				Path:  e.Path,
			}
			if e.URL != "" && e.Path == "" && !downloader.IsLiveURL(e.URL) {
				tracks[i].State = queue.Pending
			} else {
				tracks[i].State = queue.Ready
			}
		}
		tracks[playlistStartIdx].State = queue.Playing
		if playlistStartCleanup != nil {
			tracks[playlistStartIdx].Cleanup = playlistStartCleanup
		}
		q := queue.New(tracks)
		q.SetCurrentIndex(playlistStartIdx)
		model = ui.NewWithQueue(p, meta, playlistSourcePath, q, playlistName)
	} else if downloader.IsURL(arg) {
		model = ui.New(p, meta, sourcePath, originalURL)
	} else if siblings := scanAudioFiles(path); siblings != nil {
		// Build queue from sibling audio files in the same directory
		playlistName = playlistNameFromDirectoryOfFile(path)
		tracks := make([]queue.Track, len(siblings))
		var startIdx int
		absPath, _ := filepath.Abs(path)
		for i, f := range siblings {
			tracks[i] = queue.Track{
				Title: strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)),
				Path:  f,
				State: queue.Ready,
			}
			if f == absPath {
				startIdx = i
			}
		}
		tracks[startIdx].State = queue.Playing
		q := queue.New(tracks)
		q.SetCurrentIndex(startIdx)
		model = ui.NewWithQueue(p, meta, "", q, playlistName)
	} else {
		model = ui.New(p, meta, "", "")
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

func openFirstPlayablePlaylistEntry(entries []media.PlaylistEntry) ([]media.PlaylistEntry, playlistStart, error) {
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
