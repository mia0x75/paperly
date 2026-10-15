package health

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// fakeOpsRepo allows injecting counts and errors.
type fakeOpsRepo struct {
	integrity    string
	integrityErr error
	size         int64
	sizeErr      error
	tag          string
	tagErr       error
}

func (f *fakeOpsRepo) VacuumInto(context.Context, string) error { return nil }
func (f *fakeOpsRepo) Optimize(context.Context) error           { return nil }
func (f *fakeOpsRepo) VerifyFileIntegrity(context.Context, string) (string, error) {
	return "ok", nil
}
func (f *fakeOpsRepo) IntegrityCheck(context.Context) (string, error) {
	return f.integrity, f.integrityErr
}
func (f *fakeOpsRepo) DBSize(context.Context) (int64, error) { return f.size, f.sizeErr }
func (f *fakeOpsRepo) ListSchemaMigrations(context.Context) ([]*domain.SchemaMigration, error) {
	return []*domain.SchemaMigration{}, nil
}

func (f *fakeOpsRepo) SchemaDisplayTag(context.Context) (string, error) {
	return f.tag, f.tagErr
}

// fakeMetricRepo provides Sum/Max/Latest aggregation.
type fakeMetricRepo struct {
	sums   map[string]int
	maxs   map[string]int
	latest map[string]time.Time
	found  map[string]bool
	err    error
}

func newFakeMetricRepo() *fakeMetricRepo {
	return &fakeMetricRepo{
		sums:   map[string]int{},
		maxs:   map[string]int{},
		latest: map[string]time.Time{},
		found:  map[string]bool{},
	}
}

func (f *fakeMetricRepo) Inc(context.Context, string) error                      { return nil }
func (f *fakeMetricRepo) SetMax(context.Context, string, int) error              { return nil }
func (f *fakeMetricRepo) SetGauge(context.Context, string, int64) error          { return nil }
func (f *fakeMetricRepo) DeleteOlderThan(context.Context, string) (int64, error) { return 0, nil }

func (f *fakeMetricRepo) DeleteGaugeOlderThan(context.Context, string) (int64, error) { return 0, nil }
func (f *fakeMetricRepo) GaugeSeries(context.Context, []string, int) ([]domain.GaugePoint, error) {
	return nil, nil
}
func (f *fakeMetricRepo) SumSince(_ context.Context, name string, _ int) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.sums[name], nil
}
func (f *fakeMetricRepo) MaxSince(_ context.Context, name string, _ int) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.maxs[name], nil
}
func (f *fakeMetricRepo) LatestAt(_ context.Context, name string) (time.Time, bool, error) {
	if f.err != nil {
		return time.Time{}, false, f.err
	}
	return f.latest[name], f.found[name], nil
}

// fakeCursorRepo returns a fixed cursor.
type fakeCursorRepo struct {
	c   *domain.MaintenanceCursor
	err error
}

func (f *fakeCursorRepo) Get(context.Context, string) (*domain.MaintenanceCursor, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.c == nil {
		return nil, domain.ErrNotFound
	}
	return f.c, nil
}
func (f *fakeCursorRepo) Upsert(context.Context, *domain.MaintenanceCursor) error { return nil }
func (f *fakeCursorRepo) List(context.Context) ([]*domain.MaintenanceCursor, error) {
	return nil, nil
}

var testNow = time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC)

// newHealthy builds an all-green environment.
func newHealthy(t *testing.T) (*Service, *fakeOpsRepo, *fakeMetricRepo, *fakeCursorRepo, string) {
	t.Helper()
	ops := &fakeOpsRepo{integrity: "ok", size: 48 << 20, tag: "v1.0.0"}
	met := newFakeMetricRepo()
	met.sums[domain.MetricRecentWindowParseFail] = 0
	met.found[domain.MetricBackupLastAt] = true
	met.latest[domain.MetricBackupLastAt] = testNow.Add(-2 * time.Hour)
	cur := &fakeCursorRepo{c: &domain.MaintenanceCursor{
		TaskName: "daily_mastery", LastID: 1250,
		LastStatus: domain.CursorStatusCompleted, LastRunAt: &testNow,
	}}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dummy.db"), make([]byte, 4096), 0o600))

	svc := NewService(ops, met, cur, dir)
	svc.nowFn = func() time.Time { return testNow }
	return svc, ops, met, cur, dir
}

func find(r *Report, key string) Check {
	for _, c := range r.Checks {
		if c.Key == key {
			return c
		}
	}
	return Check{Key: "MISSING:" + key}
}

func TestHealthAllGreen(t *testing.T) {
	svc, _, _, _, _ := newHealthy(t)
	r := svc.Check(context.Background())
	require.Equal(t, StatusOK, r.Status, "全绿应为 ok；异常项: %+v", r.Checks)
	require.Len(t, r.Checks, 6, "6 项体检")
	require.NotEmpty(t, r.Summary)
}

func TestHealthCoversRequiredItems(t *testing.T) {
	svc, _, _, _, _ := newHealthy(t)
	r := svc.Check(context.Background())
	keys := map[string]bool{}
	for _, c := range r.Checks {
		keys[c.Key] = true
	}
	for _, k := range []string{
		"database", "schema_version", "last_backup",
		"filesystem", "concurrency", "maintenance_cursor",
	} {
		require.True(t, keys[k], "缺少体检项 %s", k)
	}
}

