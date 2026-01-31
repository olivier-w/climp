package ui

// RepeatMode represents the current repeat setting.
type RepeatMode int

const (
	RepeatOff RepeatMode = iota
	RepeatOne
)

// Next cycles to the next repeat mode.
func (r RepeatMode) Next() RepeatMode {
	switch r {
	case RepeatOff:
		return RepeatOne
	default:
		return RepeatOff
	}
}

// String returns the name of the repeat mode.
func (r RepeatMode) String() string {
	switch r {
	case RepeatOne:
		return "one"
	default:
		return "off"
	}
}

// Icon returns a visual indicator for the repeat mode.
func (r RepeatMode) Icon() string {
	switch r {
	case RepeatOne:
		return "[repeat]"
	default:
		return ""
	}
}
