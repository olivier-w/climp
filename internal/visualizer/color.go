package visualizer

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
)

type colorProfile uint8

const (
	colorNone colorProfile = iota
	colorANSI16
	colorANSI256
	colorTrueColor
)

type colorRGB struct {
	R uint8
	G uint8
	B uint8
}

var (
	profileOnce sync.Once
	profile     colorProfile
	seqCache    sync.Map
)

func currentColorProfile() colorProfile {
	profileOnce.Do(func() {
		if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
			profile = colorNone
			return
		}
		term := strings.ToLower(os.Getenv("TERM"))
		colorTerm := strings.ToLower(os.Getenv("COLORTERM"))
		switch {
		case strings.Contains(colorTerm, "truecolor"), strings.Contains(colorTerm, "24bit"):
			profile = colorTrueColor
		case strings.Contains(term, "256color"):
			profile = colorANSI256
		case term == "", term == "dumb":
			profile = colorNone
		default:
			profile = colorANSI16
		}
	})
	return profile
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func lerpColor(a, b colorRGB, t float64) colorRGB {
	t = clamp01(t)
	return colorRGB{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
	}
}

func rgbFromHSV(h, s, v float64) colorRGB {
	h = math.Mod(h, 1)
	if h < 0 {
		h += 1
	}
	s = clamp01(s)
	v = clamp01(v)

	i := int(h * 6)
	f := h*6 - float64(i)
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)

	var r, g, b float64
	switch i % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}

	return colorRGB{R: uint8(r * 255), G: uint8(g * 255), B: uint8(b * 255)}
}

func heatColor(t float64) colorRGB {
	t = clamp01(t)
	switch {
	case t < 0.25:
		return lerpColor(colorRGB{R: 16, G: 25, B: 70}, colorRGB{R: 0, G: 174, B: 255}, t/0.25)
	case t < 0.5:
		return lerpColor(colorRGB{R: 0, G: 174, B: 255}, colorRGB{R: 20, G: 255, B: 161}, (t-0.25)/0.25)
	case t < 0.75:
		return lerpColor(colorRGB{R: 20, G: 255, B: 161}, colorRGB{R: 255, G: 230, B: 92}, (t-0.5)/0.25)
	default:
		return lerpColor(colorRGB{R: 255, G: 230, B: 92}, colorRGB{R: 255, G: 80, B: 60}, (t-0.75)/0.25)
	}
}

type ansiState struct {
	profile colorProfile
	current uint32
}

func newANSIState() ansiState {
	return ansiState{profile: currentColorProfile(), current: ^uint32(0)}
}

func (s *ansiState) set(sb *strings.Builder, c colorRGB) {
	if s.profile == colorNone {
		return
	}
	key := uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
	if key == s.current {
		return
	}
	sb.WriteString(colorSequence(s.profile, c))
	s.current = key
}

func (s *ansiState) reset(sb *strings.Builder) {
	if s.profile == colorNone || s.current == ^uint32(0) {
		return
	}
	sb.WriteString("\x1b[0m")
	s.current = ^uint32(0)
}

func colorSequence(profile colorProfile, c colorRGB) string {
	key := uint32(profile)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
	if seq, ok := seqCache.Load(key); ok {
		return seq.(string)
	}

	var seq string
	switch profile {
	case colorTrueColor:
		seq = fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B)
	case colorANSI256:
		r := int(c.R) * 5 / 255
		g := int(c.G) * 5 / 255
		b := int(c.B) * 5 / 255
		idx := 16 + 36*r + 6*g + b
		seq = fmt.Sprintf("\x1b[38;5;%dm", idx)
	case colorANSI16:
		pal := []colorRGB{
			{R: 0, G: 0, B: 0},
			{R: 205, G: 49, B: 49},
			{R: 13, G: 188, B: 121},
			{R: 229, G: 229, B: 16},
			{R: 36, G: 114, B: 200},
			{R: 188, G: 63, B: 188},
			{R: 17, G: 168, B: 205},
			{R: 229, G: 229, B: 229},
		}
		best := 0
		bestDist := math.MaxFloat64
		for i, p := range pal {
			dr := float64(c.R) - float64(p.R)
			dg := float64(c.G) - float64(p.G)
			db := float64(c.B) - float64(p.B)
			d := dr*dr + dg*dg + db*db
			if d < bestDist {
				bestDist = d
				best = i
			}
		}
		seq = fmt.Sprintf("\x1b[%dm", 30+best)
	default:
		seq = ""
	}

	seqCache.Store(key, seq)
	return seq
}
