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
// onStatus is called with a clean phase description when the phase changes.
// Returns the path to the temp file, the video title, and a cleanup function.
func Download(url string, onStatus func(string)) (string, string, func(), error) {
	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		return "", "", nil, fmt.Errorf("yt-dlp not found. Install it:\n  Windows: winget install yt-dlp\n  macOS:   brew install yt-dlp\n  Linux:   sudo apt install yt-dlp  (or pip install yt-dlp)")
	}

	tmpFile, err := os.CreateTemp("", "climp-*.wav")
	if err != nil {
		return "", "", nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	cleanup := func() {
		os.Remove(tmpPath)
	}

	// Fetch title with a quick metadata-only call
	var title string
	titleCmd := exec.Command(ytdlp, "--skip-download", "--print", "title", url)
	if titleOut, err := titleCmd.Output(); err == nil {
		title = strings.TrimSpace(string(titleOut))
	}

	// Download audio
	cmd := exec.Command(ytdlp, "-x", "--audio-format", "wav", "-o", tmpPath, "--force-overwrite", url)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("setting up yt-dlp: %w", err)
	}
	cmd.Stdout = cmd.Stderr // merge stdout into stderr pipe

	if err := cmd.Start(); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("starting yt-dlp: %w", err)
	}

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

	return tmpPath, title, cleanup, nil
}
