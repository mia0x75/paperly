// Package worker is the background consumer of the tasks queue.
//
// Responsibilities: poll the head pending task → dispatch by kind → push it to a terminal state
// (done / skipped / failed). A single Worker runs serially, consuming one task at a time so the
// same task can never be claimed twice under concurrency; the single-connection write pool is a
// second safeguard.
//
// There is no cron: main runs it as a resident goroutine, polling by pollInterval when the queue is
// empty and claiming the next task immediately after finishing one, without sleeping. On ctx
// cancellation (graceful exit) the running task finishes before the loop returns.
//
// Dependencies go through the narrow interfaces below, assembled and injected by main; the package
// does not import repository/service, keeping it independently testable.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"path"
	"strings"
	"time"

	"paperly/internal/domain"
)

// pollInterval is the idle polling interval when the queue is empty; after finishing a task the
// loop claims the next immediately instead of waiting here.
const pollInterval = 30 * time.Second

// skipUnknownReason is the skip reason for an unknown kind, written to last_error to help trace the
// enqueue source.
const skipUnknownReason = "unknown kind"

// skipAggregateGapsReason marks the placeholder handler: the domain layer for these kinds has not
// landed, so they must never linger in pending.
const skipAggregateGapsReason = "domain layer not implemented (levels/gaps/questions repos and algorithms)"

// Deps holds all external dependencies of the Worker, assembled by main.
type Deps struct {
	// Log is the structured logger; uses slog.Default() when nil.
	Log *slog.Logger
	// Clock returns the current time; uses time.Now when nil. Handlers use it for the school year.
	Clock func() time.Time
	// DataDir is the data root; cleanup_storage resolves relative directories under it.
	DataDir string
	// PollInterval is the idle poll interval; defaults to 30s when <=0. Tests inject a small value
	// to speed up empty-queue polling; production leaves it zero.
	PollInterval time.Duration
	// Wake signals "task enqueued": while idle, receiving on it ends the wait immediately instead of
	// sleeping the full PollInterval. nil disables it (only the fallback timer runs).
	Wake <-chan struct{}
	// Tasks is the task queue read/write surface.
	Tasks TaskStore
	// Kids is the student profile surface (grade-advance idempotency gate + graduation).
	Kids KidStore
	// Backup is the backup execution surface.
	Backup BackupRunner
	// Notify writes notification-center rows (backup success/failure, grade advance,
	// grade-change notices to kids).
	Notify Notifier
	// Pages is the page write surface (the reorder task inserts new page rows).
	Pages PageStore
}

// TaskStore is the subset of domain.TaskRepository that the Worker needs.
type TaskStore interface {
	// Claim atomically takes the head pending task and sets it running.
	Claim(ctx context.Context) (*domain.Task, error)
	Finish(ctx context.Context, id int64) error
	// Skip marks skipped and writes last_error=reason.
	Skip(ctx context.Context, id int64, reason string) error
	// Fail records a failure; when retryable=true it returns to pending, otherwise it is set to
	// failed, per the retry rules.
	Fail(ctx context.Context, id int64, lastError string, retryable bool) error
}

// KidStore is the subset of domain.KidRepository that the Worker needs.
type KidStore interface {
	// GetByID fetches a student; returns domain.ErrNotFound if absent.
	GetByID(ctx context.Context, id int64) (*domain.Kid, error)
	// Update updates mutable fields (grade advance uses it to change grade).
	Update(ctx context.Context, kid *domain.Kid) error
	// Graduate sets graduation status=2.
	Graduate(ctx context.Context, id int64) error
}

// PageStore is the subset of domain.PageRepository that the Worker needs (the reorder task inserts
// new page rows).
type PageStore interface {
	// InsertIfAbsent batch-inserts new pages, skipping on (exam_id, page_index) conflict; it
	// returns the number actually inserted, which is the idempotency guarantee for reruns.
	InsertIfAbsent(ctx context.Context, pages []*domain.Page) (int, error)
}

// BackupRunner is the backup execution surface (adapts the multi-return backup.Service.Create).
type BackupRunner interface {
	// CreateBackup runs one full backup (VACUUM INTO + integrity check + tiered retention).
	CreateBackup(ctx context.Context) error
}

type BackupRunnerFunc func(ctx context.Context) error

func (f BackupRunnerFunc) CreateBackup(ctx context.Context) error { return f(ctx) }

// Notifier writes the notification-center rows the Worker produces; its signatures match
// service.MessageService, so the service can be injected directly.
type Notifier interface {
	// CreateParentNotice writes one family-level (parent) notice and returns its autoincrement id.
	CreateParentNotice(ctx context.Context, kind int, title, body, link string) (int64, error)
	// CreateKidNotice writes one notice bound to kidID and returns its autoincrement id.
	CreateKidNotice(ctx context.Context, kidID int64, kind int, title, body, link string) (int64, error)
}

// handlerFunc is the consumption logic for one task kind.
//
// nil → done; errSkipped → skipped (writes reason); plain error → failed; errNonRetryable → failed
// with no further retries.
type handlerFunc func(ctx context.Context, deps Deps, task *domain.Task) error

