package player

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	localFFmpegLookPath = exec.LookPath
	localFFmpegRun      = func(name string, args ...string) ([]byte, error) {
		cmd := exec.Command(name, args...)
		cmd.Stdin = nil
		return cmd.CombinedOutput()
	}
	localMkdirTemp = os.MkdirTemp
	localRemoveAll = os.RemoveAll
	localSleep     = time.Sleep
)

func needsLocalFFmpegTranscode(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".aac", ".m4a", ".m4b":
		return true
	default:
		return false
	}
}

func transcodeToTempWAV(path string) (string, func(), error) {
	ffmpeg, err := localFFmpegLookPath("ffmpeg")
	if err != nil {
		return "", nil, fmt.Errorf("ffmpeg not found (required for local .aac/.m4a/.m4b playback)")
	}

	tmpDir, err := localMkdirTemp("", "climp-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanup := func() {
		cleanupTempDirWithRetry(tmpDir)
	}

	outPath := filepath.Join(tmpDir, "audio.wav")
	output, err := localFFmpegRun(ffmpeg, "-y", "-i", path, outPath)
	if err != nil {
		cleanup()
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			return "", nil, fmt.Errorf("ffmpeg failed to decode local audio: %w", err)
		}
		return "", nil, fmt.Errorf("ffmpeg failed to decode local audio: %w\n%s", err, msg)
	}

	return outPath, cleanup, nil
}

func cleanupTempDirWithRetry(dir string) {
	for attempt := 0; attempt < 5; attempt++ {
		if err := localRemoveAll(dir); err == nil || !isRetryableCleanupAttempt(attempt) {
			return
		}
		localSleep(75 * time.Millisecond)
	}
}

func isRetryableCleanupAttempt(attempt int) bool {
	return attempt < 4
}
