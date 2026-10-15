package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// One queue drained in one go—backup done, gaps/unknown kinds skipped, then one
// more idle poll after everything is processed, exiting after Cancel.
func TestRunDrainsQueueAndTerminates(t *testing.T) {
	store := &fakeTaskStore{queue: []*domain.Task{
		{ID: 1, Kind: domain.TaskKindBackup, Detail: "{}"},
		{ID: 2, Kind: domain.TaskKindAggregateGaps, Detail: "{}"},
		{ID: 3, Kind: 99, Detail: "{}"},
	}}
	backup := &fakeBackup{}
	notifier := &fakeNotifier{}
	deps := testDeps()
	deps.Tasks = store
	deps.Backup = backup
	deps.Notify = notifier
	deps.PollInterval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Run(ctx, deps)
		close(done)
	}()

	require.Eventually(t, func() bool {
		return len(store.finished) == 1 && len(store.skipped) == 2
	}, 2*time.Second, 5*time.Millisecond, "三条任务应分别完成/跳过")

	require.Equal(t, []int64{1}, store.finished, "backup 执行成功置 done")
	require.Equal(t, skipAggregateGapsReason, store.skipped[2], "gaps 占位跳过")
	require.Equal(t, skipUnknownReason, store.skipped[3], "未知 kind 跳过")
	require.Equal(t, 1, backup.calls, "backup handler 恰好跑一次")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run 未在 cancel 后退出")
	}
}

// Backup failure → Fail(retryable=true); path out-of-bounds (non-retryable
// signal) → Fail(retryable=false).
func TestRunFailsTaskWithRetryableFlag(t *testing.T) {
	store := &fakeTaskStore{queue: []*domain.Task{
		{ID: 5, Kind: domain.TaskKindBackup, Detail: "{}"},
		{ID: 6, Kind: domain.TaskKindCleanupStorage, Detail: `{"dir":"../../etc/passwd"}`},
	}}
	backup := &fakeBackup{err: errors.New("disk full")}
	deps := testDeps()
	deps.Tasks = store
	deps.Backup = backup
	deps.Notify = &fakeNotifier{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, deps)

	require.Eventually(t, func() bool { return len(store.failed) == 2 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, failCall{lastError: "disk full", retryable: true}, store.failed[5], "备份运行时错误可重试")
	require.Equal(t, failCall{lastError: "cleanup directory out of bounds (must be under assets/exams/): ../../etc/passwd", retryable: false}, store.failed[6], "路径越界不可重试")
}

// With an empty queue and a long sleep interval, cancel should exit immediately
// without idling.
func TestRunExitsOnCancelWhileIdle(t *testing.T) {
	deps := testDeps()
	deps.Tasks = &fakeTaskStore{}
	deps.PollInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Run(ctx, deps)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("空队列睡大间隔时 cancel 未生效")
	}
}

// A Claim returning a non-ErrNotFound error is only logged; polling is not cut
// off.
func TestRunContinuesOnClaimError(t *testing.T) {
	store := &errClaimStore{firstClaim: make(chan struct{})}
	deps := testDeps()
	deps.Tasks = store
	deps.PollInterval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Run(ctx, deps)
		close(done)
	}()
	select {
	case <-store.firstClaim:
	case <-time.After(2 * time.Second):
		t.Fatal("Worker 未开始领任务")
	}
	cancel()
	<-done
}

// Errors on the first two Claims, then returns ErrNotFound (validates loop
// robustness).
type errClaimStore struct {
	fakeTaskStore
	firstClaim chan struct{}
	n          int
}

func (e *errClaimStore) Claim(ctx context.Context) (*domain.Task, error) {
	if e.n == 0 {
		close(e.firstClaim)
	}
	if e.n < 2 {
		e.n++
		return nil, errors.New("db temporarily unavailable")
	}
	return e.fakeTaskStore.Claim(ctx)
}

// Worker TaskStore that returns ErrNotFound while empty (so the loop enters the
// idle wait) and pops a task once one is pushed. The buffered idle channel lets a
// test wait until the worker is genuinely blocked in waitIdle.
type wakeStore struct {
	mu       sync.Mutex
	queue    []*domain.Task
	finished []int64
	idle     chan struct{}
}

