package player

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// The test executable also supplies fake FFmpeg help without a local install.
func TestMain(m *testing.M) {
	if mode := os.Getenv("CLIMP_TEST_FFMPEG_MODE"); mode != "" {
		if len(os.Args) != 4 || os.Args[1] != "-hide_banner" || os.Args[2] != "-h" || os.Args[3] != "protocol=tls" {
			fmt.Fprintln(os.Stderr, "unexpected fake FFmpeg arguments")
			os.Exit(2)
		}
		if mode == "timeout" {
			time.Sleep(30 * time.Second)
		}
		fmt.Print(os.Getenv("CLIMP_TEST_FFMPEG_OUTPUT"))
		if mode == "failure" {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCheckStreamTLS(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		output  string
		wantErr bool
	}{
		{name: "boolean enabled", output: "tls AVOptions:\n  -tls_verify <boolean> ED... Verify the peer certificate (default true)\n"},
		{name: "integer enabled", output: "  -tls_verify <int> ED... Verify the peer certificate (from 0 to 1) (default 1)\r\n"},
		{name: "boolean disabled", output: "  -tls_verify <boolean> ED... Verify the peer certificate (default false)\n", wantErr: true},
		{name: "integer disabled", output: "  -tls_verify <int> ED... Verify the peer certificate (default 0)\n", wantErr: true},
		{name: "missing option", output: "  -verify <boolean> ED... Verify the peer certificate (default true)\n", wantErr: true},
		{name: "empty help", wantErr: true},
		{name: "missing default", output: "  -tls_verify <boolean> ED... Verify the peer certificate\n", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLIMP_TEST_FFMPEG_MODE", "help")
			t.Setenv("CLIMP_TEST_FFMPEG_OUTPUT", tc.output)
			err := checkStreamTLS(executable)
			if (err != nil) != tc.wantErr {
				t.Fatalf("checkStreamTLS() error = %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr && !strings.Contains(err.Error(), "live streams require ffmpeg") {
				t.Fatalf("expected an unsupported-build error, got %v", err)
			}
		})
	}
}

func TestCheckStreamTLSCommandFailure(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLIMP_TEST_FFMPEG_MODE", "failure")
	t.Setenv("CLIMP_TEST_FFMPEG_OUTPUT", "  -tls_verify <boolean> ED... Verify the peer certificate (default true)\n")
	err = checkStreamTLS(executable)
	if err == nil || !strings.Contains(err.Error(), "checking ffmpeg TLS support") {
		t.Fatalf("expected a command error, got %v", err)
	}
}

func TestCheckStreamTLSTimeout(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLIMP_TEST_FFMPEG_MODE", "timeout")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err = checkStreamTLSContext(ctx, executable)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
