package player

import (
	"os"

	aacfile "github.com/olivier-w/climp-aac-decoder/aacfile"
)

func newAACDecoder(f *os.File) (audioDecoder, error) {
	return aacfile.OpenFile(f)
}
