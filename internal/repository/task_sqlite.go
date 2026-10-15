package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"paperly/internal/domain"
)

// Explicit column list; SELECT * is forbidden (AGENTS.md SQLite rules).
const taskColumns = `id, kind, priority, status, retry_count, last_error, detail, created_at, started_at, finished_at`

// Non-blocking enqueue wake signal; nil is a no-op (tests and non-worker call sites).
type Notifier interface {
	Notify()
}

type TaskRepoSQLite struct {
	baseRepo
	// notifier is optional; when set, a successful Insert wakes the Worker.
	notifier Notifier
}

var _ domain.TaskRepository = (*TaskRepoSQLite)(nil)

func NewTaskRepoSQLite(db *DB) *TaskRepoSQLite {
	return &TaskRepoSQLite{baseRepo: baseRepo{db: db}}
}

// WithNotifier attaches the enqueue wake notifier and returns the receiver for chaining.
// Kept separate from the constructor so existing call sites (main and tests) are unaffected.
func (r *TaskRepoSQLite) WithNotifier(n Notifier) *TaskRepoSQLite {
	r.notifier = n
	return r
}

func (r *TaskRepoSQLite) WithTx(tx any) domain.TaskRepository {
	return &TaskRepoSQLite{baseRepo: r.mustBindTx(tx), notifier: r.notifier}
}

