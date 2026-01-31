package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/olivier-w/climp/internal/player"
	"github.com/olivier-w/climp/internal/ui"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: climp <file.mp3>\n")
		os.Exit(1)
	}

	path := os.Args[1]

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

	// Read metadata
	meta := player.ReadMetadata(path)

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
