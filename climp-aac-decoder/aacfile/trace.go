package aacfile

// FrameTrace captures per-access-unit synthesis diagnostics. It exists for
// local parity tooling and tests, not normal playback.
type FrameTrace struct {
	AUIndex         int
	PCMStartFrame   int64
	PCMFrames       int
	WindowSequence  uint8
	WindowShape     uint8
	NumWindows      int
	NumWindowGroups int
	MaxSFB          int

	TNSPresent     bool
	TNSFilters     int
	PulsePresent   bool
	PNSBands       int
	IntensityBands int
	MSBands        int
	ESCBands       int
	MaxQuantized   int

	SpecPeakPreTNS  float64
	SpecPeakPostTNS float64
	IMDCTPeak       float64
	OverlapPeak     float64
	PCMPeak         float64
}

// TraceSink receives per-frame diagnostics during decode.
type TraceSink interface {
	OnFrame(FrameTrace)
}

type traceSinkFunc func(FrameTrace)

func (f traceSinkFunc) OnFrame(trace FrameTrace) {
	if f != nil {
		f(trace)
	}
}
