package aacfile

import "math"

type fftPlan struct {
	n      int
	bitRev []int
	roots  []complex128
}

type imdctPlan struct {
	n   int
	fft *fftPlan
}

type imdctWork struct {
	fftBuf []complex128
	dctBuf []float64
}

func newIMDCTPlan(n int) *imdctPlan {
	return &imdctPlan{
		n:   n,
		fft: newFFTPlan(8 * n),
	}
}

func newIMDCTWork(n int) imdctWork {
	return imdctWork{
		fftBuf: make([]complex128, 8*n),
		dctBuf: make([]float64, n),
	}
}

func newFFTPlan(n int) *fftPlan {
	p := &fftPlan{
		n:      n,
		bitRev: make([]int, n),
		roots:  make([]complex128, n/2),
	}

	bits := 0
	for 1<<bits < n {
		bits++
	}
	for i := 0; i < n; i++ {
		p.bitRev[i] = reverseBits(i, bits)
	}
	for k := 0; k < n/2; k++ {
		angle := -2 * math.Pi * float64(k) / float64(n)
		sin, cos := math.Sincos(angle)
		p.roots[k] = complex(cos, sin)
	}
	return p
}

func reverseBits(v, bits int) int {
	out := 0
	for i := 0; i < bits; i++ {
		out = (out << 1) | (v & 1)
		v >>= 1
	}
	return out
}

func (p *fftPlan) transform(data []complex128) {
	for i := 0; i < p.n; i++ {
		j := p.bitRev[i]
		if j > i {
			data[i], data[j] = data[j], data[i]
		}
	}

	for size := 2; size <= p.n; size <<= 1 {
		half := size / 2
		step := p.n / size
		for start := 0; start < p.n; start += size {
			for i := 0; i < half; i++ {
				twiddle := p.roots[i*step]
				even := data[start+i]
				odd := twiddle * data[start+i+half]
				data[start+i] = even + odd
				data[start+i+half] = even - odd
			}
		}
	}
}

func (p *imdctPlan) transform(spec []float64, work *imdctWork) []float64 {
	fftBuf := work.fftBuf[:8*p.n]
	for i := range fftBuf {
		fftBuf[i] = 0
	}
	for i, coeff := range spec[:p.n] {
		index := 2*i + 1
		value := complex(coeff, 0)
		fftBuf[index] = value
		fftBuf[len(fftBuf)-index] = value
	}

	p.fft.transform(fftBuf)

	dct := work.dctBuf[:p.n]
	for k := 0; k < p.n; k++ {
		dct[k] = real(fftBuf[2*k+1]) * 0.5
	}

	scale := 2.0 / float64(p.n)
	half := p.n / 2
	out := make([]float64, 2*p.n)
	for i := 0; i < half; i++ {
		out[i] = scale * dct[half+i]
		out[half+i] = -scale * dct[p.n-1-i]
		out[p.n+i] = -scale * dct[half-1-i]
		out[p.n+half+i] = -scale * dct[i]
	}
	return out
}
