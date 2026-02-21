package downloader

import (
	"errors"
	"testing"
)

func TestNormalizeAndValidateURL(t *testing.T) {
	got, err := normalizeAndValidateURL(` "https://example.com/live.m3u8?token=abc" `)
	if err != nil {
		t.Fatalf("normalizeAndValidateURL() unexpected error: %v", err)
	}
	want := "https://example.com/live.m3u8?token=abc"
	if got != want {
		t.Fatalf("normalizeAndValidateURL() = %q, want %q", got, want)
	}
}

func TestNormalizeAndValidateURLUnsupportedScheme(t *testing.T) {
	_, err := normalizeAndValidateURL("ftp://example.com/stream")
	if !errors.Is(err, ErrUnsupportedScheme) {
		t.Fatalf("normalizeAndValidateURL() error = %v, want ErrUnsupportedScheme", err)
	}
}

func TestIsLiveBySuffix(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{url: "https://radio.example.com/live.m3u8", want: true},
		{url: "https://radio.example.com/live.M3U", want: true},
		{url: "https://radio.example.com/channel.aac?token=abc", want: true},
		{url: "https://example.com/track.mp3", want: false},
		{url: "https://radio.example.com/stream", want: false},
		{url: "https://www.youtube.com/watch?v=dQw4w9WgXcQ", want: false},
	}

	for _, tc := range cases {
		if got := IsLiveBySuffix(tc.url); got != tc.want {
			t.Fatalf("IsLiveBySuffix(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}
