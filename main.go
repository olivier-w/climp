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
	var meta player.Metadata
	if downloader.IsURL(arg) {
		// Download the first video immediately (--no-playlist ensures only one).
		// Playlist extraction happens in the background once playback starts.
		dlModel := ui.NewDownload(arg)
		dlProgram := tea.NewProgram(dlModel, tea.WithAltScreen())
		finalModel, err := dlProgram.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		dm, ok := finalModel.(ui.DownloadModel)
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: unexpected model type from downloader\n")
			os.Exit(1)
		}
		result := dm.Result()
		if result.Err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", result.Err)
			os.Exit(1)
		}
		if result.Cleanup != nil {
			defer result.Cleanup()
		}
		path = result.Path

		if result.Title != "" {
			meta = player.Metadata{Title: result.Title}
		} else {
			meta = player.ReadMetadata(path)
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
		if !media.IsSupportedExt(ext) {
			fmt.Fprintf(os.Stderr, "Error: unsupported format %s (supported: %s)\n", ext, media.SupportedExtsList())
			os.Exit(1)
		}
	}

	// Read metadata for local files
	if !downloader.IsURL(arg) {
		meta = player.ReadMetadata(path)
	}

	// Create audio player
	p, err := player.New(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating player: %v\n", err)
		os.Exit(1)
	}
	defer p.Close()

	// Create and run TUI
	var model ui.Model
	if downloader.IsURL(arg) {
		// For URL downloads: mediaPath=path (temp file), sourcePath=path (for saving), originalURL=arg
		model = ui.New(p, meta, path, path, arg)
	} else if siblings := scanMediaFiles(path); siblings != nil {
		// Build queue from sibling media files in the same directory
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
		model = ui.NewWithQueue(p, meta, path, "", q)
	} else {
		// Single local file: mediaPath=path, sourcePath="" (no saving), originalURL=""
		model = ui.New(p, meta, path, "", "")
	}
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// scanMediaFiles returns all supported media files in the same directory as path,
// sorted alphabetically (case-insensitive). Returns nil if fewer than 2 files found.
func scanMediaFiles(path string) []string {
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
