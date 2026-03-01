package aacfile

import "math"

type imdctPlan struct {
	n int
}

type imdctWork struct {
	out []float64
}

func newIMDCTPlan(n int) *imdctPlan {
	return &imdctPlan{n: n}
}

func newIMDCTWork(n int) imdctWork {
	return imdctWork{out: make([]float64, 2*n)}
}

func (p *imdctPlan) transform(spec []float64, work *imdctWork) []float64 {
	if cap(work.out) < 2*p.n {
		work.out = make([]float64, 2*p.n)
	}
	out := work.out[:2*p.n]

	nf := float64(p.n)
	scale := 1.0 / nf
	for sample := range out {
		base := math.Pi / nf * (float64(sample) + 0.5 + nf/2)
		stepSin, stepCos := math.Sincos(base)
		phaseSin, phaseCos := math.Sincos(base * 0.5)

		sum := 0.0
		cosTerm := phaseCos
		sinTerm := phaseSin
		for _, coeff := range spec[:p.n] {
			sum += coeff * cosTerm
			nextCos := cosTerm*stepCos - sinTerm*stepSin
			nextSin := sinTerm*stepCos + cosTerm*stepSin
			cosTerm, sinTerm = nextCos, nextSin
		}
		out[sample] = sum * scale
	}

	return out
}
