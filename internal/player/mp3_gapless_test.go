package player

import (
	"os"
	"testing"

	"github.com/hajimehoshi/go-mp3"
)

func TestReadMP3GaplessTrim(t *testing.T) {
	path := fixturePath("4 Raws.mp3")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", path, err)
	}
	defer f.Close()

	start, end, err := readMP3GaplessTrim(f)
	if err != nil {
		t.Fatalf("readMP3GaplessTrim() error = %v", err)
	}
	if start != 1105 {
		t.Fatalf("start trim = %d, want 1105", start)
	}
	if end != 1071 {
		t.Fatalf("end trim = %d, want 1071", end)
	}
}

func TestReadMP3GaplessTrimAbsent(t *testing.T) {
	path := fixturePath("arc-radiers-ost.mp3")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", path, err)
	}
	defer f.Close()

	start, end, err := readMP3GaplessTrim(f)
	if err != nil {
		t.Fatalf("readMP3GaplessTrim() error = %v", err)
	}
	if start != 0 || end != 0 {
		t.Fatalf("trim = (%d, %d), want (0, 0)", start, end)
	}
}

func TestNewMP3DecoderAdjustsLengthForGaplessTrim(t *testing.T) {
	path := fixturePath("4 Raws.mp3")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", path, err)
	}
	defer f.Close()

	dec, err := newMP3Decoder(f)
	if err != nil {
		t.Fatalf("newMP3Decoder() error = %v", err)
	}

	rawFile, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open raw(%q) error = %v", path, err)
	}
	defer rawFile.Close()

	rawDec, err := mp3.NewDecoder(rawFile)
	if err != nil {
		t.Fatalf("mp3.NewDecoder() error = %v", err)
	}

	want := rawDec.Length() - int64((1105+1071)*4)
	if got := dec.Length(); got != want {
		t.Fatalf("Length() = %d, want %d", got, want)
	}
	if pos, err := dec.Seek(0, 0); err != nil || pos != 0 {
		t.Fatalf("Seek(0, SeekStart) = (%d, %v), want (0, nil)", pos, err)
	}
}
