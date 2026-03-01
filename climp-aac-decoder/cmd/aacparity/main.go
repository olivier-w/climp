package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	aacfile "github.com/olivier-w/climp-aac-decoder/aacfile"
)

type parityConfig struct {
	scan             bool
	stepFrames       int
	top              int
	trace            bool
	traceWindowSec   float64
	traceWindowCount int
}

type parityReport struct {
	Input          string              `json:"input"`
	Backend        string              `json:"backend"`
	FFmpegPath     string              `json:"ffmpeg_path"`
	Container      string              `json:"container"`
	SampleRate     int                 `json:"sample_rate"`
	Channels       int                 `json:"channels"`
	PCMBytes       int64               `json:"pcm_bytes"`
	WindowBytes    int                 `json:"window_bytes"`
	Scan           bool                `json:"scan"`
	StepFrames     int                 `json:"step_frames,omitempty"`
	ScannedWindows int                 `json:"scanned_windows,omitempty"`
	Windows        []windowReport      `json:"windows"`
	TraceWindows   []traceWindowReport `json:"trace_windows,omitempty"`
}

type windowReport struct {
	Label       string  `json:"label,omitempty"`
	WindowIndex int     `json:"window_index,omitempty"`
	OffsetBytes int64   `json:"offset_bytes"`
	OffsetSec   float64 `json:"offset_seconds"`
	Frames      int     `json:"frames"`
	Correlation float64 `json:"correlation"`
	SNRdB       float64 `json:"snr_db"`
	MaxAbsDiff  int     `json:"max_abs_diff"`
	RMSDiff     float64 `json:"rms_diff"`
	NativePeak  float64 `json:"native_peak_dbfs"`
	NativeClips int     `json:"native_clips"`
	RefPeak     float64 `json:"reference_peak_dbfs"`
	RefClips    int     `json:"reference_clips"`
}

type traceWindowReport struct {
	Label       string             `json:"label"`
	WindowIndex int                `json:"window_index,omitempty"`
	OffsetBytes int64              `json:"offset_bytes"`
	OffsetSec   float64            `json:"offset_seconds"`
	Frames      int                `json:"frames"`
	TraceFrames []frameTraceReport `json:"trace_frames"`
}

type frameTraceReport struct {
	AUIndex         int     `json:"au_index"`
	PCMStartFrame   int64   `json:"pcm_start_frame"`
	PCMFrames       int     `json:"pcm_frames"`
	WindowSequence  uint8   `json:"window_sequence"`
	WindowShape     uint8   `json:"window_shape"`
	NumWindows      int     `json:"num_windows"`
	NumWindowGroups int     `json:"num_window_groups"`
	MaxSFB          int     `json:"max_sfb"`
	TNSPresent      bool    `json:"tns_present"`
	TNSFilters      int     `json:"tns_filters"`
	PulsePresent    bool    `json:"pulse_present"`
	PNSBands        int     `json:"pns_bands"`
	IntensityBands  int     `json:"intensity_bands"`
	MSBands         int     `json:"ms_bands"`
	ESCBands        int     `json:"esc_bands"`
	MaxQuantized    int     `json:"max_quantized"`
	SpecPeakPreTNS  float64 `json:"spec_peak_pre_tns"`
	SpecPeakPostTNS float64 `json:"spec_peak_post_tns"`
	IMDCTPeak       float64 `json:"imdct_peak"`
	OverlapPeak     float64 `json:"overlap_peak"`
	PCMPeak         float64 `json:"pcm_peak"`
}

type pcmStats struct {
	peakDBFS float64
	clips    int
}

type parityOffset struct {
	label string
	index int
	offset int64
}

type referencePCM struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr bytes.Buffer
}

