package player

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNeedsLocalFFmpegTranscode(t *testing.T) {
	for _, path := range []string{"track.aac", "track.m4a", "track.m4b"} {
		if !needsLocalFFmpegTranscode(path) {
			t.Fatalf("expected %s to require local ffmpeg transcode", path)
		}
	}
	if needsLocalFFmpegTranscode("track.mp3") {
		t.Fatal("did not expect mp3 to require local ffmpeg transcode")
	}
}

func TestTranscodeToTempWAVMissingFFmpeg(t *testing.T) {
	restore := stubLocalTranscodeDeps()
	defer restore()

	localFFmpegLookPath = func(string) (string, error) {
		return "", errors.New("missing")
	}

	_, _, err := transcodeToTempWAV("track.m4a")
	if err == nil || !strings.Contains(err.Error(), "ffmpeg not found") {
		t.Fatalf("expected ffmpeg not found error, got %v", err)
	}
}

func TestTranscodeToTempWAVSuccessAndCleanup(t *testing.T) {
	restore := stubLocalTranscodeDeps()
	defer restore()

	root := t.TempDir()
	localFFmpegLookPath = func(string) (string, error) {
		return "ffmpeg", nil
	}
	localMkdirTemp = func(dir, pattern string) (string, error) {
		tmpDir := filepath.Join(root, "job")
		if err := os.MkdirAll(tmpDir, 0o755); err != nil {
			return "", err
		}
		return tmpDir, nil
	}
	localFFmpegRun = func(name string, args ...string) ([]byte, error) {
		outPath := args[len(args)-1]
		if err := os.WriteFile(outPath, []byte("RIFF"), 0o644); err != nil {
			return nil, err
		}
		return []byte("ok"), nil
	}

	outPath, cleanup, err := transcodeToTempWAV(filepath.Join(root, "track.m4a"))
	if err != nil {
		t.Fatalf("transcodeToTempWAV() error = %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected cleanup function")
	}
	if filepath.Base(outPath) != "audio.wav" {
		t.Fatalf("expected audio.wav output, got %q", outPath)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output file to exist: %v", err)
	}

	tmpDir := filepath.Dir(outPath)
	cleanup()
	if _, err := os.Stat(tmpDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected cleanup to remove temp dir, stat err = %v", err)
	}
}

func TestTranscodeToTempWAVFailureCleansTempDir(t *testing.T) {
	restore := stubLocalTranscodeDeps()
	defer restore()

	root := t.TempDir()
	tmpDir := filepath.Join(root, "job")
	localFFmpegLookPath = func(string) (string, error) {
		return "ffmpeg", nil
	}
	localMkdirTemp = func(dir, pattern string) (string, error) {
		if err := os.MkdirAll(tmpDir, 0o755); err != nil {
			return "", err
		}
		return tmpDir, nil
	}
	localFFmpegRun = func(name string, args ...string) ([]byte, error) {
		return []byte("decode failed"), errors.New("exit status 1")
	}

	_, _, err := transcodeToTempWAV(filepath.Join(root, "track.m4b"))
	if err == nil || !strings.Contains(err.Error(), "ffmpeg failed to decode local audio") {
		t.Fatalf("expected decode failure, got %v", err)
	}
	if _, statErr := os.Stat(tmpDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected temp dir to be removed after failure, stat err = %v", statErr)
	}
}

func TestCleanupTempDirWithRetryRetries(t *testing.T) {
	restore := stubLocalTranscodeDeps()
	defer restore()

	attempts := 0
	sleeps := 0
	localRemoveAll = func(string) error {
		attempts++
		if attempts < 3 {
			return errors.New("busy")
		}
		return nil
	}
	localSleep = func(_ time.Duration) {
		sleeps++
	}

	cleanupTempDirWithRetry("temp")
	if attempts != 3 {
		t.Fatalf("expected 3 cleanup attempts, got %d", attempts)
	}
	if sleeps != 2 {
		t.Fatalf("expected 2 sleep calls, got %d", sleeps)
	}
}

func TestPlayerCloseRunsCleanupOnce(t *testing.T) {
	calls := 0
	p := &Player{
		stopMon: make(chan struct{}),
		cleanup: func() {
			calls++
		},
	}

	p.Close()
	p.Close()

	if calls != 1 {
		t.Fatalf("expected cleanup to run once, got %d", calls)
	}
}

func stubLocalTranscodeDeps() func() {
	origLookPath := localFFmpegLookPath
	origRun := localFFmpegRun
	origMkdirTemp := localMkdirTemp
	origRemoveAll := localRemoveAll
	origSleep := localSleep
	return func() {
		localFFmpegLookPath = origLookPath
		localFFmpegRun = origRun
		localMkdirTemp = origMkdirTemp
		localRemoveAll = origRemoveAll
		localSleep = origSleep
	}
}
