package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// fakeOpsRepo records calls and can inject failures.
type fakeOpsRepo struct {
	vacuumErr     error
	verifyResult  string
	verifyErr     error
	vacuumPaths   []string
	optimizeCalls int
}

func (f *fakeOpsRepo) VacuumInto(_ context.Context, dest string) error {
	f.vacuumPaths = append(f.vacuumPaths, dest)
	if f.vacuumErr != nil {
		return f.vacuumErr
	}
	// Simulate VACUUM INTO actually producing an openable SQLite file.
	return os.WriteFile(dest, []byte("SQLite format 3\x00fake"), 0o600)
}

func (f *fakeOpsRepo) Optimize(context.Context) error { f.optimizeCalls++; return nil }

func (f *fakeOpsRepo) IntegrityCheck(context.Context) (string, error) { return "ok", nil }

func (f *fakeOpsRepo) VerifyFileIntegrity(context.Context, string) (string, error) {
	if f.verifyErr != nil {
		return "", f.verifyErr
	}
	if f.verifyResult == "" {
		return "ok", nil
	}
	return f.verifyResult, nil
}

func (f *fakeOpsRepo) DBSize(context.Context) (int64, error) { return 1 << 20, nil }
func (f *fakeOpsRepo) ListSchemaMigrations(context.Context) ([]*domain.SchemaMigration, error) {
	return []*domain.SchemaMigration{}, nil
}

func (f *fakeOpsRepo) SchemaDisplayTag(context.Context) (string, error) {
	return "v1.0.0", nil
}

// fakeMetricRepo records the Inc/SetMax sequence.
type fakeMetricRepo struct {
	incs   []string
	maxSet map[string]int
	latest time.Time
	found  bool
	sums   map[string]int
	err    error
	incErr error
}

// count returns how many times a metric was Inc'd.
func (f *fakeMetricRepo) count(name string) int {
	n := 0
	for _, m := range f.incs {
		if m == name {
			n++
		}
	}
	return n
}

func newFakeMetricRepo() *fakeMetricRepo {
	return &fakeMetricRepo{maxSet: map[string]int{}, sums: map[string]int{}}
}

func (f *fakeMetricRepo) Inc(_ context.Context, name string) error {
	f.incs = append(f.incs, name)
	return f.incErr
}

func (f *fakeMetricRepo) SetMax(_ context.Context, name string, n int) error {
	if n > f.maxSet[name] {
		f.maxSet[name] = n
	}
	return nil
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
	return f.maxSet[name], nil
}

func (f *fakeMetricRepo) LatestAt(_ context.Context, name string) (time.Time, bool, error) {
	if f.err != nil {
		return time.Time{}, false, f.err
	}
	if name == domain.MetricBackupLastAt {
		return f.latest, f.found, nil
	}
	return time.Time{}, false, nil
}

func (f *fakeMetricRepo) SetGauge(context.Context, string, int64) error { return nil }

func (f *fakeMetricRepo) DeleteOlderThan(context.Context, string) (int64, error) { return 0, nil }

func (f *fakeMetricRepo) DeleteGaugeOlderThan(context.Context, string) (int64, error) { return 0, nil }
func (f *fakeMetricRepo) GaugeSeries(context.Context, []string, int) ([]domain.GaugePoint, error) {
	return nil, nil
}

// newTestService builds a backup service with a fixed time.
func newTestService(t *testing.T, now time.Time) (*Service, *fakeOpsRepo, *fakeMetricRepo, string) {
	t.Helper()
	ops := &fakeOpsRepo{}
	met := newFakeMetricRepo()
	dir := t.TempDir()
	svc := NewService(ops, met, dir)
	svc.nowFn = func() time.Time { return now }
	return svc, ops, met, dir
}

