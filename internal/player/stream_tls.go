package player

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// HLS child requests can use fresh TLS connections without the input options.
// Require verification by default so those connections also check certificates.
func checkStreamTLS(ffmpeg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return checkStreamTLSContext(ctx, ffmpeg)
}

func checkStreamTLSContext(ctx context.Context, ffmpeg string) error {
	output, err := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-h", "protocol=tls").CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return fmt.Errorf("checking ffmpeg TLS support: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "-tls_verify" {
			if strings.Contains(line, "(default true)") || strings.Contains(line, "(default 1)") {
				return nil
			}
			break
		}
	}
	return fmt.Errorf("live streams require ffmpeg with TLS verification enabled by default; update ffmpeg to 9.0.2 or later")
}
