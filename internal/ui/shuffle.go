package ui

// ShuffleMode represents the current shuffle setting.
type ShuffleMode int

const (
	ShuffleOff ShuffleMode = iota
	ShuffleOn
)

// Toggle switches between shuffle on and off.
func (s ShuffleMode) Toggle() ShuffleMode {
	if s == ShuffleOn {
		return ShuffleOff
	}
	return ShuffleOn
}

// Icon returns a visual indicator for the shuffle mode.
func (s ShuffleMode) Icon() string {
	if s == ShuffleOn {
		return "[shuffle]"
	}
	return ""
}