func TestBackupCreateHappyPath(t *testing.T) {
	now := time.Date(2026, 9, 28, 3, 0, 0, 0, time.UTC) // 2026-09-28 is Monday
	svc, ops, met, dir := newTestService(t, now)

	m, pruned, err := svc.Create(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, pruned)
	require.Equal(t, string(tierWeekly), m.Tier)
	require.Equal(t, "20260928_030000", m.Timestamp)
	require.Equal(t, "ok", m.DBIntegrity)
	require.Greater(t, m.DBBytes, int64(0))

	require.FileExists(t, filepath.Join(dir, "backup", "weekly", "paperly_20260928_030000.db"))
	require.FileExists(t, filepath.Join(dir, "backup", "weekly", "paperly_20260928_030000.manifest.json"))
	require.Len(t, ops.vacuumPaths, 1)

	require.Contains(t, met.incs, domain.MetricBackupSuccess)
	require.Equal(t, 1, met.maxSet[domain.MetricBackupLastAt], "备份成功要写 last_at 标记")

	blob, err := os.ReadFile(filepath.Join(dir, "backup", "weekly", "paperly_20260928_030000.manifest.json"))
	require.NoError(t, err)
	var back Manifest
	require.NoError(t, json.Unmarshal(blob, &back))
	require.Equal(t, m.Timestamp, back.Timestamp)
}

func TestBackupCreateTierRouting(t *testing.T) {
	cases := []struct {
		name string
		when time.Time
		want tier
	}{
		{"月初归 monthly", time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC), tierMonthly},
		{"周一归 weekly", time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC), tierWeekly},
		{"平日归 daily", time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC), tierDaily},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, tierFor(c.when))
		})
	}
	// The 1st falling on a Monday -> goes to monthly (keeps the most copies),
	// so weekly is not left empty from never being assigned.
	require.Equal(t, tierMonthly, tierFor(time.Date(2026, 6, 1, 3, 0, 0, 0, time.UTC)))
}

func TestBackupCreateVacuumFailRecordsFail(t *testing.T) {
	svc, ops, met, dir := newTestService(t, time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC))
	ops.vacuumErr = errors.New("disk full")

	_, _, err := svc.Create(context.Background())
	require.Error(t, err)
	require.Contains(t, met.incs, domain.MetricBackupFail)
	require.NotContains(t, met.incs, domain.MetricBackupSuccess)
	require.NoFileExists(t, filepath.Join(dir, "backup", "daily", "paperly_20260910_030000.db"))
}

func TestBackupCreateIntegrityFailDeletesBadFile(t *testing.T) {
	svc, ops, met, dir := newTestService(t, time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC))
	ops.verifyResult = "corrupt page 42"

	_, _, err := svc.Create(context.Background())
	require.ErrorContains(t, err, "corrupt page 42")
	require.Contains(t, met.incs, domain.MetricBackupFail)
	// A bad backup must be deleted immediately; it must not linger in the
	// retention directory pretending to be a valid backup.
	require.NoFileExists(t, filepath.Join(dir, "backup", "daily", "paperly_20260910_030000.db"))
}

func TestBackupCreateVerifyErrDeletesFile(t *testing.T) {
	svc, ops, met, dir := newTestService(t, time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC))
	ops.verifyErr = errors.New("cannot open")

	_, _, err := svc.Create(context.Background())
	require.Error(t, err)
	require.Contains(t, met.incs, domain.MetricBackupFail)
	require.NoFileExists(t, filepath.Join(dir, "backup", "daily", "paperly_20260910_030000.db"))
}

