package domain

import (
	"context"
	"time"
)

// Background task types (tasks.kind).
const (
	TaskKindAggregateGaps  = 1  // aggregate diagnostics (gaps)
	TaskKindAdvanceGrade   = 2  // auto +1 on 9/1
	TaskKindBackup         = 3  // backup
	TaskKindCleanupStorage = 4  // file cleanup
	TaskKindReorderPages   = 5  // delete/add/reorder pages
	TaskKindOther          = 99 // other
)

// tasks.status is declared in dashboard.go (shared with the home "pending tasks" card) and not
// redeclared here — two enums would drift, leaving the dashboard and the ops page disagreeing on
// status values.

// TaskMaxRetryCount caps retries per task. Beyond it a task stays failed: an infinitely retried task,
// re-triggered by repeated "retry" clicks, would occupy the worker queue forever and block others.
const TaskMaxRetryCount = 3

// TaskListLimit is the default task-list page size.
const TaskListLimit = 50

// ListTasksFilter is the task-list filter (keyset cursor, id DESC).
type ListTasksFilter struct {
	// Status is the status filter; 0 means no filter.
	//
	// HasStatus exists because 0 is also a valid TaskStatusPending: it expresses "filter by status"
	// explicitly, avoiding "filter pending" being read as "no filter".
	Status    int
	HasStatus bool
	Cursor    int64
	Limit     int
}

// Task corresponds to the tasks table.
//
// Payload-only, no foreign keys: association targets live in the detail JSON. They are deliberately
// not columns — different kinds target different entities, so kid_id/exam_id/dir would leave half of
// them always NULL.
type Task struct {
	ID         int64      `db:"id"`
	Kind       int        `db:"kind"`
	Priority   int        `db:"priority"`
	Status     int        `db:"status"`
	RetryCount int        `db:"retry_count"`
	LastError  *string    `db:"last_error"`
	Detail     string     `db:"detail"`
	CreatedAt  time.Time  `db:"created_at"`
	StartedAt  *time.Time `db:"started_at"`
	FinishedAt *time.Time `db:"finished_at"`
}

// TaskRepository is the tasks data-access contract.
type TaskRepository interface {
	// List returns tasks id DESC; Cursor>0 keeps id<Cursor; Limit+1 detects has_more.
	List(ctx context.Context, f ListTasksFilter) ([]*Task, error)
	// CountByStatus returns the task count per status (via the status index, not row by row).
	CountByStatus(ctx context.Context) (map[int]int, error)
	// GetByID queries by id; ErrNotFound when missing.
	GetByID(ctx context.Context, id int64) (*Task, error)
	// Retry resets a failed task to pending: retry_count+1, clears last_error and finished_at. A
	// count of 0 means "missing or not failed"; the caller turns that into ErrInvalidInput.
	Retry(ctx context.Context, id int64) (int64, error)
	// ClearFinished clears done/skipped tasks, returning the deletion count.
	ClearFinished(ctx context.Context) (int64, error)
	// Insert enqueues a task (status=pending), returning the auto-increment id.
	Insert(ctx context.Context, t *Task) (int64, error)
	// Claim atomically takes the head pending task and sets it running (writes started_at).
	//
	// Head = higher priority first, then FIFO (idx_tasks_status_priority). A single
	// UPDATE...RETURNING does both take and claim; SQLite statements are atomic, so no second
	// executor can snatch it between query and write. Returns ErrNotFound when the queue is empty.
	Claim(ctx context.Context) (*Task, error)
	// Finish marks a claimed task done (writes finished_at). Only running can finish; an already
	// non-running task (e.g. re-queued by retry) returns ErrNotFound.
	Finish(ctx context.Context, id int64) error
	// Skip skips a task: sets skipped and writes last_error=reason, finished_at. For
	// "not implemented / not applicable" placeholders. Only running can skip.
	Skip(ctx context.Context, id int64, reason string) error
	// Fail records one failure: retryable increments retry_count and goes back to pending for
	// another try; otherwise it goes failed. Both write last_error. Retry rules see DATABASE_DESIGN
	// tasks section 7.2. Only running can fail.
	Fail(ctx context.Context, id int64, lastError string, retryable bool) error
	// ClearOlderThan clears tasks finished (done/failed/skipped) more than days ago, returning the
	// deletion count. Section 7.4 retention: caps the table; failed tasks get an observation period
	// and are not cleared immediately.
	ClearOlderThan(ctx context.Context, days int) (int64, error)
	// WithTx returns a new instance bound to tx.
	WithTx(tx Tx) TaskRepository
}
