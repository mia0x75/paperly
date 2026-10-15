package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

var errRunFailed = errors.New("maintenance failed")

// fakeCursorRepo is an in-memory MaintainerCursorRepo.
type fakeCursorRepo struct {
	cur      *domain.MaintenanceCursor
	err      error
	upserted []*domain.MaintenanceCursor
}

func (f *fakeCursorRepo) Get(_ context.Context, _ string) (*domain.MaintenanceCursor, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.cur == nil {
		return nil, domain.ErrNotFound
	}
	return f.cur, nil
}

func (f *fakeCursorRepo) Upsert(_ context.Context, c *domain.MaintenanceCursor) error {
	f.upserted = append(f.upserted, c)
	return nil
}

// countingRunner records the number of Run calls.
type countingRunner struct {
	runs int
	err  error
}

func (r *countingRunner) Run(context.Context) error {
	r.runs++
	return r.err
}

// newScheduler assembles a Scheduler: fake runner + mutable clock.
func newScheduler(runner *countingRunner, cursors *fakeCursorRepo, now *time.Time) *Scheduler {
	s := NewScheduler(runner, cursors, discardSlog())
	s.WithClock(func() time.Time { return *now })
	return s
}

func TestSchedulerRunsWhenCursorMissing(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	runner := &countingRunner{}
	cursors := &fakeCursorRepo{}
	s := newScheduler(runner, cursors, &now)

	s.runIfDue(context.Background())
	require.Equal(t, 1, runner.runs, "首次（无游标）应立即执行")
	require.Len(t, cursors.upserted, 1)
	require.Equal(t, CursorTaskDailyMaintenance, cursors.upserted[0].TaskName)
	require.Equal(t, domain.CursorStatusCompleted, cursors.upserted[0].LastStatus)
	require.NotNil(t, cursors.upserted[0].LastRunAt)
	require.Equal(t, now, *cursors.upserted[0].LastRunAt)
}

func TestSchedulerSkipsWhenRecent(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	oneHourAgo := now.Add(-time.Hour)
	runner := &countingRunner{}
	cursors := &fakeCursorRepo{cur: &domain.MaintenanceCursor{
		TaskName: CursorTaskDailyMaintenance, LastRunAt: &oneHourAgo,
		LastStatus: domain.CursorStatusCompleted,
	}}
	s := newScheduler(runner, cursors, &now)

	s.runIfDue(context.Background())
	require.Equal(t, 0, runner.runs, "1 小时前刚跑过，不应再跑")
	require.Empty(t, cursors.upserted)
}

func TestSchedulerRunsWhenOverdue(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	overdue := now.Add(-25 * time.Hour)
	runner := &countingRunner{}
	cursors := &fakeCursorRepo{cur: &domain.MaintenanceCursor{
		TaskName: CursorTaskDailyMaintenance, LastRunAt: &overdue,
		LastStatus: domain.CursorStatusCompleted,
	}}
	s := newScheduler(runner, cursors, &now)

	s.runIfDue(context.Background())
	require.Equal(t, 1, runner.runs, "超过 24h 视为逾期")
	require.Len(t, cursors.upserted, 1)
}

func TestSchedulerMarksFailedWhenRunFails(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	runner := &countingRunner{err: errRunFailed}
	cursors := &fakeCursorRepo{}
	s := newScheduler(runner, cursors, &now)

	s.runIfDue(context.Background())
	require.Equal(t, 1, runner.runs)
	require.Len(t, cursors.upserted, 1)
	require.Equal(t, domain.CursorStatusFailed, cursors.upserted[0].LastStatus,
		"失败要回写 Failed，给体检页一个可见的失败信号")
}

// TestSchedulerCursorReadErrorStopsRun: on a cursor read failure, abandon this
// scheduling attempt (better to miss a run than to duplicate one).
func TestSchedulerCursorReadErrorStopsRun(t *testing.T) {
	runner := &countingRunner{}
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	cursors := &fakeCursorRepo{err: errors.New("read db failed")}
	s := newScheduler(runner, cursors, &now)

	s.runIfDue(context.Background())
	require.Equal(t, 0, runner.runs)
}

// TestSchedulerRunLoopStartsImmediately: Run runs once immediately on start
// (without waiting for the first tick), then rechecks at the interval; with a
// fixed clock the second recheck is necessarily not due, and it is canceled after
// finishing.
func TestSchedulerRunLoopStartsImmediately(t *testing.T) {
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	runner := &countingRunner{}
	cursors := &fakeCursorRepo{}
	s := newScheduler(runner, cursors, &now)
	s.WithInterval(10 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool { return runner.runs >= 1 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done

	require.Equal(t, 1, runner.runs, "启动即跑一次 + 固定时钟下当天 due 为 false，总执行 1 次")
}
