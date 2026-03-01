package player

import "testing"

func TestPlayerCloseRunsCleanupOnce(t *testing.T) {
	calls := 0
	p := &Player{
		stopMon: make(chan struct{}),
		cleanup: func() {
			calls++
		},
	}

	p.Close()
	p.Close()

	if calls != 1 {
		t.Fatalf("expected cleanup to run once, got %d", calls)
	}
}