func main() {
	var (
		inputPath   string
		ffmpegPath  string
		reportPath  string
		backend     string
		windowFrame int
		cfg         parityConfig
	)

	flag.StringVar(&inputPath, "input", "", "path to a local .aac/.m4a/.m4b file")
	flag.StringVar(&ffmpegPath, "ffmpeg", "ffmpeg", "path to ffmpeg executable")
	flag.StringVar(&reportPath, "report", "", "optional path to write JSON report")
	flag.StringVar(&backend, "backend", "reference", "decoder backend to validate")
	flag.IntVar(&windowFrame, "window-frames", 16384, "PCM frames per comparison window")
	flag.BoolVar(&cfg.scan, "scan", false, "scan the whole stream and report the worst windows")
	flag.IntVar(&cfg.stepFrames, "step-frames", 16384, "PCM frame step between scan windows")
	flag.IntVar(&cfg.top, "top", 20, "number of worst scan windows to report")
	flag.BoolVar(&cfg.trace, "trace", false, "collect per-access-unit traces around the reported windows")
	flag.Float64Var(&cfg.traceWindowSec, "trace-window-sec", 2, "seconds of extra context to trace on both sides of a reported window")
	flag.IntVar(&cfg.traceWindowCount, "trace-window-count", 3, "number of reported windows to trace")
	flag.Parse()

	if inputPath == "" {
		exitf("missing -input")
	}
	if backend != "reference" {
		exitf("unsupported backend %q (only \"reference\" is currently available)", backend)
	}
	if windowFrame <= 0 {
		exitf("invalid -window-frames: %d", windowFrame)
	}
	if cfg.scan && cfg.stepFrames <= 0 {
		exitf("invalid -step-frames: %d", cfg.stepFrames)
	}
	if cfg.top <= 0 {
		exitf("invalid -top: %d", cfg.top)
	}
	if cfg.traceWindowCount <= 0 {
		exitf("invalid -trace-window-count: %d", cfg.traceWindowCount)
	}
	if cfg.traceWindowSec < 0 {
		exitf("invalid -trace-window-sec: %f", cfg.traceWindowSec)
	}

	report, err := runParity(inputPath, ffmpegPath, backend, windowFrame, cfg)
	if err != nil {
		exitf("%v", err)
	}

	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		exitf("encoding report: %v", err)
	}
	if reportPath == "" {
		fmt.Println(string(out))
		return
	}
	if err := os.WriteFile(reportPath, out, 0o644); err != nil {
		exitf("writing report: %v", err)
	}
	fmt.Println(reportPath)
}

func runParity(inputPath, ffmpegPath, backend string, windowFrames int, cfg parityConfig) (*parityReport, error) {
	f, err := os.Open(inputPath)
	if err != nil {
		return nil, fmt.Errorf("open input: %w", err)
	}
	defer f.Close()

	reader, err := aacfile.OpenFile(f)
	if err != nil {
		return nil, fmt.Errorf("open native AAC reader: %w", err)
	}
	defer reader.Close()

	info := reader.Info()
	frameSize := info.ChannelCount * 2
	windowBytes := windowFrames * frameSize

	report := &parityReport{
		Input:       inputPath,
		Backend:     backend,
		FFmpegPath:  ffmpegPath,
		Container:   filepath.Ext(inputPath),
		SampleRate:  info.SampleRate,
		Channels:    info.ChannelCount,
		PCMBytes:    reader.Length(),
		WindowBytes: windowBytes,
		Scan:        cfg.scan,
	}

	if cfg.scan {
		report.StepFrames = cfg.stepFrames
		windows, traces, scanned, err := runScanParity(inputPath, ffmpegPath, info, reader.Length(), windowFrames, windowBytes, cfg)
		if err != nil {
			return nil, err
		}
		report.Windows = windows
		report.TraceWindows = traces
		report.ScannedWindows = scanned
		return report, nil
	}

	offsets := parityOffsets(reader.Length(), int64(windowBytes))
	for i := range offsets {
		offsets[i].offset = alignPCMOffset(offsets[i].offset, reader.Length(), int64(frameSize))
	}
	referenceWindows, err := readReferenceWindows(ffmpegPath, inputPath, info.SampleRate, info.ChannelCount, offsets, windowBytes)
	if err != nil {
		return nil, err
	}

	report.Windows = make([]windowReport, 0, len(offsets))
	for _, item := range offsets {
		native, err := readNativeWindow(reader, item.offset, windowBytes)
		if err != nil {
			return nil, fmt.Errorf("read native %s window: %w", item.label, err)
		}
		ref := referenceWindows[item.label]
		if len(ref) != len(native) {
			return nil, fmt.Errorf("%s window length mismatch: native=%d reference=%d", item.label, len(native), len(ref))
		}

		report.Windows = append(report.Windows, buildWindowReport(item, native, ref, frameSize, info.SampleRate))
	}
	return report, nil
}

