package player

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNewNativeDecoderOpensAACFixtures(t *testing.T) {
	for _, name := range []string{
		"smoke-aac-12s.aac",
		"smoke-aac-18s.m4a",
		"smoke-aac-45s.m4b",
	} {
		t.Run(name, func(t *testing.T) {
			path := fixturePath(name)
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("Open(%q) error = %v", path, err)
			}
			defer f.Close()

			dec, err := newNativeDecoder(f)
			if err != nil {
				t.Fatalf("newNativeDecoder() error = %v", err)
			}
			if dec.Length() <= 0 {
				t.Fatalf("Length() = %d, want > 0", dec.Length())
			}
			if dec.SampleRate() <= 0 {
				t.Fatalf("SampleRate() = %d, want > 0", dec.SampleRate())
			}
			if dec.ChannelCount() < 1 || dec.ChannelCount() > 2 {
				t.Fatalf("ChannelCount() = %d, want 1 or 2", dec.ChannelCount())
			}

			buf := make([]byte, 512)
			n, err := dec.Read(buf)
			if err != nil && err != io.EOF {
				t.Fatalf("Read() error = %v", err)
			}
			if n == 0 {
				t.Fatal("Read() returned no data")
			}
		})
	}
}

func TestNewDecoderNormalizesAACFixtures(t *testing.T) {
	for _, name := range []string{
		"smoke-aac-12s.aac",
		"smoke-aac-18s.m4a",
		"smoke-aac-45s.m4b",
	} {
		t.Run(name, func(t *testing.T) {
			path := fixturePath(name)
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("Open(%q) error = %v", path, err)
			}
			defer f.Close()

			dec, err := newDecoder(f)
			if err != nil {
				t.Fatalf("newDecoder() error = %v", err)
			}
			if dec.SampleRate() != playbackSampleRate {
				t.Fatalf("SampleRate() = %d, want %d", dec.SampleRate(), playbackSampleRate)
			}
			if dec.ChannelCount() != playbackChannels {
				t.Fatalf("ChannelCount() = %d, want %d", dec.ChannelCount(), playbackChannels)
			}
			if dec.Length() <= 0 {
				t.Fatalf("Length() = %d, want > 0", dec.Length())
			}

			if _, err := dec.Seek(dec.Length()/2, io.SeekStart); err != nil {
				t.Fatalf("Seek() error = %v", err)
			}
			buf := make([]byte, 1024)
			n, err := dec.Read(buf)
			if err != nil && err != io.EOF {
				t.Fatalf("Read() after seek error = %v", err)
			}
			if n == 0 {
				t.Fatal("Read() after seek returned no data")
			}
		})
	}
}

func fixturePath(name string) string {
	return filepath.Join("..", "..", "songs", name)
}