// Assembles WHERE fragment and arguments; all values are parameterized.
func taskFilterSQL(f domain.ListTasksFilter) (string, []any) {
	var clauses []string
	var args []any
	if f.HasStatus {
		clauses = append(clauses, "status = ?")
		args = append(args, f.Status)
	}
	if f.Cursor > 0 {
		clauses = append(clauses, "id < ?")
		args = append(args, f.Cursor)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// Returns tasks ordered id DESC. The caller passes Limit+1 to probe has_more.
// Uses the read pool.
func (r *TaskRepoSQLite) List(ctx context.Context, f domain.ListTasksFilter) ([]*domain.Task, error) {
	where, args := taskFilterSQL(f)
	var out []*domain.Task
	//nolint:gosec // where is assembled only from fixed keywords and placeholders; all values go through parameters.
	q := `SELECT ` + taskColumns + ` FROM tasks` + where + ` ORDER BY id DESC LIMIT ?`
	if err := r.readExt().SelectContext(ctx, &out, q, append(args, f.Limit)...); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	if out == nil {
		out = make([]*domain.Task, 0)
	}
	return out, nil
}

// Single GROUP BY fetches the full distribution rather than issuing one COUNT per status.
// Uses the read pool.
func (r *TaskRepoSQLite) CountByStatus(ctx context.Context) (map[int]int, error) {
	var rows []struct {
		Status int `db:"status"`
		Count  int `db:"count"`
	}
	if err := r.readExt().SelectContext(ctx, &rows,
		`SELECT status, COUNT(*) AS count FROM tasks GROUP BY status`); err != nil {
		return nil, fmt.Errorf("count tasks by status: %w", err)
	}
	out := make(map[int]int, len(rows))
	for _, r := range rows {
		out[r.Status] = r.Count
	}
	return out, nil
}

func (r *TaskRepoSQLite) GetByID(ctx context.Context, id int64) (*domain.Task, error) {
	var t domain.Task
	err := r.readExt().GetContext(ctx, &t,
		`SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get task by id: %w", err)
	}
	return &t, nil
}

// WHERE carries status=failed; this is the atomic "only failed tasks are retryable" expression.
// Without it, check-then-write could knock a task claimed between calls back to pending,
// causing double execution. Uses the write pool.
func (r *TaskRepoSQLite) Retry(ctx context.Context, id int64) (int64, error) {
	res, err := r.writeExt().ExecContext(ctx, `
UPDATE tasks
SET status      = ?,
    retry_count = retry_count + 1,
    last_error  = NULL,
    started_at  = NULL,
    finished_at = NULL
WHERE id = ? AND status = ?`,
		domain.TaskStatusPending, id, domain.TaskStatusFailed)
	if err != nil {
		return 0, fmt.Errorf("retry task %d: %w", id, err)
	}
	return res.RowsAffected()
}

// IN allowlist (done, skipped) rather than NOT IN (pending, running) so future states are
// not cleaned by mistake. Uses the write pool.
func (r *TaskRepoSQLite) ClearFinished(ctx context.Context) (int64, error) {
	res, err := r.writeExt().ExecContext(ctx,
		`DELETE FROM tasks WHERE status IN (?, ?)`, domain.TaskStatusDone, domain.TaskStatusSkipped)
	if err != nil {
		return 0, fmt.Errorf("clear finished tasks: %w", err)
	}
	return res.RowsAffected()
}

// Shared by TaskRepoSQLite.Insert and PageRepoSQLite.RebuildPages (same-transaction enqueue).
const taskInsertSQL = `
INSERT INTO tasks (kind, priority, status, retry_count, detail)
VALUES (:kind, :priority, :status, :retry_count, :detail)`

// Enqueues a pending task; status/retry_count come from the caller (default pending/0).
// No timestamps are written (SQLite defaults + Claim/Finish). Uses the write pool.
func (r *TaskRepoSQLite) Insert(ctx context.Context, t *domain.Task) (int64, error) {
	res, err := r.writeExt().NamedExecContext(ctx, taskInsertSQL, t)
	if err != nil {
		return 0, fmt.Errorf("insert task: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("task last insert id: %w", err)
	}
	if r.notifier != nil {
		r.notifier.Notify()
	}
	return id, nil
}

// Single-statement UPDATE ... WHERE id=(SELECT id ... LIMIT 1) fetch-and-occupies atomically.
// Queue head: priority DESC, created_at ASC, id ASC (uses index idx_tasks_status_priority).
// Returns domain.ErrNotFound when queue is empty. Uses the write pool.
func (r *TaskRepoSQLite) Claim(ctx context.Context) (*domain.Task, error) {
	var t domain.Task
	err := r.writeExt().GetContext(ctx, &t, `
UPDATE tasks
SET status = ?, started_at = datetime('now')
WHERE id = (
	SELECT id FROM tasks
	WHERE status = ?
	ORDER BY priority DESC, created_at ASC, id ASC
	LIMIT 1
)
RETURNING `+taskColumns,
		domain.TaskStatusRunning, domain.TaskStatusPending)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("claim next task: %w", err)
	}
	return &t, nil
}

// WHERE carries status=running; only a task the Worker claimed and still running may be
// finished. ErrNotFound is returned if the condition does not match. Uses the write pool.
func (r *TaskRepoSQLite) Finish(ctx context.Context, id int64) error {
	res, err := r.writeExt().ExecContext(ctx, `
UPDATE tasks
SET status = ?, finished_at = datetime('now')
WHERE id = ? AND status = ?`,
		domain.TaskStatusDone, id, domain.TaskStatusRunning)
	if err != nil {
		return fmt.Errorf("finish task %d: %w", id, err)
	}
	return affectedOrNotFound(res, "task")
}

// Sets status=skipped, last_error=reason, finished_at. Only the running state may be
// skipped. Uses the write pool.
func (r *TaskRepoSQLite) Skip(ctx context.Context, id int64, reason string) error {
	res, err := r.writeExt().ExecContext(ctx, `
UPDATE tasks
SET status = ?, last_error = ?, finished_at = datetime('now')
WHERE id = ? AND status = ?`,
		domain.TaskStatusSkipped, reason, id, domain.TaskStatusRunning)
	if err != nil {
		return fmt.Errorf("skip task %d: %w", id, err)
	}
	return affectedOrNotFound(res, "task")
}

// Retry semantics (retryable=true): retry_count+1; if still below TaskMaxRetryCount, return
// to pending and clear started_at/finished_at so next Claim restarts timing. If at limit,
// set to failed. retryable=false: set failed directly. Only the running state may be marked
// failed. Uses the write pool.
func (r *TaskRepoSQLite) Fail(ctx context.Context, id int64, lastError string, retryable bool) error {
	if retryable {
		res, err := r.writeExt().ExecContext(ctx, `
UPDATE tasks
SET status      = CASE WHEN retry_count + 1 < ? THEN ? ELSE ? END,
    retry_count = retry_count + 1,
    last_error  = ?,
    started_at  = NULL,
    finished_at = CASE WHEN retry_count + 1 < ? THEN NULL ELSE datetime('now') END
WHERE id = ? AND status = ?`,
			domain.TaskMaxRetryCount, domain.TaskStatusPending, domain.TaskStatusFailed,
			lastError,
			domain.TaskMaxRetryCount, id, domain.TaskStatusRunning)
		if err != nil {
			return fmt.Errorf("fail task %d: %w", id, err)
		}
		return affectedOrNotFound(res, "task")
	}

	res, err := r.writeExt().ExecContext(ctx, `
UPDATE tasks
SET status = ?, last_error = ?, finished_at = datetime('now')
WHERE id = ? AND status = ?`,
		domain.TaskStatusFailed, lastError, id, domain.TaskStatusRunning)
	if err != nil {
		return fmt.Errorf("fail task %d: %w", id, err)
	}
	return affectedOrNotFound(res, "task")
}

// Deletes explicitly concluded tasks (done/failed/skipped) with finished_at < datetime('now', ?)
// (status IN allowlist). pending/running are never time-cleaned; rows with NULL finished_at
// are not deleted. The comparison is inside SQLite (datetime('now', ?) relative to UTC),
// matching finished_at's storage convention. Uses the write pool.
func (r *TaskRepoSQLite) ClearOlderThan(ctx context.Context, days int) (int64, error) {
	res, err := r.writeExt().ExecContext(ctx, `
DELETE FROM tasks
WHERE status IN (?, ?, ?)
  AND finished_at IS NOT NULL
  AND finished_at < datetime('now', ?)`,
		domain.TaskStatusDone, domain.TaskStatusFailed, domain.TaskStatusSkipped,
		fmt.Sprintf("-%d days", days))
	if err != nil {
		return 0, fmt.Errorf("clear tasks older than %d days: %w", days, err)
	}
	return res.RowsAffected()
}
