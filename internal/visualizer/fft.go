package visualizer

import "math"

// fft performs an in-place radix-2 Cooley-Tukey FFT on complex data.
// len(real) and len(imag) must be equal and a power of 2.
func fft(real, imag []float64) {
	n := len(real)
	if n <= 1 {
		return
	}

	// Bit-reversal permutation
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			real[i], real[j] = real[j], real[i]
			imag[i], imag[j] = imag[j], imag[i]
		}
	}

	// Butterfly operations
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		angleStep := -2.0 * math.Pi / float64(size)
		for i := 0; i < n; i += size {
			for k := 0; k < half; k++ {
				angle := angleStep * float64(k)
				wr := math.Cos(angle)
				wi := math.Sin(angle)
				a := i + k
				b := a + half
				tr := wr*real[b] - wi*imag[b]
				ti := wr*imag[b] + wi*real[b]
				real[b] = real[a] - tr
				imag[b] = imag[a] - ti
				real[a] += tr
				imag[a] += ti
			}
		}
	}
}
