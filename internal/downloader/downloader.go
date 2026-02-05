package downloader

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DownloadStatus represents the current state of a download.
type DownloadStatus struct {
	Phase     string  // "fetching", "downloading", "converting"
	Percent   float64 // 0.0-1.0, or -1 if indeterminate
	TotalSize string  // e.g. "4.53MiB"
	Speed     string  // e.g. "1.23MiB/s"
	ETA       string  // e.g. "00:03"
}

var errYtdlpNotFound = fmt.Errorf("yt-dlp not found. Install it:\n  Windows: winget install yt-dlp\n  macOS:   brew install yt-dlp\n  Linux:   sudo apt install yt-dlp  (or pip install yt-dlp)")

var downloadLineRe = regexp.MustCompile(
	`\[download\]\s+([\d.]+)%\s+of\s+~?\s*([\d.]+\S+)\s+at\s+([\d.]+\S+)\s+ETA\s+(\S+)`,
)

// DownloadMode selects what streams to keep from a URL download.
type DownloadMode uint8

const (
	// ModeAudioOnly extracts audio and converts to WAV (original behavior).
	ModeAudioOnly DownloadMode = iota
	// ModeVideo downloads merged video+audio as mp4 for terminal video playback.
	ModeVideo
)

// IsURL returns true if the argument looks like a URL.
func IsURL(arg string) bool {
	return strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://")
}

// Download uses yt-dlp to download audio from a URL as WAV.
// onStatus is called with structured progress data as it becomes available.
// Returns the path to the temp file, the video title, and a cleanup function.
func Download(url string, onStatus func(DownloadStatus)) (string, string, func(), error) {
	return DownloadWithMode(url, ModeAudioOnly, onStatus)
}

// DownloadWithMode uses yt-dlp to download media from a URL.
// mode selects audio-only (WAV) or video-preserving (mp4) output.
// onStatus is called with structured progress data as it becomes available.
// Returns the path to the temp file, the video title, and a cleanup function.
func DownloadWithMode(url string, mode DownloadMode, onStatus func(DownloadStatus)) (string, string, func(), error) {
	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		return "", "", nil, errYtdlpNotFound
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
	outTemplate := filepath.Join(tmpDir, "media.%(ext)s")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	args := []string{
		"--no-playlist", // only download the single video, even if URL is a playlist
		"--newline",     // print progress on new lines instead of \r (needed when piped)
		"--progress",    // force progress output even when not connected to a TTY
		"--print", "title",
		"--print", "after_move:filepath",
		"-o", outTemplate,
	}

	switch mode {
	case ModeVideo:
		// Download best video+audio merged into mp4.
		args = append(args,
			"-f", "bestvideo*+bestaudio/best",
			"--merge-output-format", "mp4",
		)
	default:
		// Audio-only: extract and convert to WAV.
		args = append(args, "-x", "--audio-format", "wav")
	}

	args = append(args, url)
	cmd := exec.CommandContext(ctx, ytdlp, args...)
	cmd.Stdin = nil
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

	// Read title and final filepath from stdout, and parse progress lines.
	// With --print and --newline, yt-dlp sends title, [download] progress lines,
	// final filepath, and possibly more [download] lines all to stdout.
	var title, finalPath string
	titleRead := false

	// Drain stderr in background (phase info like "Extracting", "ExtractAudio")
	var stderrWg sync.WaitGroup
	stderrWg.Add(1)
	go func() {
		defer stderrWg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.Contains(line, "Extracting") || strings.Contains(line, "Downloading webpage"):
				if onStatus != nil {
					onStatus(DownloadStatus{Phase: "fetching", Percent: -1})
				}
			case strings.Contains(line, "ExtractAudio"):
				if onStatus != nil {
					onStatus(DownloadStatus{Phase: "converting", Percent: -1})
				}
			}
		}
	}()

	// Parse stdout for title, download progress, and final filepath.
	scanner := bufio.NewScanner(stdout)
	scanner.Split(scanCRLF)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.Contains(line, "[download]") && strings.Contains(line, "%") {
			if onStatus != nil {
				status := DownloadStatus{Phase: "downloading", Percent: -1}
				if m := downloadLineRe.FindStringSubmatch(line); m != nil {
					if pct, err := strconv.ParseFloat(m[1], 64); err == nil {
						status.Percent = pct / 100.0
					}
					status.TotalSize = m[2]
					status.Speed = m[3]
					status.ETA = m[4]
				}
				onStatus(status)
			}
		} else if !titleRead {
			title = line
			titleRead = true
		} else {
			// Last non-download line is the filepath from --print after_move:filepath
			finalPath = line
		}
	}

	stderrWg.Wait()

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

// PlaylistEntry represents a single video/track in a playlist.
type PlaylistEntry struct {
	ID    string
	Title string
	URL   string // actual webpage URL for the entry
}

// ExtractPlaylist runs yt-dlp --flat-playlist to extract track IDs, titles, and URLs.
// Returns nil, nil if the URL is a single video (0 or 1 entries).
// Caps at 50 entries.
func ExtractPlaylist(url string) ([]PlaylistEntry, error) {
	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		return nil, errYtdlpNotFound
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ytdlp,
		"--flat-playlist",
		"--print", "id",
		"--print", "title",
		"--print", "url",
		"--playlist-end", "50",
		url,
	)
	cmd.Stdin = nil

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp playlist extraction failed: %w", err)
	}

	// Split, trim whitespace, and drop empty lines in a single pass.
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}

	// Lines come in triples: id, title, url, id, title, url...
	if len(lines) < 3 {
		return nil, nil // single video, not a playlist
	}

	var entries []PlaylistEntry
	for i := 0; i+2 < len(lines); i += 3 {
		entryURL := lines[i+2]
		// For YouTube, --flat-playlist --print url returns the raw video URL;
		// construct a proper watch URL if it looks like a bare YouTube video ID.
		if !strings.HasPrefix(entryURL, "http") {
			entryURL = "https://www.youtube.com/watch?v=" + lines[i]
		}
		title := lines[i+1]
		if title == "NA" || title == "[Private video]" || title == "[Deleted video]" {
			title = ""
		}
		entries = append(entries, PlaylistEntry{
			ID:    lines[i],
			Title: title,
			URL:   entryURL,
		})
	}

	if len(entries) <= 1 {
		return nil, nil // single video
	}

	return entries, nil
}

// scanCRLF is a bufio.SplitFunc that splits on \n, \r\n, or \r.
// This is needed because yt-dlp uses bare \r to overwrite progress lines in place.
// bufio.ScanLines doesn't handle bare \r as a line terminator.
func scanCRLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i, b := range data {
		if b == '\n' {
			return i + 1, data[:i], nil
		}
		if b == '\r' {
			// \r\n counts as one line break
			if i+1 < len(data) && data[i+1] == '\n' {
				return i + 2, data[:i], nil
			}
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}
