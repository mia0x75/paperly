package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// sqlDateTime formats a time like datetime('now') so it can scan into *time.Time.
func sqlDateTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// insertTimedTask writes a task directly with explicit created_at / finished_at,
// for testing time ordering and retention. priority defaults to 50; status and
// detail are configurable, and a finishedAt of "" means NULL.
func insertTimedTask(t *testing.T, db *DB, kind, priority, status int, createdAt, finishedAt string) int64 {
	t.Helper()
	var fin interface{}
	if finishedAt != "" {
		fin = finishedAt
	}
	res, err := db.Write().ExecContext(context.Background(), `
INSERT INTO tasks (kind, priority, status, detail, created_at, finished_at)
VALUES (?, ?, ?, '{}', ?, ?)`,
		kind, priority, status, createdAt, fin)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

func TestTaskRepoInsertRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	id, err := repo.Insert(ctx, &domain.Task{
		Kind:     domain.TaskKindAdvanceGrade,
		Priority: 80,
		Status:   domain.TaskStatusPending,
		Detail:   `{"kid_id":1,"enrolled_year":2026,"enrolled_grade":7,"target_grade":8}`,
	})
	require.NoError(t, err)
	require.Positive(t, id)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.TaskKindAdvanceGrade, got.Kind)
	require.Equal(t, 80, got.Priority)
	require.Equal(t, domain.TaskStatusPending, got.Status)
	require.Equal(t, 0, got.RetryCount)
	require.Contains(t, got.Detail, `"kid_id":1`)
}

func TestTaskRepoClaimEmptyQueue(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	_, err := repo.Claim(ctx)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

// TestTaskRepoClaimPicksPriorityThenFIFO: higher priority is claimed first;
// within equal priority, the earliest enqueue wins.
func TestTaskRepoClaimPicksPriorityThenFIFO(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	idB := insertTimedTask(t, db, domain.TaskKindBackup, 80, domain.TaskStatusPending, "2026-01-01 00:00:00", "")
	idC := insertTimedTask(t, db, domain.TaskKindBackup, 50, domain.TaskStatusPending, "2026-01-01 00:00:00", "")
	idA := insertTimedTask(t, db, domain.TaskKindBackup, 50, domain.TaskStatusPending, "2026-01-01 00:00:01", "")

	got, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, idB, got.ID, "priority 80 应先被领走")

	got, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, idC, got.ID, "同 priority 时 created_at 更早的先领")

	got, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, idA, got.ID)

	_, err = repo.Claim(ctx)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestTaskRepoClaimMarksRunningAndReturnsDetail(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusPending, `{"x":1}`)

	got, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, domain.TaskKindBackup, got.Kind)
	require.Equal(t, domain.TaskStatusRunning, got.Status)
	require.Equal(t, `{"x":1}`, got.Detail)
	require.NotNil(t, got.StartedAt, "领单须写 started_at")
	require.Nil(t, got.FinishedAt)

	// Once claimed, cannot be claimed again.
	_, err = repo.Claim(ctx)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestTaskRepoFinishMarksDone(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	id := insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusPending, "{}")
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.Equal(t, id, claimed.ID)

	require.NoError(t, repo.Finish(ctx, id))

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusDone, got.Status)
	require.NotNil(t, got.FinishedAt)
}

func TestTaskRepoFinishRejectsNonRunning(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	id := insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusPending, "{}")

	err := repo.Finish(ctx, id)
	require.ErrorIs(t, err, domain.ErrNotFound, "pending 任务不允许直接被完成")
}

func TestTaskRepoSkipSetsSkipped(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	id := insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusPending, "{}")
	_, err := repo.Claim(ctx)
	require.NoError(t, err)

	require.NoError(t, repo.Skip(ctx, id, "不适用"))

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusSkipped, got.Status)
	require.NotNil(t, got.FinishedAt)
	require.NotNil(t, got.LastError)
	require.Equal(t, "不适用", *got.LastError)
}

