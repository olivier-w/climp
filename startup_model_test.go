package main

import (
	"testing"

	"github.com/olivier-w/climp/internal/downloader"
	"github.com/olivier-w/climp/internal/ui"
)

func TestStartupModelSelectionEntersOpeningPhase(t *testing.T) {
	model, cmd := newStartupModel().Update(ui.BrowserSelectedMsg{Path: "song.mp3"})
	if cmd == nil {
		t.Fatal("expected opening command")
	}

	startup, ok := model.(startupModel)
	if !ok {
		t.Fatalf("expected startupModel, got %T", model)
	}
	if startup.phase != phaseOpening {
		t.Fatalf("expected phaseOpening, got %v", startup.phase)
	}
	if startup.statusCh == nil {
		t.Fatal("expected status channel to be initialized")
	}
}

func TestStartupModelErrorReturnsToBrowsePhase(t *testing.T) {
	m := newStartupModel()
	m.phase = phaseOpening

	model, cmd := m.Update(startupResolvedMsg{err: errBoom{}})
	if cmd != nil {
		t.Fatal("expected no command on error return")
	}

	startup := model.(startupModel)
	if startup.phase != phaseBrowse {
		t.Fatalf("expected phaseBrowse, got %v", startup.phase)
	}
	if startup.errMsg == "" {
		t.Fatal("expected error message")
	}
}

func TestStartupModelConsumesStatusUpdates(t *testing.T) {
	m := newStartupModel()
	m.phase = phaseOpening
	m.statusCh = make(chan downloader.DownloadStatus)

	model, cmd := m.Update(startupDownloadStatusMsg(downloader.DownloadStatus{
		Phase:   "downloading",
		Percent: 0.5,
	}))
	if cmd == nil {
		t.Fatal("expected waitForStatus command")
	}

	startup := model.(startupModel)
	if !startup.hasStatus {
		t.Fatal("expected hasStatus to be true")
	}
	if startup.status.Phase != "downloading" || startup.status.Percent != 0.5 {
		t.Fatalf("unexpected status: %+v", startup.status)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
