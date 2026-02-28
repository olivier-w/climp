package media

import "strings"

var audioExts = map[string]bool{
	".mp3":  true,
	".wav":  true,
	".flac": true,
	".ogg":  true,
	".aac":  true,
	".m4a":  true,
	".m4b":  true,
}

var playlistExts = map[string]bool{
	".m3u":  true,
	".m3u8": true,
	".pls":  true,
}

// IsSupportedExt returns true if the extension is a supported playable media format.
func IsSupportedExt(ext string) bool {
	return audioExts[strings.ToLower(ext)]
}

// IsPlaylistExt returns true if the extension is a supported playlist format.
func IsPlaylistExt(ext string) bool {
	return playlistExts[strings.ToLower(ext)]
}

// SupportedExtsList returns a human-readable list of supported playable media formats.
func SupportedExtsList() string {
	return ".mp3, .wav, .flac, .ogg, .aac, .m4a, .m4b"
}
