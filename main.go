package main

import (
	"fmt"
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
		openedLive := false
		if downloader.IsLiveBySuffix(arg) {
			var err error
			p, err = player.NewStream(arg)
			if err == nil {
				openedLive = true
				meta = player.Metadata{Title: arg}
				metaSet = true
			}
		}
		if !openedLive {
			result, err := downloadURL(arg)
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
			if len(playlistEntries) == 0 {
				fmt.Fprintf(os.Stderr, "Error: playlist contains no playable entries\n")
				os.Exit(1)
			}
			for i := range playlistEntries {
				e := &playlistEntries[i]
				if e.Path != "" && e.URL == "" {
					path = e.Path
					playlistStartIdx = i
					break
				}
				if e.URL == "" {
					continue
				}

				if downloader.IsLiveBySuffix(e.URL) {
					sp, err := player.NewStream(e.URL)
					if err != nil {
						continue
					}
					p = sp
					playlistStartCleanup = nil
					path = ""
					playlistSourcePath = ""
					meta = player.Metadata{Title: e.Title}
					if meta.Title == "" {
						meta.Title = e.URL
					}
					metaSet = true
					playlistStartIdx = i
					break
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
				playlistStartCleanup = result.Cleanup
				path = e.Path
				playlistSourcePath = e.Path
				meta = player.Metadata{Title: e.Title}
				if meta.Title == "" {
					meta = player.ReadMetadata(path)
				}
				metaSet = true
				playlistStartIdx = i
				break
			}
			if playlistStartIdx < 0 {
				fmt.Fprintf(os.Stderr, "Error: playlist contains no playable entries\n")
				os.Exit(1)
			}
		} else if !media.IsSupportedExt(ext) {
			fmt.Fprintf(os.Stderr, "Error: unsupported format %s (supported: %s)\n", ext, media.SupportedExtsList())
			os.Exit(1)
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
	if downloader.IsURL(arg) {
		model = ui.New(p, meta, sourcePath, originalURL)
	} else if len(playlistEntries) > 0 {
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
			if e.URL != "" && e.Path == "" && !downloader.IsLiveBySuffix(e.URL) {
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