func runScanParity(inputPath, ffmpegPath string, info aacfile.Info, pcmBytes int64, windowFrames, windowBytes int, cfg parityConfig) ([]windowReport, []traceWindowReport, int, error) {
	frameSize := info.ChannelCount * 2
	stepBytes := cfg.stepFrames * frameSize
	offsets := scanOffsets(pcmBytes, int64(windowBytes), int64(stepBytes))
	if len(offsets) == 0 {
		return nil, nil, 0, fmt.Errorf("scan produced no windows")
	}

	nativeFile, err := os.Open(inputPath)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("open native scan input: %w", err)
	}
	defer nativeFile.Close()

	nativeReader, err := aacfile.OpenFile(nativeFile)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("open native scan reader: %w", err)
	}
	defer nativeReader.Close()

	refPCM, err := openReferencePCM(ffmpegPath, inputPath, info.SampleRate, info.ChannelCount)
	if err != nil {
		return nil, nil, 0, err
	}
	defer func() {
		_ = refPCM.close()
	}()

	allWindows, err := compareSequentialWindows(nativeReader, refPCM.stdout, offsets, windowBytes, frameSize, info.SampleRate)
	if err != nil {
		return nil, nil, 0, err
	}
	if err := refPCM.wait(); err != nil {
		return nil, nil, 0, err
	}

	sort.Slice(allWindows, func(i, j int) bool {
		return worseWindow(allWindows[i], allWindows[j])
	})
	if cfg.top < len(allWindows) {
		allWindows = allWindows[:cfg.top]
	}

	var traceWindows []traceWindowReport
	if cfg.trace && len(allWindows) > 0 {
		traceCount := cfg.traceWindowCount
		if traceCount > len(allWindows) {
			traceCount = len(allWindows)
		}
		traceWindows, err = captureTraceWindows(inputPath, info, windowBytes, allWindows[:traceCount], cfg.traceWindowSec)
		if err != nil {
			return nil, nil, 0, err
		}
	}

	return allWindows, traceWindows, len(offsets), nil
}

func parityOffsets(length, windowBytes int64) []parityOffset {
	if windowBytes <= 0 {
		return nil
	}
	clamp := func(v int64) int64 {
		if v < 0 {
			return 0
		}
		max := length - windowBytes
		if max < 0 {
			max = 0
		}
		if v > max {
			return max
		}
		return v
	}
	return []parityOffset{
		{label: "start", offset: 0},
		{label: "seek", offset: clamp(length / 3)},
		{label: "mid", offset: clamp(length / 2)},
		{label: "tail", offset: clamp(length - windowBytes)},
	}
}

func scanOffsets(length, windowBytes, stepBytes int64) []parityOffset {
	if windowBytes <= 0 || stepBytes <= 0 || length <= 0 {
		return nil
	}
	if windowBytes > length {
		windowBytes = length
	}
	maxOffset := length - windowBytes
	if maxOffset < 0 {
		maxOffset = 0
	}

	var offsets []parityOffset
	for index, offset := 0, int64(0); offset <= maxOffset; index, offset = index+1, offset+stepBytes {
		offsets = append(offsets, parityOffset{index: index, offset: offset})
	}
	if last := offsets[len(offsets)-1].offset; last != maxOffset {
		offsets = append(offsets, parityOffset{index: len(offsets), offset: maxOffset})
	}
	return offsets
}

