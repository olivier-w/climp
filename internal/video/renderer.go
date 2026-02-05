package video

import (
	"strings"
)

// Renderer converts raw RGB24 frame data into a terminal string.
// It supports two modes:
//   - Color (half-block): uses "▀" with fg/bg colors to pack 2 pixel rows per terminal row.
//   - ASCII (no color): maps each pixel to a brightness character.
type Renderer struct {
	mode colorMode
	sb   strings.Builder // reusable builder to reduce allocations
}

// NewRenderer creates a renderer using the current terminal's color capabilities.
func NewRenderer() *Renderer {
	return &Renderer{
		mode: detectColorMode(),
	}
}

// Render converts an RGB24 frame buffer into a terminal string.
//
// frameW, frameH: dimensions of the raw frame (in pixels).
// frame: RGB24 data (3 bytes per pixel, row-major, top-to-bottom).
// outW, outH: target terminal cell dimensions.
//
// In color mode, outH terminal rows represent outH*2 pixel rows (half-block packing).
// In ASCII mode, outH terminal rows represent outH pixel rows.
func (r *Renderer) Render(frame []byte, frameW, frameH, outW, outH int) string {
	if len(frame) < frameW*frameH*3 || frameW <= 0 || frameH <= 0 || outW <= 0 || outH <= 0 {
		return ""
	}

	r.sb.Reset()
	// Generous pre-allocation: worst case ~20 bytes per cell (color escapes) + newlines.
	r.sb.Grow(outW * outH * 24)

	if r.mode == colorOff {
		r.renderASCII(frame, frameW, frameH, outW, outH)
	} else {
		r.renderHalfBlock(frame, frameW, frameH, outW, outH)
	}

	return r.sb.String()
}

// renderHalfBlock uses "▀" (upper half block) with fg = top pixel, bg = bottom pixel.
// This packs 2 pixel rows into 1 terminal row.
func (r *Renderer) renderHalfBlock(frame []byte, frameW, frameH, outW, outH int) {
	// Each terminal row covers 2 pixel rows.
	pixelRows := outH * 2

	var lastFg, lastBg string

	for row := 0; row < outH; row++ {
		topPixRow := row * 2
		botPixRow := row*2 + 1

		for col := 0; col < outW; col++ {
			// Map terminal cell to source pixel via nearest-neighbor.
			srcX := col * frameW / outW
			srcY := topPixRow * frameH / pixelRows

			tr, tg, tb := samplePixel(frame, frameW, srcX, srcY)

			// Bottom pixel row (may be out of bounds for odd heights).
			var br, bg, bb uint8
			if botPixRow < pixelRows {
				botSrcY := botPixRow * frameH / pixelRows
				br, bg, bb = samplePixel(frame, frameW, srcX, botSrcY)
			}

			fg := fgColorSeq(r.mode, tr, tg, tb)
			bgc := bgColorSeq(r.mode, br, bg, bb)

			if fg != lastFg {
				r.sb.WriteString(fg)
				lastFg = fg
			}
			if bgc != lastBg {
				r.sb.WriteString(bgc)
				lastBg = bgc
			}
			r.sb.WriteString("▀")
		}

		r.sb.WriteString(ansiReset)
		lastFg = ""
		lastBg = ""
		if row < outH-1 {
			r.sb.WriteByte('\n')
		}
	}
}

// renderASCII maps each pixel to a brightness character.
func (r *Renderer) renderASCII(frame []byte, frameW, frameH, outW, outH int) {
	for row := 0; row < outH; row++ {
		for col := 0; col < outW; col++ {
			srcX := col * frameW / outW
			srcY := row * frameH / outH

			pr, pg, pb := samplePixel(frame, frameW, srcX, srcY)
			lum := luminance(pr, pg, pb)
			r.sb.WriteByte(brightnessChar(lum))
		}
		if row < outH-1 {
			r.sb.WriteByte('\n')
		}
	}
}

// samplePixel reads an RGB triplet from the frame buffer.
func samplePixel(frame []byte, stride, x, y int) (uint8, uint8, uint8) {
	off := (y*stride + x) * 3
	if off+2 >= len(frame) {
		return 0, 0, 0
	}
	return frame[off], frame[off+1], frame[off+2]
}

// luminance computes perceived brightness (ITU-R BT.601).
func luminance(r, g, b uint8) uint8 {
	// 0.299*R + 0.587*G + 0.114*B using integer math.
	return uint8((299*int(r) + 587*int(g) + 114*int(b)) / 1000)
}

// CalcFrameDimensions computes the output pixel dimensions for the ffmpeg scaler
// and the terminal cell dimensions, given terminal bounds and source aspect ratio.
//
// termW, termH: available terminal cells.
// srcW, srcH: source video pixel dimensions.
// color: whether half-block rendering is active (doubles vertical pixel budget).
//
// Returns (outW cells, outH cells, scaleW pixels, scaleH pixels).
func CalcFrameDimensions(termW, termH, srcW, srcH int, color bool) (outW, outH, scaleW, scaleH int) {
	if srcW <= 0 || srcH <= 0 || termW <= 0 || termH <= 0 {
		return 0, 0, 0, 0
	}

	// Terminal cells are roughly 1:2 aspect (width:height in pixels).
	// Each cell is ~1 char wide, ~2 pixels tall.
	// In color mode, half-blocks give us 2 pixel rows per cell row.

	outW = termW
	if color {
		// 2 pixel rows per terminal row.
		outH = termH
		pixelH := termH * 2
		// Compute aspect-correct pixel dimensions.
		// Target aspect: srcW/srcH
		// Available: outW cells wide, pixelH pixels tall
		// Cell aspect correction: each cell is ~0.5 aspect (width/height in char grid).
		aspectSrc := float64(srcW) / float64(srcH)
		aspectTerm := float64(outW) * 0.5 / float64(pixelH) // cell width ~= 0.5 * cell height

		if aspectSrc > aspectTerm {
			// Source is wider: fit to width, reduce height.
			scaleW = outW
			scaleH = int(float64(outW) * 0.5 / aspectSrc)
			if scaleH > pixelH {
				scaleH = pixelH
			}
			outH = (scaleH + 1) / 2 // ceil to terminal rows
		} else {
			// Source is taller: fit to height, reduce width.
			scaleH = pixelH
			scaleW = int(float64(pixelH) * aspectSrc / 0.5)
			if scaleW > outW {
				scaleW = outW
			}
			outH = termH
			outW = scaleW
		}
	} else {
		// ASCII mode: 1 pixel row per terminal row, cell ~2:1 char aspect.
		outH = termH
		aspectSrc := float64(srcW) / float64(srcH)
		// Each char is ~2x tall as wide, so multiply width by 2 for aspect correction.
		aspectTerm := float64(outW) / (float64(outH) * 2.0)

		if aspectSrc > aspectTerm {
			scaleW = outW
			scaleH = int(float64(outW) / aspectSrc / 2.0)
			if scaleH > outH {
				scaleH = outH
			}
			outH = scaleH
		} else {
			scaleH = outH
			scaleW = int(float64(outH) * aspectSrc * 2.0)
			if scaleW > outW {
				scaleW = outW
			}
			outW = scaleW
		}
	}

	// Ensure minimum dimensions.
	if outW < 4 {
		outW = 4
	}
	if outH < 2 {
		outH = 2
	}
	if scaleW < 4 {
		scaleW = 4
	}
	if scaleH < 2 {
		scaleH = 2
	}

	return outW, outH, scaleW, scaleH
}
