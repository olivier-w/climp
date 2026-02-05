package media

import (
	"path/filepath"
	"strings"
)

// Kind classifies a media source.
type Kind uint8

const (
	KindAudio Kind = iota
	KindVideo
)

var audioExts = map[string]bool{
	".mp3":  true,
	".wav":  true,
	".flac": true,
	".ogg":  true,
}

var videoExts = map[string]bool{
	".mp4":  true,
	".mkv":  true,
	".webm": true,
	".mov":  true,
}

// IsAudioExt returns true if the extension is a supported audio format.
func IsAudioExt(ext string) bool {
	return audioExts[strings.ToLower(ext)]
}

// IsVideoExt returns true if the extension is a supported video format.
func IsVideoExt(ext string) bool {
	return videoExts[strings.ToLower(ext)]
}

// IsSupportedExt returns true if the extension is any supported media format.
func IsSupportedExt(ext string) bool {
	ext = strings.ToLower(ext)
	return audioExts[ext] || videoExts[ext]
}

// NativeAudioExt returns true if the extension has a native (non-ffmpeg) decoder.
func NativeAudioExt(ext string) bool {
	return audioExts[strings.ToLower(ext)]
}

// DetectLocalKind classifies a local file path by its extension.
func DetectLocalKind(path string) Kind {
	ext := strings.ToLower(filepath.Ext(path))
	if videoExts[ext] {
		return KindVideo
	}
	return KindAudio
}

// SupportedExtsList returns a human-readable list of all supported extensions.
func SupportedExtsList() string {
	return ".mp3, .wav, .flac, .ogg, .mp4, .mkv, .webm, .mov"
}
