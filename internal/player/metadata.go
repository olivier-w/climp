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

// ReadMetadata reads ID3v2 tags from an MP3 file, falling back to filename.
func ReadMetadata(path string) Metadata {
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

	// Fallback: use filename without extension
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	return Metadata{
		Title: name,
	}
}
