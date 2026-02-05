package video

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// Probe holds video stream metadata from ffprobe.
type Probe struct {
	Width    int
	Height   int
	FPS      float64
	Duration time.Duration
	HasVideo bool
}

type ffprobeVideoResult struct {
	Streams []struct {
		CodecType    string `json:"codec_type"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		RFrameRate   string `json:"r_frame_rate"` // e.g. "30/1" or "24000/1001"
		AvgFrameRate string `json:"avg_frame_rate"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// ProbeMedia uses ffprobe to get video stream metadata.
// Returns HasVideo=false if no video stream exists (audio-only files).
func ProbeMedia(path string) (Probe, error) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return Probe{}, fmt.Errorf("ffprobe not found")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		"-select_streams", "v:0",
		path,
	)
	cmd.Stdin = nil

	output, err := cmd.Output()
	if err != nil {
		return Probe{}, fmt.Errorf("ffprobe failed: %w", err)
	}

	var result ffprobeVideoResult
	if err := json.Unmarshal(output, &result); err != nil {
		return Probe{}, fmt.Errorf("parsing ffprobe output: %w", err)
	}

	// Parse duration from format level.
	durSec, _ := strconv.ParseFloat(result.Format.Duration, 64)
	dur := time.Duration(durSec * float64(time.Second))

	// Find the first video stream.
	for _, s := range result.Streams {
		if s.CodecType != "video" {
			continue
		}
		fps := parseFraction(s.AvgFrameRate)
		if fps <= 0 {
			fps = parseFraction(s.RFrameRate)
		}
		if fps <= 0 {
			fps = 24 // sensible fallback
		}
		return Probe{
			Width:    s.Width,
			Height:   s.Height,
			FPS:      fps,
			Duration: dur,
			HasVideo: true,
		}, nil
	}

	return Probe{Duration: dur, HasVideo: false}, nil
}

// parseFraction parses "num/den" into a float64.
func parseFraction(s string) float64 {
	parts := splitFraction(s)
	if len(parts) != 2 {
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0
	}
	return num / den
}

// splitFraction splits "a/b" into ["a", "b"].
func splitFraction(s string) []string {
	for i, c := range s {
		if c == '/' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return nil
}
