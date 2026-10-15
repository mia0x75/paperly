package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	"paperly/internal/health"
)

type stubDB struct {
	size    int64
	tag     string
	sizeErr error
	tagErr  error
}

func (s *stubDB) DBSize(context.Context) (int64, error)            { return s.size, s.sizeErr }
func (s *stubDB) SchemaDisplayTag(context.Context) (string, error) { return s.tag, s.tagErr }

type stubHealth struct{ report *health.Report }

func (s stubHealth) Check(context.Context) *health.Report { return s.report }

// stubSystemMetrics implements domain.SystemMetricsRepository: only GaugeSeries matters,
// the other write/count methods are irrelevant to these cases and return zero values.
type stubSystemMetrics struct {
	rows    []domain.GaugePoint
	err     error
	got     []string // records the received metric names, verifying no unrelated gauge is requested
	gotDays int
}

func (s *stubSystemMetrics) Inc(context.Context, string) error             { return nil }
func (s *stubSystemMetrics) SetMax(context.Context, string, int) error     { return nil }
func (s *stubSystemMetrics) SetGauge(context.Context, string, int64) error { return nil }
func (s *stubSystemMetrics) SumSince(context.Context, string, int) (int, error) {
	return 0, nil
}
func (s *stubSystemMetrics) MaxSince(context.Context, string, int) (int, error) {
	return 0, nil
}
func (s *stubSystemMetrics) LatestAt(context.Context, string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}
func (s *stubSystemMetrics) DeleteOlderThan(context.Context, string) (int64, error) {
	return 0, nil
}
func (s *stubSystemMetrics) DeleteGaugeOlderThan(context.Context, string) (int64, error) {
	return 0, nil
}
func (s *stubSystemMetrics) GaugeSeries(_ context.Context, names []string, days int) ([]domain.GaugePoint, error) {
	s.got, s.gotDays = names, days
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}

func newTestMetrics(db *stubDB, lastAt LastBackupAtFunc) *MetricsService {
	return NewMetricsService(db, &stubSystemMetrics{}, stubHealth{report: &health.Report{}}, lastAt, "")
}

func TestDatabaseInfoWrapsErrors(t *testing.T) {
	boom := errors.New("boom")
	svc := newTestMetrics(&stubDB{sizeErr: boom}, nil)

	_, err := svc.DatabaseInfo(context.Background())
	require.ErrorIs(t, err, boom)
	require.Contains(t, err.Error(), "database size", "错误要说明是哪一步失败")
}

func TestDatabaseInfoNoBackupFuncYieldsEmpty(t *testing.T) {
	// With no lastAt injected (backup service unavailable) it must not crash, just have no backup time.
	svc := newTestMetrics(&stubDB{size: 10, tag: "v1.0.0"}, nil)

	info, err := svc.DatabaseInfo(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 10, info.SizeBytes)
	require.Equal(t, "v1.0.0", info.SchemaTag)
	require.False(t, info.HasBackup)
	require.True(t, info.LastBackupAt.IsZero())
}

func TestDatabaseInfoPropagatesBackupError(t *testing.T) {
	boom := errors.New("backup boom")
	svc := newTestMetrics(&stubDB{}, func(context.Context) (time.Time, bool, error) {
		return time.Time{}, false, boom
	})

	_, err := svc.DatabaseInfo(context.Background())
	require.ErrorIs(t, err, boom)
	require.Contains(t, err.Error(), "last backup at")
}

// StorageTrend: long table pivoted to wide, unsampled metrics stay nil, two metrics on the same day merge into one row,
// and only the two volume gauges are requested (not fs_* or counters).
func TestStorageTrendPivotsAndKeepsMissingNil(t *testing.T) {
	sys := &stubSystemMetrics{rows: []domain.GaugePoint{
		{Date: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), Name: "db_size_kb", Value: 8192},
		{Date: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), Name: "assets_size_kb", Value: 1048576},
		{Date: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), Name: "assets_size_kb", Value: 1049600}, // db unsampled
	}}
	svc := NewMetricsService(&stubDB{}, sys, stubHealth{report: &health.Report{}}, nil, "")

	points, err := svc.StorageTrend(context.Background(), 30)
	require.NoError(t, err)
	require.Len(t, points, 2, "按日 pivot，不应按指标数膨胀")
	require.Equal(t, "2026-10-06", points[0].Date)
	require.NotNil(t, points[0].DBKB)
	require.EqualValues(t, 8192, *points[0].DBKB)
	require.NotNil(t, points[0].AssetsKB)
	require.Equal(t, "2026-10-07", points[1].Date)
	require.Nil(t, points[1].DBKB, "缺采必须是 nil 而非 0——0 会把趋势图砸出假谷底")
	require.NotNil(t, points[1].AssetsKB)

	require.ElementsMatch(t, []string{"db_size_kb", "assets_size_kb"}, sys.got,
		"只请求两个体量 gauge")
	require.Equal(t, 30, sys.gotDays, "窗口天数透传")
}

// The repository error must be propagated (the handler maps it to 500); it must not be swallowed into an empty chart.
func TestStorageTrendWrapsRepoError(t *testing.T) {
	sys := &stubSystemMetrics{err: errors.New("db locked")}
	svc := NewMetricsService(&stubDB{}, sys, stubHealth{report: &health.Report{}}, nil, "")

	_, err := svc.StorageTrend(context.Background(), 30)
	require.Error(t, err)
	require.ErrorContains(t, err, "db locked")
	require.ErrorContains(t, err, "storage trend", "错误应带业务上下文")
}

func (m *stubSystemMetrics) WithTx(tx domain.Tx) domain.SystemMetricsRepository { return m }
