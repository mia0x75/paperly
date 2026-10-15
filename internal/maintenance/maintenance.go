// Package maintenance implements the daily maintenance task.
//
// Current scope: write 4 volume gauge snapshots + clean expired metrics by
// is_gauge tier + refresh query-planner statistics (PRAGMA optimize).
//
// The trigger mechanism is the application layer's responsibility (startup /
// timer / external cron all work); this package only provides an idempotent Run:
// repeated runs do not double the volume (gauge is overwritten), nor
// double-count an unfinished cleanup.
package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"paperly/internal/domain"
	"paperly/internal/health"
	"paperly/internal/metrics"
)

// Repo is the metrics read/write interface that maintenance needs (a subset of
// domain.SystemMetricsRepository).
type Repo interface {
	metrics.Repo
	// DeleteOlderThan cleans counter rows whose metric_date is earlier than
	// cutoff, returning the deleted row count.
	DeleteOlderThan(ctx context.Context, cutoff string) (int64, error)
	// DeleteGaugeOlderThan cleans gauge rows whose metric_date is earlier than
	// cutoff, returning the deleted row count.
	DeleteGaugeOlderThan(ctx context.Context, cutoff string) (int64, error)
}

// DBLocator is the narrow interface returning the main DB file size (a subset of
// domain.DatabaseStatsRepository).
type DBLocator interface {
	// DBSize returns the main DB file size in bytes.
	DBSize(ctx context.Context) (int64, error)
}

// TaskStore is the task queue interface that maintenance needs (a subset of
// domain.TaskRepository).
type TaskStore interface {
	// ClearOlderThan cleans tasks finished (done/failed/skipped) more than days ago.
	ClearOlderThan(ctx context.Context, days int) (int64, error)
	// Insert enqueues one pending task and returns its autoincrement id.
	Insert(ctx context.Context, t *domain.Task) (int64, error)
}

// KidRoster is the active-student listing interface that maintenance needs (a
// subset of domain.KidRepository).
type KidRoster interface {
	// List returns students by filter (grade advance only uses status=active).
	List(ctx context.Context, filter domain.ListKidsFilter) ([]*domain.Kid, error)
}

// Optimizer is the DB-level statistics-refresh interface that maintenance needs
// (a subset of domain.OpsRepository).
type Optimizer interface {
	// Optimize runs PRAGMA optimize (SQLite's lightweight stats refresh) so the
	// query planner keeps good plans as the data grows.
	Optimize(ctx context.Context) error
}

// Daily maintenance task-housekeeping parameters.
const (
	// maintainTaskRetentionDays is the retention days for completed tasks in
	// tasks (clean those 30 days old).
	maintainTaskRetentionDays = 30
	// taskEnqueuePriority is the enqueue priority of the grade advance task.
	taskEnqueuePriority = 80
	// backupEnqueuePriority is the enqueue priority of the daily backup task
	// (same tier as grade advance).
	backupEnqueuePriority = 80
)

type Service struct {
	repo      Repo
	db        DBLocator
	tasks     TaskStore
	kids      KidRoster
	optimizer Optimizer
	dataDir   string
	clock     func() time.Time
	log       *slog.Logger
}

// NewService constructs the maintenance service; uses slog.Default() when log is nil.
func NewService(repo Repo, db DBLocator, tasks TaskStore, kids KidRoster, optimizer Optimizer, dataDir string, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, db: db, tasks: tasks, kids: kids, optimizer: optimizer, dataDir: dataDir, clock: time.Now, log: log}
}

// WithClock injects a clock (for tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.clock = now
	return s
}

// gaugeSnapshot is one volume snapshot to be written.
type gaugeSnapshot struct {
	name  string
	value int64
	// skip true means this collection could not get a number (e.g. the platform
	// does not support statfs), so no row is written.
	skip bool
}