// dispatch maps kind → handler. Adding a task type is adding a row, with no change to the main
// loop.
var dispatch = map[int]handlerFunc{
	domain.TaskKindAggregateGaps:  handleAggregateGapsUnavailable,
	domain.TaskKindAdvanceGrade:   handleAdvanceGrade,
	domain.TaskKindBackup:         handleBackup,
	domain.TaskKindCleanupStorage: handleCleanupStorage,
	domain.TaskKindReorderPages:   handleReorderPages,
}

// errSkipped is the "task not applicable" terminal signal: the Worker sets status=skipped. It is
// the third endpoint alongside success (done) and failure (failed).
type errSkipped struct{ reason string }

func (e *errSkipped) Error() string { return e.reason }

func skip(reason string) error { return &errSkipped{reason} }

// errNonRetryable is the "retrying is pointless" failure signal: the Worker sets failed directly.
type errNonRetryable struct{ err error }

func (e *errNonRetryable) Error() string { return e.err.Error() }

func (e *errNonRetryable) Unwrap() error { return e.err }

func nonRetryable(err error) error { return &errNonRetryable{err} }

// Run starts the Worker main loop and returns when ctx is canceled.
//
// Single resident goroutine: Claim → execute → set terminal state → claim the next immediately;
// when empty it waits on a wake signal (deps.Wake) with a pollInterval fallback. Cancellation at
// any wait point exits immediately; a task in flight is not interrupted and ends naturally as ctx
// propagates into the business logic.
func Run(ctx context.Context, deps Deps) {
	log := deps.Log
	if log == nil {
		log = slog.Default()
	}
	deps.Log = log
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	interval := deps.PollInterval
	if interval <= 0 {
		interval = pollInterval
	}

	log.Info("background task consumer goroutine started; serial execution, one task at a time", "poll_interval", interval.String())
	for {
		task, err := deps.Tasks.Claim(ctx)
		if errors.Is(err, domain.ErrNotFound) {
			// Queue empty: wait for a wake signal or the fallback interval.
			if !waitIdle(ctx, deps.Wake, interval) {
				return
			}
			continue
		}
		if err != nil {
			log.Error("failed to claim task", "err", err)
			if !waitIdle(ctx, deps.Wake, interval) {
				return
			}
			continue
		}

		handler, ok := dispatch[task.Kind]
		if !ok {
			handler = func(_ context.Context, _ Deps, _ *domain.Task) error {
				return skip(skipUnknownReason)
			}
		}
		runUnit(ctx, deps, task, handler)
	}
}

// runUnit executes one task and pushes it to a terminal state per the result. A failure to write
// that state is only logged, not retried: the task has already left pending, and retrying the unit
// would duplicate side effects.
func runUnit(ctx context.Context, deps Deps, task *domain.Task, handler handlerFunc) {
	log := deps.Log

	var sk *errSkipped
	err := handler(ctx, deps, task)
	switch {
	case err == nil:
		if ferr := deps.Tasks.Finish(ctx, task.ID); ferr != nil {
			log.Error("failed to mark task finished", "task_id", task.ID, "kind", task.Kind, "err", ferr)
		}
		log.Info("task finished", "task_id", task.ID, "kind", task.Kind)
	case errors.As(err, &sk):
		if serr := deps.Tasks.Skip(ctx, task.ID, sk.reason); serr != nil {
			log.Error("failed to mark task skipped", "task_id", task.ID, "kind", task.Kind, "err", serr)
		}
		log.Info("task skipped", "task_id", task.ID, "kind", task.Kind, "reason", sk.reason)
	default:
		var nre *errNonRetryable
		terr := deps.Tasks.Fail(ctx, task.ID, err.Error(), !errors.As(err, &nre))
		if terr != nil {
			log.Error("failed to mark task failed", "task_id", task.ID, "kind", task.Kind, "err", terr)
		}
		log.Error("task execution failed", "task_id", task.ID, "kind", task.Kind, "err", err)
	}
}

// waitIdle blocks until a wake signal arrives, the fallback interval elapses, or ctx is canceled.
// It returns false only on cancellation (the caller then exits). A signal is advisory: waking early
// just re-polls the queue.
func waitIdle(ctx context.Context, wake <-chan struct{}, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return true
	case <-t.C:
		return true
	}
}

// assetsExamsPrefix is the only directory prefix that cleanup_storage may clean.
const assetsExamsPrefix = "assets/exams/"

// examDirWithinAssets validates that detail.dir is under assets/exams/ and returns the sanitized
// relative path.
//
// Path-traversal guard: only this prefix is accepted, and the cleaned path is checked again to
// still be under it (rejecting "assets/exams/../x" that escapes after Clean); the root directory
// itself cannot be cleaned either.
func examDirWithinAssets(dir string) (string, error) {
	if !strings.HasPrefix(dir, assetsExamsPrefix) {
		return "", nonRetryable(errors.New("cleanup directory out of bounds (must be under assets/exams/): " + dir))
	}
	clean := path.Clean(dir)
	if clean == "assets/exams" || !strings.HasPrefix(clean, assetsExamsPrefix) {
		return "", nonRetryable(errors.New("cleanup directory out of bounds (escaped assets/exams/ after path cleaning): " + dir))
	}
	return clean, nil
}
