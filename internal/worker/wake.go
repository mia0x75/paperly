package worker

// WakeSignal delivers "a task was just enqueued" wakeups to the Worker loop.
//
// Notify is non-blocking: an already-pending signal is dropped rather than blocking the enqueue
// path. Dropping is safe—the Worker drains the queue until empty, and the fallback poll interval
// covers any missed wakeup.
//
// Wiring: main creates one WakeSignal, injects it into the task repository (whose Insert notifies)
// and passes Wake() into worker.Deps.
type WakeSignal struct {
	ch chan struct{}
}

// NewWakeSignal returns a signal backed by a capacity-1 buffered channel.
func NewWakeSignal() *WakeSignal {
	return &WakeSignal{ch: make(chan struct{}, 1)}
}

// Notify sends a wakeup without blocking the enqueue path.
func (s *WakeSignal) Notify() {
	select {
	case s.ch <- struct{}{}:
	default:
	}
}

func (s *WakeSignal) Wake() <-chan struct{} {
	return s.ch
}