func TestBackupRetentionKeepsTierLimits(t *testing.T) {
	// 2026-09-10 is Thursday -> daily tier
	now := time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC)
	svc, _, met, dir := newTestService(t, now)

	// Create 10 historical backups (file names with increasing timestamps)
	dailyDir := filepath.Join(dir, "backup", "daily")
	require.NoError(t, os.MkdirAll(dailyDir, 0o750))
	for i := 1; i <= 10; i++ {
		ts := now.AddDate(0, 0, -i).Format(backupTimestampLayout)
		require.NoError(t, os.WriteFile(filepath.Join(dailyDir, "paperly_"+ts+".db"), []byte("x"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dailyDir, "paperly_"+ts+".manifest.json"), []byte("{}"), 0o600))
	}

	// The 11th backup triggers the retention policy
	_, pruned, err := svc.Create(context.Background())
	require.NoError(t, err)
	require.Equal(t, 4, pruned, "11 份 - 保留 7 份 = 删 4 份")

	names, err := listBackupFiles(dailyDir)
	require.NoError(t, err)
	require.Len(t, names, DailyKeep, "保留上限 %d 生效", DailyKeep)
	require.Contains(t, met.incs, "backup_pruned")

	// The manifest of a pruned backup must be cleared too, leaving no orphan
	var manifests int
	entries, err := os.ReadDir(dailyDir)
	require.NoError(t, err)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			manifests++
		}
	}
	require.Equal(t, DailyKeep, manifests, "manifest 数应与备份数一致")
}

func TestBackupRetentionTiersIndependent(t *testing.T) {
	now := time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC)
	svc, _, _, dir := newTestService(t, now)

	// Create 3 daily (under 7, kept) and 6 weekly (over 4, prune 2)
	for tierName, n := range map[string]int{"daily": 3, "weekly": 6} {
		d := filepath.Join(dir, "backup", tierName)
		require.NoError(t, os.MkdirAll(d, 0o750))
		for i := 1; i <= n; i++ {
			ts := now.AddDate(0, 0, -i).Format(backupTimestampLayout)
			require.NoError(t, os.WriteFile(filepath.Join(d, "paperly_"+ts+".db"), []byte("x"), 0o600))
		}
	}

	_, pruned, err := svc.Create(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, pruned, "只 weekly 超限")

	weekly, err := listBackupFiles(filepath.Join(dir, "backup", "weekly"))
	require.NoError(t, err)
	require.Len(t, weekly, WeeklyKeep)
	daily, err := listBackupFiles(filepath.Join(dir, "backup", "daily"))
	require.NoError(t, err)
	require.Len(t, daily, 4, "daily 3+1=4 未超 7 不该删")
}

func TestBackupListCrossTierSortedByTime(t *testing.T) {
	now := time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC)
	svc, _, _, dir := newTestService(t, now)

	// Create one daily (newer) and one weekly (older)
	dailyDir := filepath.Join(dir, "backup", "daily")
	weeklyDir := filepath.Join(dir, "backup", "weekly")
	require.NoError(t, os.MkdirAll(dailyDir, 0o750))
	require.NoError(t, os.MkdirAll(weeklyDir, 0o750))
	newer := now.Format(backupTimestampLayout)
	older := now.AddDate(0, 0, -3).Format(backupTimestampLayout)
	require.NoError(t, os.WriteFile(filepath.Join(dailyDir, "paperly_"+newer+".db"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(weeklyDir, "paperly_"+older+".db"), []byte("x"), 0o600))

	items, err := svc.List()
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, newer, items[0].Timestamp, "按时间戳统一降序，不按层级分组")
	require.Equal(t, string(tierDaily), items[0].Tier)
}

func TestBackupListEmptyNoError(t *testing.T) {
	svc, _, _, _ := newTestService(t, time.Now())
	items, err := svc.List()
	require.NoError(t, err)
	require.Empty(t, items, "无备份目录应返回空而非报错")
}

func TestBackupLastBackupAt(t *testing.T) {
	svc, _, met, _ := newTestService(t, time.Now())
	met.found = true
	met.latest = time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
	ts, found, err := svc.LastBackupAt(context.Background())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 2026, ts.Year())

	met.found = false
	_, found, err = svc.LastBackupAt(context.Background())
	require.NoError(t, err)
	require.False(t, found)
}

func TestBackupCopiesAssets(t *testing.T) {
	now := time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC)
	svc, _, _, dir := newTestService(t, now)

	// Create the assets directory (exam page images)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets", "abc"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "abc", "p1.png"), []byte("PNG"), 0o600))

	m, _, err := svc.Create(context.Background())
	require.NoError(t, err)
	require.Greater(t, m.AssetsBytes, int64(0), "assets 应被复制")

	ts := now.Format(backupTimestampLayout)
	require.FileExists(t, filepath.Join(dir, "backup", "daily", "assets_"+ts, "abc", "p1.png"))
}

