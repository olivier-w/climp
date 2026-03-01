package aacfile

import (
	"errors"
	"testing"
)

func TestParseASCAcceptsLCSyncExtensionTail(t *testing.T) {
	cfg, err := parseASC([]byte{0x12, 0x08, 0x56, 0xe5, 0x00})
	if err != nil {
		t.Fatalf("parseASC() error = %v", err)
	}
	if cfg.objectType != aacLCProfile {
		t.Fatalf("objectType = %d, want %d", cfg.objectType, aacLCProfile)
	}
	if cfg.sampleRate != 44100 {
		t.Fatalf("sampleRate = %d, want 44100", cfg.sampleRate)
	}
	if cfg.channelConfig != 1 {
		t.Fatalf("channelConfig = %d, want 1", cfg.channelConfig)
	}
}

func TestParseASCRejectsSBRSyncExtension(t *testing.T) {
	_, err := parseASC([]byte{0x12, 0x08, 0x56, 0xe5, 0xa0})
	if err == nil {
		t.Fatal("parseASC() error = nil, want unsupported AAC error")
	}
	if !errors.Is(err, ErrUnsupportedBitstream) {
		t.Fatalf("parseASC() error = %v, want ErrUnsupportedBitstream", err)
	}
}