func alignPCMOffset(offset, length, frameSize int64) int64 {
	if frameSize <= 0 {
		return offset
	}
	if offset < 0 {
		offset = 0
	}
	if offset > length {
		offset = length
	}
	return offset - offset%frameSize
}

func readNativeWindow(r *aacfile.Reader, offset int64, size int) ([]byte, error) {
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	n, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

func readReferenceWindows(ffmpegPath, inputPath string, sampleRate, channels int, offsets []parityOffset, size int) (map[string][]byte, error) {
	sorted := append([]parityOffset(nil), offsets...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].offset == sorted[j].offset {
			return sorted[i].label < sorted[j].label
		}
		return sorted[i].offset < sorted[j].offset
	})

	refPCM, err := openReferencePCM(ffmpegPath, inputPath, sampleRate, channels)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = refPCM.close()
	}()

	windows := make(map[string][]byte, len(sorted))
	current := int64(0)
	for _, item := range sorted {
		if item.offset < current {
			return nil, fmt.Errorf("reference offsets must be ascending")
		}
		if skip := item.offset - current; skip > 0 {
			skipped, err := io.CopyN(io.Discard, refPCM.stdout, skip)
			current += skipped
			if err != nil {
				return nil, fmt.Errorf("skip ffmpeg PCM for %s: %w%s", item.label, err, formatStderr(refPCM.stderr.Bytes()))
			}
		}

		buf := make([]byte, size)
		if _, err := io.ReadFull(refPCM.stdout, buf); err != nil {
			return nil, fmt.Errorf("read ffmpeg PCM for %s: %w%s", item.label, err, formatStderr(refPCM.stderr.Bytes()))
		}
		windows[item.label] = buf
		current += int64(len(buf))
	}
	if err := refPCM.wait(); err != nil {
		return nil, err
	}
	return windows, nil
}

