package ui

// RepeatMode represents the current repeat setting.
type RepeatMode int

const (
	RepeatOff RepeatMode = iota
	RepeatOne
	RepeatAll
)

// Next cycles to the next repeat mode: off → song → playlist → off.
func (r RepeatMode) Next() RepeatMode {
	switch r {
	case RepeatOff:
		return RepeatOne
	case RepeatOne:
		return RepeatAll
	default:
		return RepeatOff
	}
}

// String returns the name of the repeat mode.
func (r RepeatMode) String() string {
	switch r {
	case RepeatOne:
		return "one"
	case RepeatAll:
		return "all"
	default:
		return "off"
	}
}

// Icon returns a visual indicator for the repeat mode.
func (r RepeatMode) Icon() string {
	switch r {
	case RepeatOne:
		return "[repeat song]"
	case RepeatAll:
		return "[repeat playlist]"
	default:
		return ""
	}
}
