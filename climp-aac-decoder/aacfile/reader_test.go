package aacfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

const seekParityWindowBytes = 4096

var regressionWindowHashes = map[string][]string{
	"smoke-aac-12s.aac": {
		"bfa11e36be409b222927e84594cde093715a80f954d5a71cc261c039e1f5e9c5",
		"f2047ba1afd36b75bf67d561fec4e9058f1e1fcc5120e16c2a92de62a0735b21",
		"fde6d033449a0b621221e4d8dbe6730bc2e300fa4afee54e0b8436ee9109ecac",
		"b54e085f27279b3d38ecf263cd833600fc9385b2144e11b37d5f4f03a472e83d",
	},
	"BeyondGoodEvil_librivox.m4b": {
		"ad7facb2586fc6e966c004d7d1d16b024f5805ff7cb47c7a85dabd8b48892ca7",
		"9340e33a984dff8bb198f2c55aa625b2bd054698bfae34e17cda453f8c1b7d9b",
		"426c66896b9e096599da6699fb7ab4e4c9376d66a62fa7fc89ea2d58f4b93317",
		"291f6b34521652bf157bd858bd8e405c9597f272980743d0812b07ba7b9d2322",
	},
}

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

func TestReaderSeekMatchesContinuousDecode(t *testing.T) {
	f1, err := os.Open(fixturePath("smoke-aac-45s.m4b"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer f1.Close()

	continuous, err := OpenFile(f1)
	if err != nil {
		t.Fatalf("OpenFile() continuous error = %v", err)
	}
	defer continuous.Close()

	f2, err := os.Open(fixturePath("smoke-aac-45s.m4b"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer f2.Close()

	seekable, err := OpenFile(f2)
	if err != nil {
		t.Fatalf("OpenFile() seekable error = %v", err)
	}
	defer seekable.Close()

	offsets := []int64{
		0,
		continuous.Length() / 3,
		continuous.Length() / 2,
		continuous.Length() - seekParityWindowBytes*2,
	}

	for _, offset := range offsets {
		offset -= offset % int64(continuous.ChannelCount()*2)
		if offset < 0 {
			offset = 0
		}
		if offset > continuous.Length() {
			offset = continuous.Length()
		}
	}

	if _, err := continuous.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("continuous Seek(0) error = %v", err)
	}

	streamPos := int64(0)
	skipBuf := make([]byte, minInt(seekParityWindowBytes, 8192))
	for _, offset := range offsets {
		for streamPos < offset {
			need := int(offset - streamPos)
			if need > len(skipBuf) {
				need = len(skipBuf)
			}
			n, err := io.ReadFull(continuous, skipBuf[:need])
			streamPos += int64(n)
			if err != nil {
				t.Fatalf("streaming skip to %d error = %v", offset, err)
			}
		}

		want, err := readWindow(continuous, seekParityWindowBytes)
		if err != nil {
			t.Fatalf("streaming window at %d error = %v", offset, err)
		}
		got, err := readWindowBySeek(seekable, offset, seekParityWindowBytes)
		if err != nil {
			t.Fatalf("seek window at %d error = %v", offset, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("seek window mismatch at offset %d", offset)
		}
		streamPos += int64(len(want))
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

func TestReaderWindowContainsVariation(t *testing.T) {
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

			buf := make([]byte, 4096)
			n, err := io.ReadFull(reader, buf)
			if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
				t.Fatalf("ReadFull() error = %v", err)
			}
			if n < 4 {
				t.Fatalf("window too short: %d", n)
			}

			first := buf[0:2]
			allSame := true
			for i := 2; i+1 < n; i += 2 {
				if buf[i] != first[0] || buf[i+1] != first[1] {
					allSame = false
					break
				}
			}
			if allSame {
				t.Fatal("decoded PCM window has no variation")
			}
		})
	}
}

func TestReaderRegressionWindows(t *testing.T) {
	for name, wantHashes := range regressionWindowHashes {
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

			offsets := []int64{
				0,
				reader.Length() / 3,
				reader.Length() / 2,
				reader.Length() - seekParityWindowBytes,
			}

			for i, offset := range offsets {
				offset -= offset % int64(reader.ChannelCount()*2)
				if offset < 0 {
					offset = 0
				}
				if offset > reader.Length() {
					offset = reader.Length()
				}
				buf, err := readWindowBySeek(reader, offset, seekParityWindowBytes)
				if err != nil {
					t.Fatalf("readWindowBySeek(offset=%d) error = %v", offset, err)
				}
				sum := sha256.Sum256(buf)
				got := hex.EncodeToString(sum[:])
				if got != wantHashes[i] {
					t.Fatalf("window %d hash = %s, want %s", i, got, wantHashes[i])
				}
			}
		})
	}
}

func fixturePath(name string) string {
	return filepath.Join("..", "..", "songs", name)
}

func readWindowBySeek(r *Reader, offset int64, size int) ([]byte, error) {
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return readWindow(r, size)
}

func readWindow(r *Reader, size int) ([]byte, error) {
	buf := make([]byte, size)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
