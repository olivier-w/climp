package aacfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Comcast/gaad"
)

const regressionWindowBytes = 4096

func TestOpenFileReadsAACFixtures(t *testing.T) {
	for _, name := range []string{
		"smoke-aac-12s.aac",
		"smoke-aac-18s.m4a",
		"smoke-aac-45s.m4b",
	} {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(fixturePath(name))
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			defer f.Close()

			reader, err := OpenFile(f)
			if err != nil {
				t.Fatalf("OpenFile() error = %v", err)
			}
			defer reader.Close()

			if reader.Length() <= 0 {
				t.Fatalf("Length() = %d, want > 0", reader.Length())
			}
			if reader.SampleRate() <= 0 {
				t.Fatalf("SampleRate() = %d, want > 0", reader.SampleRate())
			}
			if reader.ChannelCount() < 1 || reader.ChannelCount() > 2 {
				t.Fatalf("ChannelCount() = %d, want 1 or 2", reader.ChannelCount())
			}

			buf := make([]byte, 2048)
			n, err := reader.Read(buf)
			if err != nil && err != io.EOF {
				t.Fatalf("Read() error = %v", err)
			}
			if n == 0 {
				t.Fatal("Read() returned no data")
			}

			if _, err := reader.Seek(reader.Length()/2, io.SeekStart); err != nil {
				t.Fatalf("Seek() error = %v", err)
			}
			n, err = reader.Read(buf)
			if err != nil && err != io.EOF {
				t.Fatalf("Read() after seek error = %v", err)
			}
			if n == 0 {
				t.Fatal("Read() after seek returned no data")
			}

			if _, err := reader.Seek(-int64(len(buf))*2, io.SeekEnd); err != nil {
				t.Fatalf("Seek() near end error = %v", err)
			}
			n, err = reader.Read(buf)
			if err != nil && err != io.EOF {
				t.Fatalf("Read() near end error = %v", err)
			}
			if n == 0 {
				t.Fatal("Read() near end returned no data")
			}
		})
	}
}

func TestReaderFixturePCMHashes(t *testing.T) {
	expected := map[string]map[string]string{
		"smoke-aac-12s.aac": {
			"start": "c0765808d1dd52c5e3244d2834587037a05c98d679351ab7865c4872c972b617",
			"mid":   "63eed12ea0b6973aa4ff502580344dedab3dfc6786a84af0d00457244876ff8f",
			"tail":  "bbb853f548ba9009b7a363605512064a5a4fa97c738d143f93cce8379f5c5dd2",
			"seek":  "a7f3028630a19dcdf2b1fc1faa541521fa1c4a1e220651dbe1024aa0a078a11d",
		},
		"smoke-aac-18s.m4a": {
			"start": "8785131af2bbed47acdbd9016af97613ca654a3dea6fab0d75d00269492c66da",
			"mid":   "62fba1e20266069d22961805ac05cd2aabbe46f74e04f93e521f9e7a501fcdf8",
			"tail":  "13d0bb5bba5a8ea479d464541dde80a160411eb9082e61c599c58ff715fb70a2",
			"seek":  "309b497915d4b76356e9c78ecb7888b6e4a6a7fe4890d220f53581d44ca0d295",
		},
		"smoke-aac-45s.m4b": {
			"start": "0174c76af61bba3a666ac691fc4aa826bfd5730b77f9078d614087558b6b0223",
			"mid":   "753691a8b2a41337384a04810a3c2ec90e63864aa3802f9b4d5b10cb39571e18",
			"tail":  "c8f08e017fdda782c1b36554dd2678d06c98a7349e74d747f09cd0291f0018b9",
			"seek":  "039c57bce0b29392a78eead5f86b664afa48e394661bbd7c3a79e81c785b8c55",
		},
	}

	for name, hashes := range expected {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(fixturePath(name))
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			defer f.Close()

			reader, err := OpenFile(f)
			if err != nil {
				t.Fatalf("OpenFile() error = %v", err)
			}
			defer reader.Close()

			assertWindowHash(t, reader, 0, io.SeekStart, "start", hashes["start"])
			assertWindowHash(t, reader, reader.Length()/2, io.SeekStart, "mid", hashes["mid"])
			assertWindowHash(t, reader, -regressionWindowBytes, io.SeekEnd, "tail", hashes["tail"])
			assertWindowHash(t, reader, reader.Length()/3, io.SeekStart, "seek", hashes["seek"])
		})
	}
}

