package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/olivier-w/climp/internal/downloader"
	"github.com/olivier-w/climp/internal/player"
	"github.com/olivier-w/climp/internal/ui"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: climp <file.mp3>\n")
		os.Exit(1)
	}

	arg := os.Args[1]

	var path string
	var meta player.Metadata
	if downloader.IsURL(arg) {
		var mu sync.Mutex
		status := "Fetching info..."

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
			i := 0
			for {
				select {
				case <-ctx.Done():
					fmt.Fprintf(os.Stderr, "\033[2K\r")
					return
				default:
					mu.Lock()
					s := status
					mu.Unlock()
					fmt.Fprintf(os.Stderr, "\033[2K\r  %c %s", frames[i%len(frames)], s)
					i++
					time.Sleep(80 * time.Millisecond)
				}
			}
		}()

		dlPath, title, cleanup, err := downloader.Download(arg, func(s string) {
			mu.Lock()
			status = s
			mu.Unlock()
		})
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer cleanup()
		path = dlPath

		if title != "" {
			meta = player.Metadata{Title: title}
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
		if ext != ".mp3" {
			fmt.Fprintf(os.Stderr, "Error: only .mp3 files are supported (got %s)\n", ext)
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
	model := ui.New(p, meta)
	program := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
