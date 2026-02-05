package visualizer

import "github.com/charmbracelet/harmonica"

type springField struct {
	spring harmonica.Spring
	pos    []float64
	vel    []float64
}

func newSpringField(fps int, frequency, damping float64) springField {
	return springField{spring: harmonica.NewSpring(harmonica.FPS(fps), frequency, damping)}
}

func (s *springField) resize(n int) {
	if len(s.pos) == n {
		return
	}
	s.pos = make([]float64, n)
	s.vel = make([]float64, n)
}

func (s *springField) step(i int, target float64) float64 {
	p, v := s.spring.Update(s.pos[i], s.vel[i], target)
	s.pos[i] = p
	s.vel[i] = v
	return p
}
