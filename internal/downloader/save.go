package downloader

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var invalidFilenameChars = regexp.MustCompile(`[\\/:*?"<>|]`)

// SanitizeFilename strips characters invalid in filenames and trims whitespace.
// Falls back to "download" if the result is empty.
func SanitizeFilename(name string) string {
	name = invalidFilenameChars.ReplaceAllString(name, "")
	name = strings.TrimSpace(name)
	if name == "" {
		return "download"
	}
	return name
}

// SaveFile converts the WAV source file to MP3 via ffmpeg and writes it to the
// current directory using the sanitized title. Returns the destination filename.
func SaveFile(srcPath, title string) (string, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg not found (required for saving)")
	}

	destName := SanitizeFilename(title) + ".mp3"

	cmd := exec.Command(ffmpeg,
		"-n", // never overwrite — fails if file exists (avoids TOCTOU race)
		"-i", srcPath,
		"-q:a", "2",
		destName,
	)
	cmd.Stdin = nil
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg failed: %w\n%s", err, output)
	}

	return destName, nil
}