func compareSequentialWindows(native io.Reader, reference io.Reader, offsets []parityOffset, windowBytes, frameSize, sampleRate int) ([]windowReport, error) {
	type rollingBuffer struct {
		start int64
		data  []byte
	}

	var nativeBuf, refBuf rollingBuffer
	reports := make([]windowReport, 0, len(offsets))
	nativeChunk := make([]byte, 64*1024)
	refChunk := make([]byte, 64*1024)
	current := int64(0)
	next := 0

	appendChunk := func(buf *rollingBuffer, chunk []byte) {
		buf.data = append(buf.data, chunk...)
	}
	trimBuffer := func(buf *rollingBuffer, keepFrom int64) {
		if keepFrom <= buf.start {
			return
		}
		if keepFrom >= buf.start+int64(len(buf.data)) {
			buf.data = buf.data[:0]
			buf.start = keepFrom
			return
		}
		trim := int(keepFrom - buf.start)
		copy(buf.data, buf.data[trim:])
		buf.data = buf.data[:len(buf.data)-trim]
		buf.start = keepFrom
	}
	windowSlice := func(buf *rollingBuffer, start, end int64) ([]byte, error) {
		if start < buf.start || end > buf.start+int64(len(buf.data)) {
			return nil, fmt.Errorf("window [%d,%d) outside buffered range [%d,%d)", start, end, buf.start, buf.start+int64(len(buf.data)))
		}
		from := int(start - buf.start)
		to := int(end - buf.start)
		return buf.data[from:to], nil
	}

	for next < len(offsets) {
		targetEnd := offsets[next].offset + int64(windowBytes)
		for current < targetEnd {
			want := len(nativeChunk)
			if remaining := targetEnd - current; remaining < int64(want) {
				want = int(remaining)
			}
			nativeN, nativeErr := readPCMChunk(native, nativeChunk[:want])
			refN, refErr := readPCMChunk(reference, refChunk[:want])
			if nativeN != refN {
				return nil, fmt.Errorf("native/reference byte count mismatch: native=%d reference=%d", nativeN, refN)
			}
			if nativeN > 0 {
				appendChunk(&nativeBuf, nativeChunk[:nativeN])
				appendChunk(&refBuf, refChunk[:refN])
				current += int64(nativeN)
			}
			if nativeErr != nil || refErr != nil {
				if errors.Is(nativeErr, io.EOF) && errors.Is(refErr, io.EOF) && current >= targetEnd {
					break
				}
				if errors.Is(nativeErr, io.EOF) && errors.Is(refErr, io.EOF) {
					return nil, fmt.Errorf("unexpected EOF while building scan window at byte %d", offsets[next].offset)
				}
				if nativeErr != nil && !errors.Is(nativeErr, io.EOF) {
					return nil, fmt.Errorf("read native PCM: %w", nativeErr)
				}
				if refErr != nil && !errors.Is(refErr, io.EOF) {
					return nil, fmt.Errorf("read reference PCM: %w", refErr)
				}
			}
			if nativeN == 0 && refN == 0 && nativeErr == nil && refErr == nil {
				return nil, fmt.Errorf("zero-length PCM read without EOF")
			}
		}

		for next < len(offsets) && current >= offsets[next].offset+int64(windowBytes) {
			start := offsets[next].offset
			end := start + int64(windowBytes)
			nativeWindow, err := windowSlice(&nativeBuf, start, end)
			if err != nil {
				return nil, err
			}
			refWindow, err := windowSlice(&refBuf, start, end)
			if err != nil {
				return nil, err
			}
			reports = append(reports, buildWindowReport(offsets[next], nativeWindow, refWindow, frameSize, sampleRate))
			next++
		}

		if next < len(offsets) {
			keepFrom := offsets[next].offset
			trimBuffer(&nativeBuf, keepFrom)
			trimBuffer(&refBuf, keepFrom)
		}
	}

	return reports, nil
}

func readPCMChunk(r io.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			if err == io.EOF && total > 0 {
				return total, io.EOF
			}
			return total, err
		}
		if n == 0 {
			if total > 0 {
				return total, nil
			}
			return 0, io.ErrNoProgress
		}
	}
	return total, nil
}

func buildWindowReport(item parityOffset, native, ref []byte, frameSize, sampleRate int) windowReport {
	label := item.label
	if label == "" {
		label = fmt.Sprintf("scan-%06d", item.index)
	}
	corr, snr, maxDiff, rmsDiff := comparePCM(native, ref)
	nativeStats := analyzePCM(native)
	refStats := analyzePCM(ref)
	return windowReport{
		Label:       label,
		WindowIndex: item.index,
		OffsetBytes: item.offset,
		OffsetSec:   float64(item.offset) / float64(frameSize) / float64(sampleRate),
		Frames:      len(native) / frameSize,
		Correlation: sanitizeReportFloat(corr),
		SNRdB:       sanitizeReportFloat(snr),
		MaxAbsDiff:  maxDiff,
		RMSDiff:     sanitizeReportFloat(rmsDiff),
		NativePeak:  sanitizeReportFloat(nativeStats.peakDBFS),
		NativeClips: nativeStats.clips,
		RefPeak:     sanitizeReportFloat(refStats.peakDBFS),
		RefClips:    refStats.clips,
	}
}

func worseWindow(a, b windowReport) bool {
	if a.SNRdB != b.SNRdB {
		return a.SNRdB < b.SNRdB
	}
	if a.Correlation != b.Correlation {
		return a.Correlation < b.Correlation
	}
	if a.RMSDiff != b.RMSDiff {
		return a.RMSDiff > b.RMSDiff
	}
	if a.MaxAbsDiff != b.MaxAbsDiff {
		return a.MaxAbsDiff > b.MaxAbsDiff
	}
	return a.OffsetBytes < b.OffsetBytes
}

