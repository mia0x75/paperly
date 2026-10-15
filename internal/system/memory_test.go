package system

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClassifyMemoryTiers(t *testing.T) {
	cases := []struct {
		name  string
		avail uint64
		want  MemoryTier
	}{
		{"tight just below 512MB", 511 << 20, TierTight},
		{"pressured at 512MB", 512 << 20, TierPressured},
		{"pressured mid", 1 << 30, TierPressured},
		{"plentiful at 2GB", 2 << 30, TierPlentiful},
		{"plentiful large", 16 << 30, TierPlentiful},
		{"zero", 0, TierTight},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.avail); got != tc.want {
				t.Fatalf("classify(%d) = %v, want %v", tc.avail, got, tc.want)
			}
		})
	}
}

func TestGateAcquireRelease(t *testing.T) {
	g := NewGate(GateConfig{SemSize: 1, QueueLen: 4})
	// First acquire takes the free token immediately.
	if err := g.Acquire(); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// Second acquire has no free token; it reserves a queue seat and blocks.
	// Run it in a goroutine: it must complete only after Release.
	done := make(chan error, 1)
	go func() {
		done <- g.Acquire()
	}()
	select {
	case err := <-done:
		t.Fatalf("queued acquire completed before release: %v", err)
	case <-time.After(50 * time.Millisecond):
		// Expected: still blocked.
	}
	g.Release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("queued acquire after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued acquire did not complete after release")
	}
	g.Release()
}

func TestGateQueueFullReturnsBusy(t *testing.T) {
	// Deterministic busy check: hold the only token, fill the only queue seat
	// with a blocked waiter, then a new acquire must return ErrBusy immediately.
	g := NewGate(GateConfig{SemSize: 1, QueueLen: 1})
	if err := g.Acquire(); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	queued := make(chan error, 1)
	go func() {
		queued <- g.Acquire()
	}()
	select {
	case <-time.After(50 * time.Millisecond):
		// Expected: the goroutine is queued and blocked.
	case err := <-queued:
		t.Fatalf("queued acquire completed unexpectedly: %v", err)
	}
	if err := g.Acquire(); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy when queue full, got %v", err)
	}
	// Drain: release so the queued worker gets the token, then it releases itself.
	g.Release()
	select {
	case err := <-queued:
		if err != nil {
			t.Fatalf("queued acquire after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued acquire did not complete")
	}
	g.Release()
}

func TestGateConfigByTier(t *testing.T) {
	if c := GateConfigByTier(TierTight); c.SemSize != 1 || c.QueueLen != 2 {
		t.Fatalf("tight config = %+v, want {1,2}", c)
	}
	if c := GateConfigByTier(TierPressured); c.SemSize != 1 || c.QueueLen != 4 {
		t.Fatalf("pressured config = %+v, want {1,4}", c)
	}
	if c := GateConfigByTier(TierPlentiful); c.SemSize != 4 || c.QueueLen == 0 {
		t.Fatalf("plentiful config = %+v, want bounded queue", c)
	}
}

// TestTierFallbackOnProbeFailure asserts that a failing probe maps to the
// conservative pressured tier instead of unlimited.
func TestTierFallbackOnProbeFailure(t *testing.T) {
	old := memoryProbe
	memoryProbe = func() uint64 { return 0 }
	defer func() { memoryProbe = old }()
	if got := classify(AvailableMemory()); got != TierPressured {
		t.Fatalf("fallback AvailableMemory tier = %v, want TierPressured", got)
	}
}

// TestGateAcquireContextCancel proves a queued waiter stops waiting when its
// context is cancelled, and the reserved queue seat is freed (a second waiter
// can still queue afterwards — no capacity leak).
func TestGateAcquireContextCancel(t *testing.T) {
	g := NewGate(GateConfig{SemSize: 1, QueueLen: 1})
	if err := g.Acquire(); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- g.AcquireContext(ctx)
	}()
	select {
	case <-time.After(50 * time.Millisecond):
		// Expected: queued and blocked.
	case err := <-done:
		t.Fatalf("queued acquire completed unexpectedly: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled acquire = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled acquire did not return")
	}
	// The freed seat is reusable: another waiter queues fine.
	queued := make(chan error, 1)
	go func() {
		queued <- g.AcquireContext(context.Background())
	}()
	select {
	case <-time.After(50 * time.Millisecond):
		// Expected: queued and blocked.
	case err := <-queued:
		t.Fatalf("re-queued acquire completed unexpectedly: %v", err)
	}
	g.Release()
	select {
	case err := <-queued:
		if err != nil {
			t.Fatalf("re-queued acquire after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("re-queued acquire did not complete")
	}
	g.Release()
}
