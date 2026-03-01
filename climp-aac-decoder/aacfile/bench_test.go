package aacfile

import (
	"io"
	"os"
	"testing"
)

const benchmarkReadBufferSize = 32 * 1024

func BenchmarkOpenFile(b *testing.B) {
	for _, name := range []string{
		"smoke-aac-12s.aac",
		"smoke-aac-18s.m4a",
		"smoke-aac-45s.m4b",
		"BeyondGoodEvil_librivox.m4b",
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			path := fixturePath(name)

			for i := 0; i < b.N; i++ {
				f, err := os.Open(path)
				if err != nil {
					b.Fatalf("Open(%q) error = %v", path, err)
				}

				reader, err := OpenFile(f)
				if err != nil {
					_ = f.Close()
					b.Fatalf("OpenFile(%q) error = %v", path, err)
				}

				if err := reader.Close(); err != nil {
					_ = f.Close()
					b.Fatalf("Reader.Close(%q) error = %v", path, err)
				}
				if err := f.Close(); err != nil {
					b.Fatalf("File.Close(%q) error = %v", path, err)
				}
			}
		})
	}
}

func BenchmarkDecodeAll(b *testing.B) {
	for _, name := range []string{
		"smoke-aac-12s.aac",
		"smoke-aac-18s.m4a",
		"smoke-aac-45s.m4b",
		"BeyondGoodEvil_librivox.m4b",
	} {
		b.Run(name, func(b *testing.B) {
			path := fixturePath(name)
			probeFile, err := os.Open(path)
			if err != nil {
				b.Fatalf("Open(%q) error = %v", path, err)
			}
			probeReader, err := OpenFile(probeFile)
			if err != nil {
				_ = probeFile.Close()
				b.Fatalf("OpenFile(%q) error = %v", path, err)
			}
			pcmBytes := probeReader.Length()
			_ = probeReader.Close()
			_ = probeFile.Close()

			b.ReportAllocs()
			b.SetBytes(pcmBytes)

			buf := make([]byte, benchmarkReadBufferSize)
			for i := 0; i < b.N; i++ {
				f, err := os.Open(path)
				if err != nil {
					b.Fatalf("Open(%q) error = %v", path, err)
				}

				reader, err := OpenFile(f)
				if err != nil {
					_ = f.Close()
					b.Fatalf("OpenFile(%q) error = %v", path, err)
				}

				total := int64(0)
				for {
					n, err := reader.Read(buf)
					total += int64(n)
					if err == io.EOF {
						break
					}
					if err != nil {
						_ = reader.Close()
						_ = f.Close()
						b.Fatalf("Read(%q) error = %v", path, err)
					}
				}
				if total != pcmBytes {
					_ = reader.Close()
					_ = f.Close()
					b.Fatalf("decoded PCM bytes = %d, want %d", total, pcmBytes)
				}

				if err := reader.Close(); err != nil {
					_ = f.Close()
					b.Fatalf("Reader.Close(%q) error = %v", path, err)
				}
				if err := f.Close(); err != nil {
					b.Fatalf("File.Close(%q) error = %v", path, err)
				}
			}
		})
	}
}

func BenchmarkSeekReadAudiobook(b *testing.B) {
	path := fixturePath("BeyondGoodEvil_librivox.m4b")
	f, err := os.Open(path)
	if err != nil {
		b.Fatalf("Open(%q) error = %v", path, err)
	}
	defer f.Close()

	reader, err := OpenFile(f)
	if err != nil {
		b.Fatalf("OpenFile(%q) error = %v", path, err)
	}
	defer reader.Close()

	windowSize := minInt64(benchmarkReadBufferSize, reader.Length()/8)
	if windowSize <= 0 {
		b.Fatalf("invalid benchmark window size for %q", path)
	}
	buf := make([]byte, windowSize)

	nearEnd := reader.Length() - int64(windowSize)
	if nearEnd < 0 {
		nearEnd = 0
	}

	b.ReportAllocs()
	b.SetBytes(int64(windowSize * 3))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchmarkSeekReadWindow(b, reader, 0, io.SeekStart, buf)
		benchmarkSeekReadWindow(b, reader, reader.Length()/2, io.SeekStart, buf)
		benchmarkSeekReadWindow(b, reader, nearEnd, io.SeekStart, buf)
	}
}

func benchmarkSeekReadWindow(b *testing.B, reader *Reader, offset int64, whence int, buf []byte) {
	if _, err := reader.Seek(offset, whence); err != nil {
		b.Fatalf("Seek(offset=%d, whence=%d) error = %v", offset, whence, err)
	}

	total := 0
	for total < len(buf) {
		n, err := reader.Read(buf[total:])
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			b.Fatalf("Read() after seek error = %v", err)
		}
	}
	if total != len(buf) {
		b.Fatalf("seek-read bytes = %d, want %d", total, len(buf))
	}
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
