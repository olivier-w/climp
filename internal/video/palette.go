package video

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
)

// ASCII brightness ramp from darkest to brightest.
// Chosen for good perceptual spacing in monospace fonts.
const asciiRamp = " .:-=+*#%@"

// colorMode describes how colors are rendered.
type colorMode uint8

const (
	colorOff     colorMode = iota // NO_COLOR or dumb terminal
	colorANSI16                   // basic 16-color
	colorANSI256                  // 256-color
	colorTrue                     // 24-bit truecolor
)

var (
	detectOnce sync.Once
	termColor  colorMode
)

// detectColorMode checks terminal capabilities once.
func detectColorMode() colorMode {
	detectOnce.Do(func() {
		if _, ok := os.LookupEnv("NO_COLOR"); ok {
			termColor = colorOff
			return
		}
		term := strings.ToLower(os.Getenv("TERM"))
		ct := strings.ToLower(os.Getenv("COLORTERM"))
		switch {
		case strings.Contains(ct, "truecolor"), strings.Contains(ct, "24bit"):
			termColor = colorTrue
		case strings.Contains(term, "256color"):
			termColor = colorANSI256
		case term == "dumb":
			termColor = colorOff
		case term == "" && runtime.GOOS == "windows":
			termColor = colorANSI16
		case term == "":
			termColor = colorOff
		default:
			termColor = colorANSI16
		}
	})
	return termColor
}

// brightnessChar maps a 0-255 luminance to an ASCII character.
func brightnessChar(lum uint8) byte {
	idx := int(lum) * (len(asciiRamp) - 1) / 255
	return asciiRamp[idx]
}

// fgColorSeq returns an ANSI foreground color escape for the given RGB.
// Returns empty string if colors are disabled.
func fgColorSeq(mode colorMode, r, g, b uint8) string {
	switch mode {
	case colorTrue:
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
	case colorANSI256:
		ri := int(r) * 5 / 255
		gi := int(g) * 5 / 255
		bi := int(b) * 5 / 255
		idx := 16 + 36*ri + 6*gi + bi
		return fmt.Sprintf("\x1b[38;5;%dm", idx)
	case colorANSI16:
		return ansi16Approx(r, g, b)
	default:
		return ""
	}
}

// bgColorSeq returns an ANSI background color escape for the given RGB.
func bgColorSeq(mode colorMode, r, g, b uint8) string {
	switch mode {
	case colorTrue:
		return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
	case colorANSI256:
		ri := int(r) * 5 / 255
		gi := int(g) * 5 / 255
		bi := int(b) * 5 / 255
		idx := 16 + 36*ri + 6*gi + bi
		return fmt.Sprintf("\x1b[48;5;%dm", idx)
	case colorANSI16:
		return ansi16ApproxBg(r, g, b)
	default:
		return ""
	}
}

const ansiReset = "\x1b[0m"

// ansi16Approx maps an RGB value to the nearest ANSI 16 foreground color.
func ansi16Approx(r, g, b uint8) string {
	best := 0
	bestDist := 1<<31 - 1
	for i, c := range ansi16Palette {
		dr := int(r) - int(c[0])
		dg := int(g) - int(c[1])
		db := int(b) - int(c[2])
		d := dr*dr + dg*dg + db*db
		if d < bestDist {
			bestDist = d
			best = i
		}
	}
	if best < 8 {
		return fmt.Sprintf("\x1b[%dm", 30+best)
	}
	return fmt.Sprintf("\x1b[%dm", 90+best-8)
}

// ansi16ApproxBg maps an RGB value to the nearest ANSI 16 background color.
func ansi16ApproxBg(r, g, b uint8) string {
	best := 0
	bestDist := 1<<31 - 1
	for i, c := range ansi16Palette {
		dr := int(r) - int(c[0])
		dg := int(g) - int(c[1])
		db := int(b) - int(c[2])
		d := dr*dr + dg*dg + db*db
		if d < bestDist {
			bestDist = d
			best = i
		}
	}
	if best < 8 {
		return fmt.Sprintf("\x1b[%dm", 40+best)
	}
	return fmt.Sprintf("\x1b[%dm", 100+best-8)
}

var ansi16Palette = [16][3]uint8{
	{0, 0, 0},       // black
	{205, 49, 49},   // red
	{13, 188, 121},  // green
	{229, 229, 16},  // yellow
	{36, 114, 200},  // blue
	{188, 63, 188},  // magenta
	{17, 168, 205},  // cyan
	{229, 229, 229}, // white
	{102, 102, 102}, // bright black
	{241, 76, 76},   // bright red
	{35, 209, 139},  // bright green
	{245, 245, 67},  // bright yellow
	{59, 142, 234},  // bright blue
	{214, 112, 214}, // bright magenta
	{41, 184, 219},  // bright cyan
	{255, 255, 255}, // bright white
}
