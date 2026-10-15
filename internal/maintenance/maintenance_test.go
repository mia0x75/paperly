package maintenance

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	"paperly/internal/metrics"
)

// discardSlog is a silent logger for tests.
func discardSlog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeMetricsRepo is an in-memory Repo: it records gauge writes, result counts,
// and cleanup calls.
type fakeMetricsRepo struct {
	gauges     map[string]int64
	incs       []string
	delCounter []string
	delGauge   []string
	err        error
}

func (f *fakeMetricsRepo) SetGauge(_ context.Context, name string, value int64) error {
	if f.gauges == nil {
		f.gauges = map[string]int64{}
	}
	f.gauges[name] = value
	return nil
}

func (f *fakeMetricsRepo) Inc(_ context.Context, name string) error {
	f.incs = append(f.incs, name)
	return nil
}

func (f *fakeMetricsRepo) DeleteOlderThan(_ context.Context, cutoff string) (int64, error) {
	f.delCounter = append(f.delCounter, cutoff)
	if f.err != nil {
		return 0, f.err
	}
	return 2, nil
}

func (f *fakeMetricsRepo) DeleteGaugeOlderThan(_ context.Context, cutoff string) (int64, error) {
	f.delGauge = append(f.delGauge, cutoff)
	return 0, nil
}

// fakeDBLocator returns a fixed main DB byte count.
type fakeDBLocator struct{ size int64 }

func (f *fakeDBLocator) DBSize(context.Context) (int64, error) { return f.size, nil }

// fakeTaskStore is an in-memory TaskStore: it records cleanup and collects enqueues.
type fakeTaskStore struct {
	clearedDays int
	inserted    []*domain.Task
	err         error
}

func (f *fakeTaskStore) ClearOlderThan(_ context.Context, days int) (int64, error) {
	f.clearedDays = days
	if f.err != nil {
		return 0, f.err
	}
	return 1, nil
}

func (f *fakeTaskStore) Insert(_ context.Context, t *domain.Task) (int64, error) {
	f.inserted = append(f.inserted, t)
	return int64(len(f.inserted)), nil
}

// fakeKidRoster is an in-memory KidRoster.
type fakeKidRoster struct {
	kids      []*domain.Kid
	gotFilter domain.ListKidsFilter
}

func (f *fakeKidRoster) List(_ context.Context, filter domain.ListKidsFilter) ([]*domain.Kid, error) {
	f.gotFilter = filter
	out := make([]*domain.Kid, 0, len(f.kids))
	for _, k := range f.kids {
		if filter.Status != 0 && k.Status != filter.Status {
			continue
		}
		out = append(out, k)
	}
	return out, nil
}

// fakeOptimizer records how many times PRAGMA optimize was invoked.
type fakeOptimizer struct {
	calls int
	err   error
}

func (f *fakeOptimizer) Optimize(context.Context) error {
	f.calls++
	return f.err
}

// newService assembles a Service with minimal dependencies (temp dataDir + fixed clock).
func newService(now time.Time) (*Service, *fakeMetricsRepo, *fakeTaskStore, *fakeKidRoster) {
	repo := &fakeMetricsRepo{}
	tasks := &fakeTaskStore{}
	kids := &fakeKidRoster{}
	optimizer := &fakeOptimizer{}
	dir, err := os.MkdirTemp("", "maintenance-test")
	if err != nil {
		panic(err) // test infrastructure only
	}
	svc := NewService(repo, &fakeDBLocator{size: 4096}, tasks, kids, optimizer, dir, discardSlog())
	svc.WithClock(func() time.Time { return now })
	return svc, repo, tasks, kids
}

func TestServiceRunNonSep1NoEnqueue(t *testing.T) {
	svc, repo, tasks, _ := newService(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))

	err := svc.Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, 30, tasks.clearedDays, "按常量 30 天清理")
	// Every day enqueues exactly one backup task (kind=3), regardless of the date.
	require.Len(t, tasks.inserted, 1, "每日入队 1 条 backup")
	require.Equal(t, domain.TaskKindBackup, tasks.inserted[0].Kind)
	require.Equal(t, backupEnqueuePriority, tasks.inserted[0].Priority)
	require.Equal(t, domain.TaskStatusPending, tasks.inserted[0].Status)
	require.JSONEq(t, `{}`, tasks.inserted[0].Detail)
	require.Contains(t, repo.incs, metrics.MetricMaintenanceSuccess)
	require.NotContains(t, repo.incs, metrics.MetricMaintenanceFail)
	for _, m := range []string{
		metrics.MetricDBSizeKB, metrics.MetricAssetsSizeKB,
		metrics.MetricFSFreeKB, metrics.MetricFSTotalKB,
	} {
		require.Contains(t, repo.gauges, m, "应打 %s 快照", m)
	}
}

