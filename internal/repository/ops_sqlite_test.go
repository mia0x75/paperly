package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

func opsCursorRows(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Write().ExecContext(ctx, `
INSERT INTO cursors (task_name, last_id, last_run_at, last_status, updated_at)
VALUES ('daily_mastery', 100, '2026-09-28 03:00:00', 2, '2026-09-28 03:00:00'),
       ('metric_cleanup', 0, NULL, 0, '2026-09-28 03:00:00')`)
	require.NoError(t, err)
}

func TestOpsRepoCursorGetUpsertList(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewMaintenanceCursorRepoSQLite(db)

	_, err := repo.Get(ctx, "daily_mastery")
	require.ErrorIs(t, err, domain.ErrNotFound, "首次运行应返回 ErrNotFound 而非 SQL 错误")

	opsCursorRows(t, db)
	c, err := repo.Get(ctx, "daily_mastery")
	require.NoError(t, err)
	require.EqualValues(t, 100, c.LastID)
	require.Equal(t, domain.CursorStatusCompleted, c.LastStatus)
	require.NotNil(t, c.LastRunAt)

	// A row with last_run_at NULL must not be scanned into the zero time.
	pending, err := repo.Get(ctx, "metric_cleanup")
	require.NoError(t, err)
	require.Nil(t, pending.LastRunAt, "NULL 应保持 nil，不能变 0001-01-01")

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, "daily_mastery", list[0].TaskName, "按 task_name 字典序")

	// upsert goes through ON CONFLICT, so no second row is inserted.
	runAt := time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Upsert(ctx, &domain.MaintenanceCursor{
		TaskName:   "daily_mastery",
		LastID:     250,
		LastRunAt:  &runAt,
		LastStatus: domain.CursorStatusRunning,
	}))
	list, err = repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2, "upsert 不得新增行")
	require.EqualValues(t, 250, list[0].LastID)
	require.Equal(t, domain.CursorStatusRunning, list[0].LastStatus)
}

func TestOpsRepoVacuumIntoAndIntegrity(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	ops := NewOpsRepoSQLite(db)

	dest := filepath.Join(t.TempDir(), "snapshot.db")
	require.NoError(t, ops.VacuumInto(ctx, dest))
	require.FileExists(t, dest)

	// The snapshot must be a readable, integrity-passing standalone SQLite file.
	result, err := ops.VerifyFileIntegrity(ctx, dest)
	require.NoError(t, err)
	require.Equal(t, "ok", result)

	main, err := ops.IntegrityCheck(ctx)
	require.NoError(t, err)
	require.Equal(t, "ok", main)

	// VACUUM INTO refuses to overwrite, so a retry removes the old file first.
	require.NoError(t, ops.VacuumInto(ctx, dest), "同名重跑应先摘除旧文件再写")

	// A missing file must error, not return an empty conclusion.
	_, err = ops.VerifyFileIntegrity(ctx, filepath.Join(t.TempDir(), "nope.db"))
	require.Error(t, err)
}

func TestOpsRepoVacuumIntoRejectsInjection(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	ops := NewOpsRepoSQLite(db)

	// VACUUM INTO takes no bound parameter for the destination path, so the
	// path is spliced into SQL and validation is the only defense. The rule is
	// simply "can a single quote close the literal early".
	for _, bad := range []string{
		"a.db'; DROP TABLE kids; --",
		"a.db'",
		"a\nb.db",
		"a\x00b.db",
		"",
	} {
		require.Error(t, ops.VacuumInto(ctx, bad), "路径 %q 必须被拒绝", bad)
	}

	// Backticks and spaces are harmless inside a literal and must pass,
	// otherwise installs under paths with spaces could not back up.
	dir := t.TempDir()
	for _, name := range []string{"a b.db", "a`b.db", "中文.db"} {
		require.NoError(t, ops.VacuumInto(ctx, filepath.Join(dir, name)),
			"路径 %q 不该被拒", name)
	}
}

func TestOpsRepoDBSizeAndOptimize(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	ops := NewOpsRepoSQLite(db)

	size, err := ops.DBSize(ctx)
	require.NoError(t, err)
	require.Greater(t, size, int64(0), "已建表的库必有体积")

	require.NoError(t, ops.Optimize(ctx))
}

func TestOpsRepoSchemaDisplayTag(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	ops := NewOpsRepoSQLite(db)

	tag, err := ops.SchemaDisplayTag(ctx)
	require.NoError(t, err)
	require.Equal(t, "v1.0.0", tag, "正常库读到 display_tag")

	// Clearing the table leaves 0 rows, so the outer COALESCE falls back to
	// "unknown" rather than returning an empty result set.
	_, err = db.Write().ExecContext(ctx, `DELETE FROM migrations`)
	require.NoError(t, err)
	tag, err = ops.SchemaDisplayTag(ctx)
	require.NoError(t, err)
	require.Equal(t, "unknown", tag, "0 行必须回显 unknown")

	// A NULL value must also be caught.
	_, err = db.Write().ExecContext(ctx, `INSERT INTO migrations (version, name, display_tag) VALUES (99, 'test.sql', NULL)`)
	require.NoError(t, err)
	tag, err = ops.SchemaDisplayTag(ctx)
	require.NoError(t, err)
	require.Equal(t, "unknown", tag, "NULL 必须回显 unknown")
}