func TestBackupCopiesMissingAssetsIsZero(t *testing.T) {
	now := time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC)
	svc, _, _, _ := newTestService(t, now)
	// A missing source directory is normal (no exams uploaded yet) -> 0 bytes, no
	// error
	m, _, err := svc.Create(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), m.AssetsBytes)
}

// TestCreateNoPartialBackupOnAssetsFailure builds a failure with an "assets
// directory exists but is unreadable" setup.
func TestCreateNoPartialBackupOnAssetsFailure(t *testing.T) {
	dir := t.TempDir()
	ops, met := &fakeOpsRepo{}, newFakeMetricRepo()
	svc := NewService(ops, met, dir)
	// 2026-09-27 is Sunday -> tier=daily (tierFor only switches on Monday/1st)
	svc.nowFn = func() time.Time { return time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC) }

	// The assets source dir must exist, otherwise copyDirInto takes the "no
	// exams uploaded yet" path and simply returns 0.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "a.png"), []byte("png"), 0o640))

	// Make copyDirInto's MkdirAll fail for sure: the target name is taken by a
	// regular file. Use a file rather than permission bits to construct the
	// failure, because tests may run as root and chmod cannot stop root.
	bdir := filepath.Join(dir, "backup", "daily")
	require.NoError(t, os.MkdirAll(bdir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(bdir, "assets_20260927_100000"), []byte("x"), 0o640))

	_, _, err := svc.Create(context.Background())
	require.Error(t, err)

	// Key: the backup directory must not keep any artifact from this run
	entries, rerr := os.ReadDir(bdir)
	require.NoError(t, rerr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), "20260927_100000",
			"失败时残留 %s：没有 manifest 的半成品会被 List() 当成有效备份", e.Name())
	}
	require.Equal(t, 1, met.count(domain.MetricBackupFail), "失败必须记 backup_fail")
}

// TestCreateNoPartialBackupOnManifestFailure must also clean up when writing
// the manifest fails.
func TestCreateNoPartialBackupOnManifestFailure(t *testing.T) {
	dir := t.TempDir()
	ops, met := &fakeOpsRepo{}, newFakeMetricRepo()
	svc := NewService(ops, met, dir)
	svc.nowFn = func() time.Time { return time.Date(2026, 9, 27, 11, 0, 0, 0, time.UTC) }

	// The same-named .manifest.json is taken by a directory -> WriteFile will
	// fail with EISDIR
	bdir := filepath.Join(dir, "backup", "daily")
	require.NoError(t, os.MkdirAll(filepath.Join(bdir, "paperly_20260927_110000.manifest.json"), 0o750))

	_, _, err := svc.Create(context.Background())
	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(bdir, "paperly_20260927_110000.db"))
	require.True(t, os.IsNotExist(statErr), "manifest 写失败时 .db 也必须被清掉")
	require.Equal(t, 1, met.count(domain.MetricBackupFail))
}

// TestCreateSuccessDespiteMetricWriteFailure a broken metrics table must not
// report a successful backup as failed.
func TestCreateSuccessDespiteMetricWriteFailure(t *testing.T) {
	dir := t.TempDir()
	ops := &fakeOpsRepo{}
	met := newFakeMetricRepo()
	met.incErr = errors.New("system_metrics table is locked")
	svc := NewService(ops, met, dir)
	svc.nowFn = func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }

	m, pruned, err := svc.Create(context.Background())
	require.NoError(t, err, "备份已落盘且校验通过，不能因为指标写失败报 500")
	require.NotNil(t, m)
	require.Equal(t, 0, pruned)
	require.Equal(t, "ok", m.DBIntegrity)
}

func (m *fakeOpsRepo) WithTx(tx domain.Tx) domain.OpsRepository { return m }

func (m *fakeMetricRepo) WithTx(tx domain.Tx) domain.SystemMetricsRepository { return m }