func captureTraceWindows(inputPath string, info aacfile.Info, windowBytes int, windows []windowReport, extraSeconds float64) ([]traceWindowReport, error) {
	f, err := os.Open(inputPath)
	if err != nil {
		return nil, fmt.Errorf("open trace input: %w", err)
	}
	defer f.Close()

	frameSize := int64(info.ChannelCount * 2)
	marginFrames := int64(extraSeconds * float64(info.SampleRate))
	targets := make([]*traceWindowCollector, len(windows))
	maxTraceBytes := int64(0)
	for i, window := range windows {
		startFrame := window.OffsetBytes / frameSize
		endFrame := startFrame + int64(window.Frames)
		targets[i] = &traceWindowCollector{
			window: window,
			start:  maxInt64(0, startFrame-marginFrames),
			end:    endFrame + marginFrames,
		}
		traceEndBytes := targets[i].end * frameSize
		if traceEndBytes > maxTraceBytes {
			maxTraceBytes = traceEndBytes
		}
	}

	collector := &traceCollector{targets: targets}
	reader, err := aacfile.OpenFileWithTrace(f, collector)
	if err != nil {
		return nil, fmt.Errorf("open trace reader: %w", err)
	}
	defer reader.Close()

	if maxTraceBytes > reader.Length() {
		maxTraceBytes = reader.Length()
	}
	buf := make([]byte, 64*1024)
	var readBytes int64
	for readBytes < maxTraceBytes {
		want := len(buf)
		remaining := maxTraceBytes - readBytes
		if remaining < int64(want) {
			want = int(remaining)
		}
		n, err := reader.Read(buf[:want])
		readBytes += int64(n)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read trace PCM: %w", err)
		}
	}

	reports := make([]traceWindowReport, 0, len(targets))
	for _, target := range targets {
		reports = append(reports, traceWindowReport{
			Label:       target.window.Label,
			WindowIndex: target.window.WindowIndex,
			OffsetBytes: target.window.OffsetBytes,
			OffsetSec:   target.window.OffsetSec,
			Frames:      target.window.Frames,
			TraceFrames: target.frames,
		})
	}
	return reports, nil
}

type traceCollector struct {
	targets []*traceWindowCollector
}

type traceWindowCollector struct {
	window windowReport
	start  int64
	end    int64
	frames []frameTraceReport
}

func (c *traceCollector) OnFrame(trace aacfile.FrameTrace) {
	if c == nil {
		return
	}
	start := trace.PCMStartFrame
	end := start + int64(trace.PCMFrames)
	for _, target := range c.targets {
		if end <= target.start || start >= target.end {
			continue
		}
		target.frames = append(target.frames, frameTraceReport{
			AUIndex:         trace.AUIndex,
			PCMStartFrame:   trace.PCMStartFrame,
			PCMFrames:       trace.PCMFrames,
			WindowSequence:  trace.WindowSequence,
			WindowShape:     trace.WindowShape,
			NumWindows:      trace.NumWindows,
			NumWindowGroups: trace.NumWindowGroups,
			MaxSFB:          trace.MaxSFB,
			TNSPresent:      trace.TNSPresent,
			TNSFilters:      trace.TNSFilters,
			PulsePresent:    trace.PulsePresent,
			PNSBands:        trace.PNSBands,
			IntensityBands:  trace.IntensityBands,
			MSBands:         trace.MSBands,
			ESCBands:        trace.ESCBands,
			MaxQuantized:    trace.MaxQuantized,
			SpecPeakPreTNS:  sanitizeReportFloat(trace.SpecPeakPreTNS),
			SpecPeakPostTNS: sanitizeReportFloat(trace.SpecPeakPostTNS),
			IMDCTPeak:       sanitizeReportFloat(trace.IMDCTPeak),
			OverlapPeak:     sanitizeReportFloat(trace.OverlapPeak),
			PCMPeak:         sanitizeReportFloat(trace.PCMPeak),
		})
	}
}