func TestSystemMetricRepoIncSumMaxLatest(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewSystemMetricRepoSQLite(db)

	require.NoError(t, repo.Inc(ctx, "demo_success"))
	require.NoError(t, repo.Inc(ctx, "demo_success"))
	require.NoError(t, repo.Inc(ctx, "demo_fail"))

	sum, err := repo.SumSince(ctx, "demo_fail", 7)
	require.NoError(t, err)
	require.Equal(t, 1, sum)
	sum, err = repo.SumSince(ctx, "demo_success", 7)
	require.NoError(t, err)
	require.Equal(t, 2, sum)

	// SetMax is the day-peak semantics, not accumulation.
	require.NoError(t, repo.SetMax(ctx, "write_queue_max_wait_ms", 120))
	require.NoError(t, repo.SetMax(ctx, "write_queue_max_wait_ms", 30))
	require.NoError(t, repo.SetMax(ctx, "write_queue_max_wait_ms", 900))
	mx, err := repo.MaxSince(ctx, "write_queue_max_wait_ms", 7)
	require.NoError(t, err)
	require.Equal(t, 900, mx)
	sum, err = repo.SumSince(ctx, "write_queue_max_wait_ms", 7)
	require.NoError(t, err)
	require.Equal(t, 900, sum, "SetMax 不该把三次写累加成 1050")

	// LatestAt reads updated_at (minute-level), not metric_date (day-level).
	require.NoError(t, repo.SetMax(ctx, domain.MetricBackupLastAt, 1))
	ts, found, err := repo.LatestAt(ctx, domain.MetricBackupLastAt)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, ts.IsZero())
	require.Equal(t, time.Now().Local().Format("2006-01-02"), ts.Format("2006-01-02"))

	// With no rows returns zero value + false, no error.
	_, found, err = repo.LatestAt(ctx, "never_happened")
	require.NoError(t, err)
	require.False(t, found)

	// With no rows Sum/Max return 0.
	sum, err = repo.SumSince(ctx, "never_happened", 7)
	require.NoError(t, err)
	require.Equal(t, 0, sum)
	mx, err = repo.MaxSince(ctx, "never_happened", 7)
	require.NoError(t, err)
	require.Equal(t, 0, mx)
}

func TestSystemMetricRepoDeleteOlderThan(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewSystemMetricRepoSQLite(db)

	_, err := db.Write().ExecContext(ctx, `
INSERT INTO metrics (metric_name, metric_date, count, updated_at)
VALUES ('old', '2020-01-01', 5, '2020-01-01 00:00:00'),
       ('keep', '2099-01-01', 3, '2099-01-01 00:00:00')`)
	require.NoError(t, err)

	n, err := repo.DeleteOlderThan(ctx, "2026-01-01")
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	var remain int
	require.NoError(t, db.Read().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM metrics WHERE metric_name='old'`).Scan(&remain))
	require.Equal(t, 0, remain, "过期行应被清掉")
}

func TestValidateBackupPath(t *testing.T) {
	// Must pass: common shapes of real Box installation paths
	ok := []string{
		"/var/lib/paperly/backup/daily/paperly_20260928_100000.db",
		`C:\Program Files\Paperly\data\backup\daily\paperly_20260928_100000.db`,
		"/home/张三/paperly/data/backup/daily/paperly_20260928_100000.db",
		"/tmp/a dir with spaces/paperly.db",
		"/data/backup/my backup (2).db",
		"/data/备份/试卷.db",
	}
	for _, p := range ok {
		require.NoError(t, validateBackupPath(p), "不该拒绝：%s", p)
	}

	// Must reject: can close an SQL string literal early / break downstream
	bad := []string{
		"",
		"/tmp/x.db'; DROP TABLE kids; --",
		"/tmp/x.db'",
		"/tmp/a\nb.db",
		"/tmp/a\rb.db",
		"/tmp/a\x00b.db",
	}
	for _, p := range bad {
		require.Error(t, validateBackupPath(p), "该拒绝：%q", p)
	}
}

func TestVacuumIntoRejectsQuoteInjection(t *testing.T) {
	dir := t.TempDir()
	db := openTestDB(t)
	repo := NewOpsRepoSQLite(db)

	err := repo.VacuumInto(context.Background(), filepath.Join(dir, "b.db'; DROP TABLE kids; --"))
	require.Error(t, err, "含单引号的路径必须在碰数据库前就被拒")
	require.Contains(t, err.Error(), "single quote")

	// Injection did not take effect; the table is still there
	var n int
	require.NoError(t, db.Read().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM kids`).Scan(&n))
	require.Equal(t, 0, n)
}

func TestVacuumIntoPathWithSpaces(t *testing.T) {
	dir := t.TempDir()
	db := openTestDB(t)
	repo := NewOpsRepoSQLite(db)
	// A Box installed under a path with spaces must still back up
	dest := filepath.Join(dir, "Program Files", "paperly_20260928.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o750))
	require.NoError(t, repo.VacuumInto(context.Background(), dest))

	res, err := repo.VerifyFileIntegrity(context.Background(), dest)
	require.NoError(t, err)
	require.Equal(t, "ok", res)
}