func TestServiceRunSep1EnqueuesGradeAdvance(t *testing.T) {
	svc, repo, tasks, kids := newService(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	kids.kids = []*domain.Kid{
		{ID: 7, Grade: 7, Status: domain.KidStatusActive},
		{ID: 8, Grade: 12, Status: domain.KidStatusActive},
		{ID: 9, Grade: 7, Status: domain.KidStatusGraduated}, // not active, not enqueued
	}

	err := svc.Run(context.Background())
	require.NoError(t, err)

	require.Equal(t, domain.KidStatusActive, kids.gotFilter.Status, "只捞在读学生")
	require.Len(t, tasks.inserted, 3, "2 条年级推进 + 1 条 backup")

	// Grade-advance tasks are enqueued first; the backup task is appended last.
	for i, want := range []struct {
		kidID, enrYear, grade, target string
	}{
		{kidID: "7", enrYear: "2026", grade: "7", target: "8"},
		{kidID: "8", enrYear: "2026", grade: "12", target: "13"},
	} {
		it := tasks.inserted[i]
		require.Equal(t, domain.TaskKindAdvanceGrade, it.Kind)
		require.Equal(t, taskEnqueuePriority, it.Priority)
		require.Equal(t, domain.TaskStatusPending, it.Status)
		require.JSONEq(t,
			`{"kid_id":`+want.kidID+`,"enrolled_year":`+want.enrYear+
				`,"enrolled_grade":`+want.grade+`,"target_grade":`+want.target+`}`,
			it.Detail)
	}
	require.Equal(t, domain.TaskKindBackup, tasks.inserted[2].Kind, "末尾补一条 backup")
	require.Contains(t, repo.incs, metrics.MetricMaintenanceSuccess)
}

func TestServiceRunNotSep1LeavesEnqueueEmpty(t *testing.T) {
	svc, _, tasks, kids := newService(time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC))
	kids.kids = []*domain.Kid{{ID: 7, Grade: 7, Status: domain.KidStatusActive}}

	err := svc.Run(context.Background())
	require.NoError(t, err)
	require.Len(t, tasks.inserted, 1, "非入学日仅入队 backup")
	require.Equal(t, domain.TaskKindBackup, tasks.inserted[0].Kind)
	require.Zero(t, kids.gotFilter.Status, "非入学日不查名单")
}

func TestServiceRunFailureIncrementsFail(t *testing.T) {
	svc, repo, tasks, _ := newService(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))
	tasks.err = errors.New("db unavailable")

	err := svc.Run(context.Background())
	require.Error(t, err)
	require.Contains(t, repo.incs, metrics.MetricMaintenanceFail)
	require.NotContains(t, repo.incs, metrics.MetricMaintenanceSuccess)
}

// TestServiceRunOptimizes: a successful pass runs PRAGMA optimize exactly once.
func TestServiceRunOptimizes(t *testing.T) {
	svc, _, _, _ := newService(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))
	opt := svc.optimizer.(*fakeOptimizer)

	require.NoError(t, svc.Run(context.Background()))
	require.Equal(t, 1, opt.calls, "每日维护应执行一次 PRAGMA optimize")
}

// TestServiceRunOptimizeFailureIsSoft: a PRAGMA optimize failure only logs and
// must not fail the whole maintenance pass.
func TestServiceRunOptimizeFailureIsSoft(t *testing.T) {
	svc, repo, _, _ := newService(time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))
	svc.optimizer.(*fakeOptimizer).err = errors.New("optimize boom")

	require.NoError(t, svc.Run(context.Background()), "optimize 失败不应让维护整体失败")
	require.Contains(t, repo.incs, metrics.MetricMaintenanceSuccess)
}

// TestEnqueueGradeAdvanceIsResilientPerKid: a single child's enqueue failure does
// not block the rest.
func TestEnqueueGradeAdvanceIsResilientPerKid(t *testing.T) {
	_, _, _, kids := newService(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	kids.kids = []*domain.Kid{
		{ID: 7, Grade: 7, Status: domain.KidStatusActive},
		{ID: 8, Grade: 8, Status: domain.KidStatusActive},
	}

	svc := &Service{
		clock: func() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) },
		kids:  kids,
		tasks: &flakyInsertStore{okAt: 2},
		log:   discardSlog(),
	}
	err := svc.enqueueGradeAdvance(context.Background())
	require.NoError(t, err, "单个入队失败不阻断整体")
}

// flakyInsertStore succeeds only on the okAt-th Insert; the rest fail.
type flakyInsertStore struct {
	okAt int
	n    int
}

func (f *flakyInsertStore) ClearOlderThan(context.Context, int) (int64, error) {
	return 0, errors.New("cleanup should not be reached")
}

func (f *flakyInsertStore) Insert(_ context.Context, t *domain.Task) (int64, error) {
	f.n++
	if f.n == f.okAt {
		return int64(f.n), nil
	}
	return 0, errors.New("insert failed")
}

// TestServiceRunDoubleRunIsHarmless: repeating Run on the same day does not error
// (not deduplicating enqueues is a design choice, with idempotency caught by the
// consumer-side gate). This test confirms the repeated-execution path does not
// blow up and result counting stays normal.
func TestServiceRunDoubleRunIsHarmless(t *testing.T) {
	svc, repo, tasks, kids := newService(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	kids.kids = []*domain.Kid{{ID: 7, Grade: 7, Status: domain.KidStatusActive}}

	require.NoError(t, svc.Run(context.Background()))
	require.Len(t, tasks.inserted, 2, "每次 Run：1 条年级推进 + 1 条 backup")
	require.NoError(t, svc.Run(context.Background()))
	require.Len(t, tasks.inserted, 4, "重复 Run 不去重（幂等由消费方保证）")

	require.Equal(t, 2, countStr(repo.incs, metrics.MetricMaintenanceSuccess))
}

func countStr(xs []string, want string) int {
	n := 0
	for _, x := range xs {
		if x == want {
			n++
		}
	}
	return n
}