// Run performs one maintenance pass.
//
// Order: write snapshots first, then clean. The other way around, the
// just-written same-day row would not be deleted by mistake (a same-day row's
// date is never earlier than cutoff), but a cleanup failure would also prevent
// the snapshots from landing—and snapshots are the input to the health page and
// trend charts, so they deserve priority over cleanup. Task housekeeping (tasks
// 30-day cleanup + 9/1 enqueue) comes last: its failure cost (queue kept a few
// more days / one missed enqueue that day) is smaller than missing metric
// snapshots. The PRAGMA optimize stats refresh runs after all writes, fail-soft.
func (s *Service) Run(ctx context.Context) error {
	for _, g := range s.collect(ctx) {
		if g.skip {
			continue
		}
		// fail-soft: a single snapshot failure should not fail the whole
		// maintenance pass (metric-write failure does not block the main flow).
		if err := s.repo.SetGauge(ctx, g.name, g.value); err != nil {
			s.log.Warn("size snapshot metric failed", "metric", g.name, "err", err)
		}
	}

	if err := s.cleanup(ctx); err != nil {
		s.incOK(ctx, metrics.MetricMaintenanceFail)
		return err
	}
	if err := s.taskHousekeeping(ctx); err != nil {
		s.incOK(ctx, metrics.MetricMaintenanceFail)
		return err
	}
	s.optimize(ctx)
	s.incOK(ctx, metrics.MetricMaintenanceSuccess)
	return nil
}

// taskHousekeeping is the daily housekeeping of the tasks queue: clean tasks
// finished 30 days ago + 9/1 enrollment advance enqueue.
func (s *Service) taskHousekeeping(ctx context.Context) error {
	n, err := s.tasks.ClearOlderThan(ctx, maintainTaskRetentionDays)
	if err != nil {
		return fmt.Errorf("clean expired tasks: %w", err)
	}
	if n > 0 {
		s.log.Info("expired tasks cleaned", "rows", n, "keep_days", maintainTaskRetentionDays)
	}
	if err := s.enqueueGradeAdvance(ctx); err != nil {
		return err
	}
	return s.enqueueBackup(ctx)
}

// enqueueBackup enqueues the daily backup task (kind=3).
//
// Runs every day: the scheduler guarantees at most one maintenance pass per day,
// so one backup task is enqueued daily. Deduplication is deliberately skipped —
// the backup handler is idempotent and, in the Box single-family scenario, a
// duplicate enqueue is harmless; adding dedup state here would only create
// another ledger that can drift (same reasoning as enqueueGradeAdvance).
func (s *Service) enqueueBackup(ctx context.Context) error {
	if _, err := s.tasks.Insert(ctx, &domain.Task{
		Kind:     domain.TaskKindBackup,
		Priority: backupEnqueuePriority,
		Status:   domain.TaskStatusPending,
		Detail:   "{}",
	}); err != nil {
		return fmt.Errorf("enqueue backup task: %w", err)
	}
	return nil
}

// enqueueGradeAdvance enqueues grade advance tasks for all active students on
// September 1 each year.
//
// Children with grade=12 are enqueued too: when raw_target crosses 12, the
// consumer sets graduation (status=2). Enqueueing deliberately does not
// deduplicate—duplicate consumption is caught by the idempotency gate
// (kids.grade == enrolled_grade), and introducing dedup state here would only
// add another ledger that can drift.
// It returns directly on non-September-1 days: the scheduler runs once daily by
// cursor, and a miss waits for next year.
func (s *Service) enqueueGradeAdvance(ctx context.Context) error {
	now := s.clock()
	if now.Month() != time.September || now.Day() != 1 {
		return nil
	}
	kids, err := s.kids.List(ctx, domain.ListKidsFilter{Status: domain.KidStatusActive})
	if err != nil {
		return fmt.Errorf("list active students: %w", err)
	}
	year := domain.CurrentSchoolYear(now)
	enqueued := 0
	for _, k := range kids {
		detail := fmt.Sprintf(`{"kid_id":%d,"enrolled_year":%d,"enrolled_grade":%d,"target_grade":%d}`,
			k.ID, year, k.Grade, k.Grade+1)
		if _, err := s.tasks.Insert(ctx, &domain.Task{
			Kind:     domain.TaskKindAdvanceGrade,
			Priority: taskEnqueuePriority,
			Status:   domain.TaskStatusPending,
			Detail:   detail,
		}); err != nil {
			// A single child's enqueue failure does not block the rest; log it and
			// remediate next round.
			s.log.Warn("failed to enqueue grade-advance task", "kid_id", k.ID, "err", err)
			continue
		}
		enqueued++
	}
	if enqueued > 0 {
		s.log.Info("grade-advance tasks enqueued", "enqueued", enqueued, "school_year", year)
	}
	return nil
}

