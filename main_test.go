package main

import (
	"runtime/debug"
	"testing"
	"time"

	"golang.org/x/mod/module"
)

func TestNormalizeEmbeddedVersion(t *testing.T) {
	timestamp := time.Date(2026, time.March, 2, 16, 22, 24, 0, time.UTC)

	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{
			name:   "release tag",
			input:  "v0.3.0",
			want:   "v0.3.0",
			wantOK: true,
		},
		{
			name:   "prerelease tag",
			input:  "v0.4.0-rc.1",
			want:   "v0.4.0-rc.1",
			wantOK: true,
		},
		{
			name:   "release pseudo version",
			input:  module.PseudoVersion("v0", "v0.3.0", timestamp, "cd1d160b4b02"),
			want:   "v0.3.1",
			wantOK: true,
		},
		{
			name:   "prerelease pseudo version",
			input:  module.PseudoVersion("v0", "v0.4.0-rc.1", timestamp, "cd1d160b4b02"),
			want:   "v0.4.0-rc.1",
			wantOK: true,
		},
		{
			name:   "zero base pseudo version",
			input:  module.PseudoVersion("v0", "", timestamp, "cd1d160b4b02"),
			wantOK: false,
		},
		{
			name:   "devel build info",
			input:  "(devel)",
			wantOK: false,
		},
		{
			name:   "invalid version",
			input:  "not-a-version",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeEmbeddedVersion(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDisplayVersionPriority(t *testing.T) {
	originalVersion := version
	originalReadBuildInfo := readBuildInfo
	originalResolveGitVersion := resolveGitVersion
	t.Cleanup(func() {
		version = originalVersion
		readBuildInfo = originalReadBuildInfo
		resolveGitVersion = originalResolveGitVersion
	})

	tests := []struct {
		name            string
		injectedVersion string
		buildInfo       *debug.BuildInfo
		buildInfoOK     bool
		gitVersion      string
		gitVersionOK    bool
		want            string
	}{
		{
			name:            "injected version wins",
			injectedVersion: "v0.3.0",
			buildInfo:       &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}},
			buildInfoOK:     true,
			gitVersion:      "v1.0.0-dev",
			gitVersionOK:    true,
			want:            "v0.3.0",
		},
		{
			name:            "build info used for go install",
			injectedVersion: "dev",
			buildInfo:       &debug.BuildInfo{Main: debug.Module{Path: "example.com/fork/climp", Version: "v0.3.0"}},
			buildInfoOK:     true,
			gitVersion:      "v1.0.0-dev",
			gitVersionOK:    true,
			want:            "v0.3.0",
		},
		{
			name:            "build info pseudo version normalized before git fallback",
			injectedVersion: "dev",
			buildInfo: &debug.BuildInfo{Main: debug.Module{
				Version: "v0.3.1-0.20260302162224-cd1d160b4b02",
			}},
			buildInfoOK:  true,
			gitVersion:   "v0.3.0-dev",
			gitVersionOK: true,
			want:         "v0.3.1",
		},
		{
			name:            "zero base pseudo version falls through to git",
			injectedVersion: "dev",
			buildInfo: &debug.BuildInfo{Main: debug.Module{
				Version: module.PseudoVersion("v0", "", time.Date(2026, time.March, 2, 16, 22, 24, 0, time.UTC), "cd1d160b4b02"),
			}},
			buildInfoOK:  true,
			gitVersion:   "v0.3.0-dev",
			gitVersionOK: true,
			want:         "v0.3.0-dev",
		},
		{
			name:            "dev fallback when nothing usable exists",
			injectedVersion: "dev",
			buildInfo:       &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			buildInfoOK:     true,
			want:            "dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version = tt.injectedVersion
			readBuildInfo = func() (*debug.BuildInfo, bool) {
				return tt.buildInfo, tt.buildInfoOK
			}
			resolveGitVersion = func() (string, bool) {
				return tt.gitVersion, tt.gitVersionOK
			}

			if got := displayVersion(); got != tt.want {
				t.Fatalf("displayVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