func TestHealthSchemaVersionY4Dynamic(t *testing.T) {
	svc, ops, _, _, _ := newHealthy(t)
	ops.tag = "v1.0.0"
	r := svc.Check(context.Background())
	c := find(r, "schema_version")
	require.Equal(t, StatusOK, c.Status)
	require.Equal(t, "v1.0.0", c.Value, "必须动态读，不硬编码")

	// unknown -> warn rather than error/crash
	ops.tag = "unknown"
	r = svc.Check(context.Background())
	c = find(r, "schema_version")
	require.Equal(t, StatusWarn, c.Status, "空库应告警而非崩")
	require.Contains(t, c.Detail, "迁移")
}

func TestHealthDatabaseOversize(t *testing.T) {
	svc, ops, _, _, _ := newHealthy(t)
	ops.size = 5 << 30 // 5GB
	r := svc.Check(context.Background())
	c := find(r, "database")
	require.Equal(t, StatusWarn, c.Status)
	require.Contains(t, c.Value, "GB")
}

func TestHealthDatabaseIntegrityFail(t *testing.T) {
	svc, ops, _, _, _ := newHealthy(t)
	ops.integrity = "page 42 corrupt"
	r := svc.Check(context.Background())
	require.Equal(t, StatusError, r.Status)
	c := find(r, "database")
	require.Equal(t, StatusError, c.Status)
	require.Contains(t, c.Detail, "page 42 corrupt")
}

func TestHealthLastBackupStaleAndNever(t *testing.T) {
	svc, _, met, _, _ := newHealthy(t)
	// 3 days ago -> warn
	met.latest[domain.MetricBackupLastAt] = testNow.AddDate(0, 0, -3)
	c := find(svc.Check(context.Background()), "last_backup")
	require.Equal(t, StatusWarn, c.Status)
	require.Contains(t, c.Detail, "超过")

	// Never backed up -> warn and prompt to back up immediately
	met.found[domain.MetricBackupLastAt] = false
	c = find(svc.Check(context.Background()), "last_backup")
	require.Equal(t, StatusWarn, c.Status)
	require.Equal(t, "从未备份", c.Value)
}

func TestHealthConcurrency(t *testing.T) {
	svc, _, met, _, _ := newHealthy(t)
	met.sums[domain.MetricWriteConflictRollback] = 3
	met.maxs[domain.MetricWriteQueueMaxWaitMS] = 2500
	met.sums[domain.MetricMaintenanceInterrupted] = 1
	c := find(svc.Check(context.Background()), "concurrency")
	require.Equal(t, StatusWarn, c.Status)
	require.Contains(t, c.Value, "3")
	require.Contains(t, c.Value, "2500ms")
	require.Contains(t, c.Detail, "瓶颈")
}

func TestHealthMaintenanceCursorRunning(t *testing.T) {
	svc, _, _, cur, _ := newHealthy(t)
	cur.c.LastStatus = domain.CursorStatusRunning
	c := find(svc.Check(context.Background()), "maintenance_cursor")
	require.Equal(t, StatusWarn, c.Status, "未跑完 = 可续跑，应告警")
	require.Contains(t, c.Value, "1250")

	cur.c = nil // never ran
	c = find(svc.Check(context.Background()), "maintenance_cursor")
	require.Equal(t, StatusWarn, c.Status)
	require.Equal(t, "未运行", c.Value)
}

func TestHealthMetricReadErrorDegrades(t *testing.T) {
	svc, _, met, _, _ := newHealthy(t)
	met.err = errors.New("table locked")
	r := svc.Check(context.Background())
	// A single item failing does not interrupt the whole — the rest still reach
	// their conclusions
	require.Equal(t, StatusError, r.Status)
	require.Equal(t, StatusError, find(r, "last_backup").Status)
	require.Equal(t, StatusError, find(r, "concurrency").Status)
	// Unaffected items stay ok
	require.Equal(t, StatusOK, find(r, "schema_version").Status)
	require.Equal(t, StatusOK, find(r, "database").Status)
}

func TestHealthFilesystemUsage(t *testing.T) {
	svc, _, _, _, dir := newHealthy(t)
	c := find(svc.Check(context.Background()), "filesystem")
	require.Equal(t, StatusOK, c.Status)
	require.Contains(t, c.Value, "KB", "数据目录有 dummy.db，应报出体积")
	_ = dir
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:                    "512 B",
		1024:                   "1.0 KB",
		48 * 1024 * 1024:       "48.0 MB",
		5 * 1024 * 1024 * 1024: "5.0 GB",
	}
	for in, want := range cases {
		require.Equal(t, want, HumanBytes(in), "HumanBytes(%d)", in)
	}
}

func (m *fakeOpsRepo) WithTx(tx domain.Tx) domain.OpsRepository { return m }

func (m *fakeCursorRepo) WithTx(tx domain.Tx) domain.MaintenanceCursorRepository { return m }

func (m *fakeMetricRepo) WithTx(tx domain.Tx) domain.SystemMetricsRepository { return m }