// collect collects 4 volume gauges.
//
// Definitions:
//   - db_size_kb: counts only the main DB file, **excluding -wal / -shm**. The WAL
//     size fluctuates sharply with checkpoint timing and would plot as a jagged
//     noise line in a trend chart; this stays consistent with the health page's
//     existing definition.
//   - assets_size_kb / fs_*_kb: reuse health.MeasureStorage (which already
//     includes directory traversal and per-platform statfs), avoiding a second
//     implementation.
func (s *Service) collect(ctx context.Context) []gaugeSnapshot {
	shots := make([]gaugeSnapshot, 0, 4)

	if n, err := s.db.DBSize(ctx); err != nil {
		s.log.Warn("failed to read main DB size, skipping snapshot", "err", err)
		shots = append(shots, gaugeSnapshot{name: metrics.MetricDBSizeKB, skip: true})
	} else {
		shots = append(shots, gaugeSnapshot{name: metrics.MetricDBSizeKB, value: bytesToKB(n)})
	}

	usage := health.MeasureStorage(s.dataDir)
	shots = append(shots, gaugeSnapshot{
		name:  metrics.MetricAssetsSizeKB,
		value: bytesToKB(usage.AssetsBytes),
	})
	if !usage.DiskSupported {
		// No statfs on Windows: writing no row is better than writing 0—a gap in
		// the trend chart immediately shows "not collected", whereas a line hugging
		// 0 would be read as "disk full".
		s.log.Info("disk capacity probe not supported on this platform, skipping fs snapshot", "data_dir", s.dataDir)
		shots = append(shots,
			gaugeSnapshot{name: metrics.MetricFSFreeKB, skip: true},
			gaugeSnapshot{name: metrics.MetricFSTotalKB, skip: true},
		)
		return shots
	}
	shots = append(shots,
		gaugeSnapshot{name: metrics.MetricFSFreeKB, value: uintBytesToKB(usage.DiskFree)},
		gaugeSnapshot{name: metrics.MetricFSTotalKB, value: uintBytesToKB(usage.DiskTotal)},
	)
	return shots
}

// cleanup cleans expired metric rows by is_gauge tier.
//
// cutoff is computed on the Go side in the local timezone and then passed to
// SQL, rather than relying on date('now','-90 day'): that form depends on
// SQLite's localtime support and scatters the definition from the write side
// into two places, making alignment harder.
func (s *Service) cleanup(ctx context.Context) error {
	now := s.clock()

	n, err := s.repo.DeleteOlderThan(ctx, cutoff(now, domain.MetricRetentionDays))
	if err != nil {
		return fmt.Errorf("clean expired counter metrics: %w", err)
	}
	if n > 0 {
		s.log.Info("expired counter metrics cleaned", "rows", n, "keep_days", domain.MetricRetentionDays)
	}

	g, err := s.repo.DeleteGaugeOlderThan(ctx, cutoff(now, domain.MetricGaugeRetentionDays))
	if err != nil {
		return fmt.Errorf("clean expired gauge metrics: %w", err)
	}
	if g > 0 {
		s.log.Info("expired gauge metrics cleaned", "rows", g, "keep_days", domain.MetricGaugeRetentionDays)
	}
	return nil
}

// incOK records a counter, only logging on failure (recording the maintenance
// result should not, in turn, fail maintenance).
func (s *Service) incOK(ctx context.Context, name string) {
	if err := s.repo.Inc(ctx, name); err != nil {
		s.log.Warn("maintenance result metric failed", "metric", name, "err", err)
	}
}

// optimize refreshes the query planner's statistics (PRAGMA optimize) after the
// pass's writes. Fail-soft: a stats refresh is an optimization, not a
// correctness step, so its failure only logs.
func (s *Service) optimize(ctx context.Context) {
	if err := s.optimizer.Optimize(ctx); err != nil {
		s.log.Warn("pragma optimize failed", "err", err)
	}
}

// cutoff returns the retention cutoff date (YYYY-MM-DD, local timezone).
func cutoff(now time.Time, keepDays int) string {
	return now.AddDate(0, 0, -keepDays).Format("2006-01-02")
}

// bytesToKB converts bytes to KB (integer division, rounded down).
func bytesToKB(n int64) int64 {
	if n <= 0 {
		return 0
	}
	return n / 1024
}

// uintBytesToKB converts unsigned bytes to KB (guards against extreme-value overflow).
func uintBytesToKB(n uint64) int64 {
	const maxKB = int64(^uint64(0)>>1) / 1024 // max int64
	kb := int64(n / 1024)
	if kb > maxKB {
		return maxKB
	}
	if kb < 0 {
		return 0
	}
	return kb
}