// TestTaskRepoFailRetryCycle: a retryable failure increments retry_count. After
// retry_count+1 the task goes back to pending below 3 and fails at 3 or more,
// matching the DATABASE_DESIGN task semantics.
func TestTaskRepoFailRetryCycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	id := insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusPending, "{}")

	// 1st failure: 0→1, still pending.
	_, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Fail(ctx, id, "e1", true))

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusPending, got.Status)
	require.Equal(t, 1, got.RetryCount)
	require.Nil(t, got.FinishedAt, "回 pending 的任务不算结束")
	require.Nil(t, got.StartedAt, "回 pending 清 started_at，下轮 Claim 重新计时")
	require.Equal(t, "e1", *got.LastError)

	// 2nd failure: 1→2, still pending.
	_, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Fail(ctx, id, "e2", true))
	got, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusPending, got.Status)
	require.Equal(t, 2, got.RetryCount)

	// 3rd failure: 2→3, retries exhausted.
	_, err = repo.Claim(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Fail(ctx, id, "e3", true))
	got, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusFailed, got.Status)
	require.Equal(t, 3, got.RetryCount)
	require.NotNil(t, got.FinishedAt, "失败达到上限置 failed 并收尾")
	require.Equal(t, "e3", *got.LastError)
}

func TestTaskRepoFailNonRetryable(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	id := insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusPending, "{}")
	_, err := repo.Claim(ctx)
	require.NoError(t, err)

	require.NoError(t, repo.Fail(ctx, id, "参数越界", false))

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusFailed, got.Status)
	require.Equal(t, 0, got.RetryCount, "不可重试失败不消耗重试名额")
	require.Equal(t, "参数越界", *got.LastError)
}

// TestTaskRepoClearOlderThan: only clears finalized tasks (done/failed/skipped) older than days;
// pending/running are never time-budget cleaned.
func TestTaskRepoClearOlderThan(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	// Three old finalized tasks: should be cleared.
	oldDone := insertTimedTask(t, db, domain.TaskKindBackup, 50, domain.TaskStatusDone, "2026-01-01 00:00:00", "2026-01-01 00:00:05")
	oldFailed := insertTimedTask(t, db, domain.TaskKindBackup, 50, domain.TaskStatusFailed, "2026-01-01 00:00:00", "2026-01-01 00:00:05")
	oldSkipped := insertTimedTask(t, db, domain.TaskKindBackup, 50, domain.TaskStatusSkipped, "2026-01-01 00:00:00", "2026-01-01 00:00:05")
	// Recent done: should be kept.
	newDone := insertTimedTask(t, db, domain.TaskKindBackup, 50, domain.TaskStatusDone, "2026-01-01 00:00:00", sqlDateTime(time.Now()))
	// running is not cleared even if very old (not a finalized state).
	running := insertTimedTask(t, db, domain.TaskKindBackup, 50, domain.TaskStatusRunning, "2026-01-01 00:00:00", "2026-01-01 00:00:05")
	// pending (finished_at empty) is not cleared.
	pending := insertTimedTask(t, db, domain.TaskKindBackup, 50, domain.TaskStatusPending, "2026-01-01 00:00:00", "")

	n, err := repo.ClearOlderThan(ctx, 30)
	require.NoError(t, err)
	require.EqualValues(t, 3, n, "旧 done/failed/skipped 各清 1 条")

	remaining := map[int64]bool{}
	rows, err := repo.List(ctx, domain.ListTasksFilter{Limit: 100})
	require.NoError(t, err)
	for _, r := range rows {
		remaining[r.ID] = true
	}
	for _, id := range []int64{oldDone, oldFailed, oldSkipped} {
		require.False(t, remaining[id], "旧收尾任务应被清掉")
	}
	for _, id := range []int64{newDone, running, pending} {
		require.True(t, remaining[id], "新收尾 / running / pending 应保留")
	}
}

// countingNotifier records how many times Notify was called.
type countingNotifier struct{ n int }

func (c *countingNotifier) Notify() { c.n++ }

// TestTaskRepoInsertNotifies: a successful Insert wakes the Worker exactly once.
func TestTaskRepoInsertNotifies(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	n := &countingNotifier{}
	repo := NewTaskRepoSQLite(db).WithNotifier(n)

	_, err := repo.Insert(ctx, &domain.Task{
		Kind:     domain.TaskKindBackup,
		Priority: 80,
		Status:   domain.TaskStatusPending,
		Detail:   "{}",
	})
	require.NoError(t, err)
	require.Equal(t, 1, n.n, "入队成功后应唤醒 Worker 一次")
}

// TestTaskRepoInsertWithoutNotifierIsNoop: no notifier injected → Insert must not panic.
func TestTaskRepoInsertWithoutNotifierIsNoop(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	_, err := repo.Insert(ctx, &domain.Task{
		Kind:     domain.TaskKindBackup,
		Priority: 80,
		Status:   domain.TaskStatusPending,
		Detail:   "{}",
	})
	require.NoError(t, err, "未注入 notifier 时不得 panic")
}