func newWakeStore() *wakeStore {
	return &wakeStore{idle: make(chan struct{}, 1)}
}

func (s *wakeStore) Claim(context.Context) (*domain.Task, error) {
	s.mu.Lock()
	if len(s.queue) == 0 {
		s.mu.Unlock()
		select {
		case s.idle <- struct{}{}:
		default:
		}
		return nil, domain.ErrNotFound
	}
	t := s.queue[0]
	s.queue = s.queue[1:]
	s.mu.Unlock()
	return t, nil
}

func (s *wakeStore) Finish(_ context.Context, id int64) error {
	s.mu.Lock()
	s.finished = append(s.finished, id)
	s.mu.Unlock()
	return nil
}

func (s *wakeStore) Skip(context.Context, int64, string) error       { return nil }
func (s *wakeStore) Fail(context.Context, int64, string, bool) error { return nil }

func (s *wakeStore) push(t *domain.Task) {
	s.mu.Lock()
	s.queue = append(s.queue, t)
	s.mu.Unlock()
}

func (s *wakeStore) doneCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.finished)
}

// An enqueue + Notify must wake the idle Worker well before the fallback interval
// (set to 1h, so the timer can never be the trigger).
func TestRunWakesOnNotify(t *testing.T) {
	store := newWakeStore()
	wake := NewWakeSignal()
	deps := testDeps()
	deps.Tasks = store
	deps.Backup = &fakeBackup{}
	deps.Notify = &fakeNotifier{}
	deps.Wake = wake.Wake()
	deps.PollInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, deps)

	select {
	case <-store.idle:
	case <-time.After(2 * time.Second):
		t.Fatal("Worker 未进入空闲等待")
	}

	start := time.Now()
	store.push(&domain.Task{ID: 1, Kind: domain.TaskKindBackup, Detail: "{}"})
	wake.Notify()

	require.Eventually(t, func() bool { return store.doneCount() == 1 }, 2*time.Second, time.Millisecond)
	require.Less(t, time.Since(start), 100*time.Millisecond, "入队唤醒应 < 100ms")
}

// Always reports an empty queue and counts Claim calls.
type countingClaimStore struct{ n int32 }

func (s *countingClaimStore) Claim(context.Context) (*domain.Task, error) {
	atomic.AddInt32(&s.n, 1)
	return nil, domain.ErrNotFound
}
func (s *countingClaimStore) Finish(context.Context, int64) error             { return nil }
func (s *countingClaimStore) Skip(context.Context, int64, string) error       { return nil }
func (s *countingClaimStore) Fail(context.Context, int64, string, bool) error { return nil }
func (s *countingClaimStore) claims() int32                                   { return atomic.LoadInt32(&s.n) }

// With a long fallback interval and no signals, the idle loop must not busy-poll.
func TestRunIdleDoesNotSpin(t *testing.T) {
	store := &countingClaimStore{}
	deps := testDeps()
	deps.Tasks = store
	deps.PollInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, deps)

	time.Sleep(50 * time.Millisecond)
	require.LessOrEqual(t, int(store.claims()), 2, "空队列不应空转")
}

// Without a wake signal the fallback timer must still pick up a task enqueued
// during the idle wait.
func TestRunFallbackPollPicksUpWithoutSignal(t *testing.T) {
	store := newWakeStore()
	deps := testDeps()
	deps.Tasks = store
	deps.Backup = &fakeBackup{}
	deps.Notify = &fakeNotifier{}
	deps.PollInterval = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Run(ctx, deps)

	select {
	case <-store.idle:
	case <-time.After(2 * time.Second):
		t.Fatal("Worker 未进入空闲等待")
	}
	store.push(&domain.Task{ID: 7, Kind: domain.TaskKindBackup, Detail: "{}"})
	// No Notify — only the fallback timer can wake the loop.
	require.Eventually(t, func() bool { return store.doneCount() == 1 }, 2*time.Second, 5*time.Millisecond)
}
