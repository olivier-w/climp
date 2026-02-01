package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/olivier-w/climp/internal/downloader"
	"github.com/olivier-w/climp/internal/player"
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

		result := finalModel.(ui.BrowserModel).Result()
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
		dlModel := ui.NewDownload(arg)
		dlProgram := tea.NewProgram(dlModel, tea.WithAltScreen())
		finalModel, err := dlProgram.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		result := finalModel.(ui.DownloadModel).Result()
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
		switch ext {
		case ".mp3", ".wav", ".flac", ".ogg":
			// supported
		default:
			fmt.Fprintf(os.Stderr, "Error: unsupported format %s (supported: .mp3, .wav, .flac, .ogg)\n", ext)
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
	var sourcePath string
	if downloader.IsURL(arg) {
		sourcePath = path
	}
	model := ui.New(p, meta, sourcePath)
	program := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
