package media

import (
	"strings"
	"testing"
)

func TestIsSupportedExtIncludesAACFamily(t *testing.T) {
	for _, ext := range []string{".aac", ".m4a", ".m4b"} {
		if !IsSupportedExt(ext) {
			t.Fatalf("expected %s to be supported", ext)
		}
	}
}

func TestSupportedExtsListIncludesAACFamily(t *testing.T) {
	list := SupportedExtsList()
	for _, ext := range []string{".aac", ".m4a", ".m4b"} {
		if !strings.Contains(list, ext) {
			t.Fatalf("expected supported ext list to include %s, got %q", ext, list)
		}
	}
}
