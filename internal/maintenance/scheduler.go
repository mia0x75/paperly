package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"paperly/internal/domain"
)

// CursorTaskDailyMaintenance is the cursor task name of daily maintenance in the
// cursors table.
//
// Scheduling only looks at its last_run_at: no record, or at least overdueAfter
// since, counts as "should run once".
const CursorTaskDailyMaintenance = "daily_maintenance"

// dailyMaintenanceInterval is the scheduling recheck interval.
// The SLA is "< 5 minutes", but neither the 9/1 enqueue nor the metric cleanup
// needs minute-level punctuality; a 1h recheck is enough for the Box scenario and
// avoids unnecessary whole-machine wakeups.
const dailyMaintenanceInterval = time.Hour

// overdueAfter is the threshold for "overdue if longer than this since the last
// run". A daily task uses 24h.
const overdueAfter = 24 * time.Hour

// MaintainerCursorRepo is the cursor read/write interface the scheduler needs (a
// subset of domain.MaintenanceCursorRepository).
type MaintainerCursorRepo interface {
	// Get fetches a cursor; returns domain.ErrNotFound if absent.
	Get(ctx context.Context, taskName string) (*domain.MaintenanceCursor, error)
	// Upsert inserts or updates by task_name.
	Upsert(ctx context.Context, c *domain.MaintenanceCursor) error
}

// Runner is the execution entry point of daily maintenance (*Service satisfies
// this interface; tests may inject a fake).
type Runner interface {
	// Run performs one full maintenance pass.
	Run(ctx context.Context) error
}

// Scheduler schedules daily maintenance with "check on start + recheck hourly"
// (for the trigger mechanism see the package comment).
//
// The overdueAfter gate guarantees at most one run per day, preventing duplicate
// triggers from doubling gauges snapshots/cleanup (Run's metric part is
// idempotent, but the 9/1 enqueue action is not, so the gate covers it as well).
type Scheduler struct {
	svc      Runner
	cursors  MaintainerCursorRepo
	interval time.Duration
	clock    func() time.Time
	log      *slog.Logger
}

// NewScheduler constructs the scheduler; uses slog.Default() when log is nil.
func NewScheduler(svc Runner, cursors MaintainerCursorRepo, log *slog.Logger) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{
		svc:      svc,
		cursors:  cursors,
		interval: dailyMaintenanceInterval,
		clock:    time.Now,
		log:      log,
	}
}

// WithClock injects a clock (for tests).
func (s *Scheduler) WithClock(now func() time.Time) *Scheduler {
	s.clock = now
	return s
}

// WithInterval injects the recheck interval (for tests).
func (s *Scheduler) WithInterval(d time.Duration) *Scheduler {
	s.interval = d
	return s
}

// Run checks once on start, then rechecks by interval; returns when ctx is
// canceled (graceful exit).
func (s *Scheduler) Run(ctx context.Context) {
	s.runIfDue(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runIfDue(ctx)
		}
	}
}

// runIfDue determines whether it is time to run; if so, runs one maintenance
// pass and writes back the cursor.
func (s *Scheduler) runIfDue(ctx context.Context) {
	due, err := s.due(ctx)
	if err != nil {
		s.log.Error("failed to read maintenance cursor", "cursor", CursorTaskDailyMaintenance, "err", err)
		return
	}
	if !due {
		return
	}

	status := domain.CursorStatusCompleted
	if err := s.svc.Run(ctx); err != nil {
		s.log.Error("daily maintenance failed", "err", err)
		status = domain.CursorStatusFailed
	}
	if err := s.markDone(ctx, status); err != nil {
		s.log.Warn("failed to write back maintenance cursor", "status", status, "err", err)
	}
}

// due decides whether it should run: no cursor record (first time) or more than
// overdueAfter since the last run.
func (s *Scheduler) due(ctx context.Context) (bool, error) {
	cur, err := s.cursors.Get(ctx, CursorTaskDailyMaintenance)
	if errors.Is(err, domain.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if cur.LastRunAt == nil {
		return true, nil
	}
	return s.clock().Sub(*cur.LastRunAt) >= overdueAfter, nil
}

// markDone writes back the cursor (last_run_at=now, last_status) for the next due check.
func (s *Scheduler) markDone(ctx context.Context, status int) error {
	now := s.clock()
	return s.cursors.Upsert(ctx, &domain.MaintenanceCursor{
		TaskName:   CursorTaskDailyMaintenance,
		LastRunAt:  &now,
		LastStatus: status,
	})
}
