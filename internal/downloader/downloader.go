package downloader

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsURL returns true if the argument looks like a URL.
func IsURL(arg string) bool {
	return strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")
}

// Download uses yt-dlp to download audio from a URL as MP3.
// onStatus is called with a clean phase description when the phase changes.
// Returns the path to the temp file, the video title, and a cleanup function.
func Download(url string, onStatus func(string)) (string, string, func(), error) {
	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		return "", "", nil, fmt.Errorf("yt-dlp not found. Install it:\n  Windows: winget install yt-dlp\n  macOS:   brew install yt-dlp\n  Linux:   sudo apt install yt-dlp  (or pip install yt-dlp)")
	}

	tmpDir, err := os.MkdirTemp("", "climp-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("creating temp dir: %w", err)
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	// Use a fixed output template inside our temp dir.
	// --print outputs title then final filepath to stdout (one per line).
	outTemplate := filepath.Join(tmpDir, "audio.%(ext)s")
	cmd := exec.Command(ytdlp,
		"-x", "--audio-format", "wav",
		"--print", "title",
		"--print", "after_move:filepath",
		"-o", outTemplate,
		url,
	)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("setting up yt-dlp: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("setting up yt-dlp: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("starting yt-dlp: %w", err)
	}

	// Read title and final filepath from stdout
	var title, finalPath string
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			title = strings.TrimSpace(scanner.Text())
		}
		if scanner.Scan() {
			finalPath = strings.TrimSpace(scanner.Text())
		}
	}()

	// Parse stderr for phase detection
	lastPhase := ""
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		phase := ""
		switch {
		case strings.Contains(line, "Extracting") || strings.Contains(line, "Downloading webpage"):
			phase = "Fetching info..."
		case strings.Contains(line, "[download]") && strings.Contains(line, "%"):
			phase = "Downloading..."
		case strings.Contains(line, "ExtractAudio"):
			phase = "Converting..."
		}
		if phase != "" && phase != lastPhase && onStatus != nil {
			lastPhase = phase
			onStatus(phase)
		}
	}

	if err := cmd.Wait(); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("yt-dlp failed: %w", err)
	}

	if finalPath == "" {
		cleanup()
		return "", "", nil, fmt.Errorf("yt-dlp did not produce an output file")
	}

	return finalPath, title, cleanup, nil
}
