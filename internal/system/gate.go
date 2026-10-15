package system

import (
	"context"
	"errors"
)

// ErrBusy reports the work queue is full and the request should return 429.
var ErrBusy = errors.New("system busy, queue full")

// GateConfig is the per-tier sizing of the upload concurrency gate.
type GateConfig struct {
	SemSize  int // concurrent workers
	QueueLen int // pending waiters before ErrBusy
}

// GateConfigByTier sizes the gate by memory tier: low-memory hosts run one
// conversion at a time with a tiny queue (no OOM pile-up), high-memory hosts
// allow more parallelism with a bounded queue (unbounded queuing would pile
// staged uploads in memory until OOM).
func GateConfigByTier(t MemoryTier) GateConfig {
	switch t {
	case TierTight:
		return GateConfig{SemSize: 1, QueueLen: 2}
	case TierPressured:
		return GateConfig{SemSize: 1, QueueLen: 4}
	default: // TierPlentiful
		return GateConfig{SemSize: gatePlentifulSemSize, QueueLen: gatePlentifulQueueLen}
	}
}

// Upload concurrency gate sizing.
const (
	// gatePlentifulSemSize is the concurrent conversions on high-memory hosts.
	gatePlentifulSemSize = 4
	// gatePlentifulQueueLen bounds the wait queue on high-memory hosts: beyond
	// this the handler returns 429 instead of piling staged uploads in memory.
	gatePlentifulQueueLen = 128
)

// Gate is a counting semaphore with a bounded wait queue.
//
// Acquire takes an execution token immediately when one is free; otherwise it
// reserves a queue seat (non-blocking, ErrBusy when the queue is full) and then
// blocks until a token frees. Release hands the token back and frees one queue
// seat, letting the next queued worker proceed.
//
// It is created once with a fixed tier; the tier is read at construction time
// (probed at startup). A live re-probe mid-run is over-engineering for a Box
// app — the tier only changes when memory moves by gigabytes.
type Gate struct {
	sem   chan struct{}
	queue chan struct{}
}

func NewGate(cfg GateConfig) *Gate {
	g := &Gate{}
	if cfg.SemSize > 0 {
		g.sem = make(chan struct{}, cfg.SemSize)
		for i := 0; i < cfg.SemSize; i++ {
			g.sem <- struct{}{}
		}
	}
	if cfg.QueueLen > 0 {
		g.queue = make(chan struct{}, cfg.QueueLen)
	}
	return g
}

// Acquire reserves a slot, waiting indefinitely for a token once queued.
// Prefer AcquireContext so a disconnected client stops waiting.
func (g *Gate) Acquire() error {
	return g.AcquireContext(context.Background())
}

// AcquireContext reserves a slot. It is non-blocking only until the queue fills:
// an immediately-free token returns at once, a token claimed via the queue
// blocks until a worker releases or ctx is done. Returns ErrBusy when the queue
// is full, or ctx.Err() when the wait is cancelled (the reserved queue seat is
// freed, so cancellation never leaks queue capacity).
func (g *Gate) AcquireContext(ctx context.Context) error {
	// Fast path: an execution token is free right now.
	select {
	case <-g.sem:
		return nil
	default:
	}
	// Tokens busy: reserve a queue seat (non-blocking).
	select {
	case g.queue <- struct{}{}:
	default:
		return ErrBusy
	}
	// Queued; block until a token frees up or the waiter goes away.
	select {
	case <-ctx.Done():
		select {
		case <-g.queue:
		default:
		}
		return ctx.Err()
	case <-g.sem:
		return nil
	}
}

// Release hands the execution token back and frees one queue seat if any
// worker is queued behind.
func (g *Gate) Release() {
	select {
	case <-g.queue:
	default:
	}
	g.sem <- struct{}{}
}

func NewUploadGate(t MemoryTier) *Gate {
	return NewGate(GateConfigByTier(t))
}