func TestReaderRepeatedSeekCycles(t *testing.T) {
	f, err := os.Open(fixturePath("smoke-aac-45s.m4b"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer f.Close()

	reader, err := OpenFile(f)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer reader.Close()

	buf := make([]byte, 4096)
	for i := 0; i < 5; i++ {
		if _, err := reader.Seek(0, io.SeekStart); err != nil {
			t.Fatalf("Seek(0) iteration %d error = %v", i, err)
		}
		if n, err := reader.Read(buf); err != nil && err != io.EOF {
			t.Fatalf("Read() after Seek(0) iteration %d error = %v", i, err)
		} else if n == 0 {
			t.Fatalf("Read() after Seek(0) iteration %d returned no data", i)
		}

		if _, err := reader.Seek(reader.Length()/2, io.SeekStart); err != nil {
			t.Fatalf("midpoint Seek() iteration %d error = %v", i, err)
		}
		if n, err := reader.Read(buf); err != nil && err != io.EOF {
			t.Fatalf("Read() after midpoint Seek iteration %d error = %v", i, err)
		} else if n == 0 {
			t.Fatalf("Read() after midpoint Seek iteration %d returned no data", i)
		}
	}
}

func TestOpenCopiesReaderAtInput(t *testing.T) {
	data, err := os.ReadFile(fixturePath("smoke-aac-12s.aac"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	reader, err := Open(bytes.NewReader(data), int64(len(data)), "sample.aac")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reader.Close()

	if reader.Length() <= 0 {
		t.Fatalf("Length() = %d, want > 0", reader.Length())
	}
}

func TestApplyTNSMutatesFixtureSpectrum(t *testing.T) {
	f, err := os.Open(fixturePath("smoke-aac-45s.m4b"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	src, err := parseContainer(".m4b", f, info.Size())
	if err != nil {
		t.Fatalf("parseContainer() error = %v", err)
	}

	au, err := src.readAccessUnit(1, nil)
	if err != nil {
		t.Fatalf("readAccessUnit() error = %v", err)
	}

	adts, err := gaad.ParseADTS(makeADTSFrame(src.asc, au))
	if err != nil {
		t.Fatalf("ParseADTS() error = %v", err)
	}

	stream := adts.Single_channel_elements[0].Channel_stream
	decoder := newSynthDecoder(src.cfg)
	decoded, err := decoder.buildICSDecoded(stream)
	if err != nil {
		t.Fatalf("buildICSDecoded() error = %v", err)
	}
	if !decoded.tnsPresent {
		t.Fatal("decoded frame unexpectedly lacks TNS")
	}

	before := append([]float64(nil), decoded.spec...)
	decoder.applyTNS(decoded)

	changed := false
	for i := range decoded.spec {
		if math.IsNaN(decoded.spec[i]) || math.IsInf(decoded.spec[i], 0) {
			t.Fatalf("applyTNS() produced invalid spectral value at %d", i)
		}
		if decoded.spec[i] != before[i] {
			changed = true
			break
		}
	}
	if !changed {
		t.Fatal("applyTNS() did not modify the TNS-bearing fixture spectrum")
	}
}

func fixturePath(name string) string {
	return filepath.Join("..", "..", "songs", name)
}

func assertWindowHash(t *testing.T, reader *Reader, offset int64, whence int, label, want string) {
	t.Helper()

	if _, err := reader.Seek(offset, whence); err != nil {
		t.Fatalf("%s Seek() error = %v", label, err)
	}

	buf := make([]byte, regressionWindowBytes)
	n, err := io.ReadFull(reader, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		t.Fatalf("%s ReadFull() error = %v", label, err)
	}
	if n != regressionWindowBytes {
		t.Fatalf("%s window size = %d, want %d", label, n, regressionWindowBytes)
	}

	sum := sha256.Sum256(buf[:n])
	got := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("%s sha256 = %s, want %s", label, got, want)
	}
}
