package visualizer

// Visualizer renders audio data as ASCII art.
type Visualizer interface {
	Name() string
	Update(samples []int16, width, height int)
	View() string
}

// Modes returns all available visualizers.
func Modes() []Visualizer {
	return []Visualizer{
		NewVUMeter(),
		NewSpectrum(),
		NewWaveform(),
	}
}
