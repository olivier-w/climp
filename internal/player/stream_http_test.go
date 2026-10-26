package player

import (
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
)

// Exercise the real FFmpeg HTTP input path when a supported build is installed.
func TestStreamDecoderHTTP(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	if err := checkStreamTLS(ffmpeg); err != nil {
		t.Skipf("ffmpeg does not meet the live stream TLS requirement: %v", err)
	}

	const sampleBytes = 48000 * 2
	wav := make([]byte, 44+sampleBytes)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], sampleBytes+36)
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 48000)
	binary.LittleEndian.PutUint32(wav[28:], 96000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], sampleBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}))
	defer server.Close()

	decoder, err := newStreamDecoder(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	pcm := make([]byte, 4096)
	if _, err := io.ReadFull(decoder, pcm); err != nil {
		t.Fatalf("reading decoded HTTP stream: %v", err)
	}
}
