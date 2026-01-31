package downloader

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// IsURL returns true if the argument looks like a URL.
func IsURL(arg string) bool {
	return strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")
}

// Download uses yt-dlp to download audio from a URL as MP3.
// onProgress is called with each line of yt-dlp output for live status.
// Returns the path to the temp file and a cleanup function.
func Download(url string, onProgress func(string)) (string, func(), error) {
	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		return "", nil, fmt.Errorf("yt-dlp not found. Install it:\n  Windows: winget install yt-dlp\n  macOS:   brew install yt-dlp\n  Linux:   sudo apt install yt-dlp  (or pip install yt-dlp)")
	}

	tmpFile, err := os.CreateTemp("", "climp-*.mp3")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	cleanup := func() {
		os.Remove(tmpPath)
	}

	cmd := exec.Command(ytdlp, "-x", "--audio-format", "mp3", "-o", tmpPath, "--force-overwrite", url)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("setting up yt-dlp: %w", err)
	}
	cmd.Stdout = cmd.Stderr // merge stdout into stderr pipe

	if err := cmd.Start(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("starting yt-dlp: %w", err)
	}

	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		if onProgress != nil {
			onProgress(scanner.Text())
		}
	}

	if err := cmd.Wait(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("yt-dlp failed: %w", err)
	}

	return tmpPath, cleanup, nil
}