func openReferencePCM(ffmpegPath, inputPath string, sampleRate, channels int) (*referencePCM, error) {
	cmd := exec.Command(
		ffmpegPath,
		"-v", "error",
		"-i", inputPath,
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-ac", fmt.Sprintf("%d", channels),
		"-ar", fmt.Sprintf("%d", sampleRate),
		"pipe:1",
	)

	ref := &referencePCM{cmd: cmd}
	cmd.Stderr = &ref.stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open ffmpeg stdout: %w", err)
	}
	ref.stdout = stdout
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	return ref, nil
}

func (r *referencePCM) wait() error {
	if r == nil || r.cmd == nil {
		return nil
	}
	if _, err := io.Copy(io.Discard, r.stdout); err != nil {
		return fmt.Errorf("drain ffmpeg PCM: %w%s", err, formatStderr(r.stderr.Bytes()))
	}
	if err := r.cmd.Wait(); err != nil {
		return fmt.Errorf("wait for ffmpeg: %w%s", err, formatStderr(r.stderr.Bytes()))
	}
	r.cmd = nil
	return nil
}

func (r *referencePCM) close() error {
	if r == nil {
		return nil
	}
	if r.stdout != nil {
		_ = r.stdout.Close()
	}
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
		_, _ = io.Copy(io.Discard, r.stdout)
		_ = r.cmd.Wait()
		r.cmd = nil
	}
	return nil
}

func comparePCM(native, ref []byte) (corr, snr float64, maxAbsDiff int, rmsDiff float64) {
	samples := len(native) / 2
	if len(ref)/2 < samples {
		samples = len(ref) / 2
	}
	var sumXY, sumXX, sumYY, sumErr float64
	for i := 0; i < samples; i++ {
		x := float64(int16(binary.LittleEndian.Uint16(native[i*2:])))
		y := float64(int16(binary.LittleEndian.Uint16(ref[i*2:])))
		diff := x - y
		sumXY += x * y
		sumXX += x * x
		sumYY += y * y
		sumErr += diff * diff
		absDiff := int(math.Abs(diff))
		if absDiff > maxAbsDiff {
			maxAbsDiff = absDiff
		}
	}
	if sumXX > 0 && sumYY > 0 {
		corr = sumXY / math.Sqrt(sumXX*sumYY)
	}
	if samples > 0 {
		rmsDiff = math.Sqrt(sumErr / float64(samples))
	}
	if sumErr == 0 {
		snr = math.Inf(1)
	} else if sumYY > 0 {
		snr = 10 * math.Log10(sumYY/sumErr)
	}
	return corr, snr, maxAbsDiff, rmsDiff
}

func analyzePCM(pcm []byte) pcmStats {
	stats := pcmStats{peakDBFS: math.Inf(-1)}
	if len(pcm) == 0 {
		return stats
	}
	peak := 0.0
	for i := 0; i < len(pcm)/2; i++ {
		s := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
		if s == 32767 || s == -32768 {
			stats.clips++
		}
		abs := math.Abs(float64(s)) / 32768.0
		if abs > peak {
			peak = abs
		}
	}
	if peak > 0 {
		stats.peakDBFS = 20 * math.Log10(peak)
	}
	return stats
}

func formatStderr(stderr []byte) string {
	if len(stderr) == 0 {
		return ""
	}
	return fmt.Sprintf(" (ffmpeg stderr: %s)", bytes.TrimSpace(stderr))
}

func sanitizeReportFloat(v float64) float64 {
	switch {
	case math.IsNaN(v):
		return 0
	case math.IsInf(v, 1):
		return 999
	case math.IsInf(v, -1):
		return -999
	default:
		return v
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
