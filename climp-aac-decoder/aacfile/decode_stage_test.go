package aacfile

import "testing"

func TestReorderShortSpectralKeepsGroupedSourceStride(t *testing.T) {
	meta := &icsMeta{
		windowSequence:    windowEightShort,
		maxSFB:            12,
		numWindowGroups:   2,
		windowGroupLength: []int{2, 6},
		swbOffset:         []int{0, 4, 8, 12, 16, 20, 28, 36, 44, 56, 68, 80, 96, 112, 128},
	}

	spec := make([]float64, aacFrameSize)
	windowBase := 0
	for g, groupLen := range meta.windowGroupLength {
		groupBase := windowBase * shortWindowLength
		srcOffset := 0
		for sfb := 0; sfb < meta.maxSFB; sfb++ {
			width := meta.swbOffset[sfb+1] - meta.swbOffset[sfb]
			for w := 0; w < groupLen; w++ {
				windowIndex := windowBase + w
				value := float64((g+1)*1000 + windowIndex*100 + sfb)
				for i := 0; i < width; i++ {
					spec[groupBase+srcOffset+i] = value
				}
				srcOffset += width
			}
		}
		windowBase += groupLen
	}

	reordered := reorderShortSpectral(spec, meta)

	windowBase = 0
	for g, groupLen := range meta.windowGroupLength {
		for w := 0; w < groupLen; w++ {
			windowIndex := windowBase + w
			for sfb := 0; sfb < meta.maxSFB; sfb++ {
				start := windowIndex*shortWindowLength + meta.swbOffset[sfb]
				end := windowIndex*shortWindowLength + meta.swbOffset[sfb+1]
				want := float64((g+1)*1000 + windowIndex*100 + sfb)
				for i := start; i < end; i++ {
					if reordered[i] != want {
						t.Fatalf("window %d sfb %d sample %d = %v, want %v", windowIndex, sfb, i, reordered[i], want)
					}
				}
			}
		}
		windowBase += groupLen
	}
}
