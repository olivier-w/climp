package player

import (
	"path/filepath"
	"strings"

	"github.com/bogem/id3v2/v2"
)

// Metadata holds song information.
type Metadata struct {
	Title  string
	Artist string
	Album  string
}

// ReadMetadata reads tags from an audio file, falling back to filename.
// ID3v2 tags are only read for MP3 files.
func ReadMetadata(path string) Metadata {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".mp3" {
		tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
		if err == nil {
			defer tag.Close()
			m := Metadata{
				Title:  strings.TrimSpace(tag.Title()),
				Artist: strings.TrimSpace(tag.Artist()),
				Album:  strings.TrimSpace(tag.Album()),
			}
			if m.Title != "" {
				return m
			}
		}
	}

	// Fallback: use filename without extension
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	return Metadata{
		Title: name,
	}
}
