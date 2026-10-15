package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// ymd returns the local "2006-01-02" date for daysAgo days ago (today when
// daysAgo=0). This matches the write side's date('now','localtime'), so window
// boundaries in tests are real boundaries.
func ymd(daysAgo int) string {
	return time.Now().AddDate(0, 0, -daysAgo).Format("2006-01-02")
}

// insertGaugeAt inserts a gauge row with an explicit date (SetGauge only writes today,
// so historical rows must hand-write the date to test window boundaries).
func insertGaugeAt(t *testing.T, db *DB, date, name string, value int64, isGauge int) {
	t.Helper()
	_, err := db.Write().ExecContext(context.Background(), `
INSERT INTO metrics (metric_date, metric_name, count, is_gauge, updated_at)
VALUES (?, ?, ?, ?, ?)`, date, name, value, isGauge, date+" 03:00:00")
	require.NoError(t, err)
}

// One query backs four read-side contracts of the trend chart: windowed filter,
// ascending order, no backfill of missing days, and only the requested gauges.
func TestGaugeSeriesWindowAscAndFilters(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewSystemMetricRepoSQLite(db)

	insertGaugeAt(t, db, ymd(0), "db_size_kb", 8192, 1)
	insertGaugeAt(t, db, ymd(2), "db_size_kb", 8000, 1)
	insertGaugeAt(t, db, ymd(2), "assets_size_kb", 1048576, 1)
	// Outside the window: with days=3 (cutoff = today-2), a row 4 days ago must be cut off.
	insertGaugeAt(t, db, ymd(4), "db_size_kb", 7000, 1)
	// counter row: same name/table but is_gauge=0, must not mix into the trend.
	insertGaugeAt(t, db, ymd(1), "demo_fail", 3, 0)
	// Unrelated gauge: not named in the request, not returned.
	insertGaugeAt(t, db, ymd(1), "fs_free_kb", 999, 1)

	points, err := repo.GaugeSeries(ctx, []string{"db_size_kb", "assets_size_kb"}, 3)
	require.NoError(t, err)

	require.Len(t, points, 3, "窗口内 2 个有数据日 × 2 指标，缺采日不补行")
	// Ascending: earliest day first, today last.
	require.Equal(t, ymd(2), points[0].Date.Format("2006-01-02"), "升序排列，最早日在前")
	require.Equal(t, ymd(0), points[len(points)-1].Date.Format("2006-01-02"))
	for i := 1; i < len(points); i++ {
		require.LessOrEqual(t, points[i-1].Date.Unix(), points[i].Date.Unix(), "metric_date 必须升序")
	}
	for _, p := range points {
		require.NotEqual(t, "demo_fail", p.Name, "counter 不得混入 gauge 序列")
		require.NotEqual(t, "fs_free_kb", p.Name, "未点名的 gauge 不得返回")
	}

	// Direct evidence of no backfill: only fs_free_kb (filtered out) existed 1 day ago,
	// so that whole day is absent rather than appearing with zero values.
	var dates []string
	for _, p := range points {
		dates = append(dates, p.Date.Format("2006-01-02")+"/"+p.Name)
	}
	require.NotContains(t, dates, ymd(1)+"/db_size_kb", "缺采日不得补 0 行")
}

// Empty names or days<=0 short-circuit to an empty set without issuing SQL.
func TestGaugeSeriesEmptyInput(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewSystemMetricRepoSQLite(db)
	insertGaugeAt(t, db, ymd(0), "db_size_kb", 8192, 1)

	empty, err := repo.GaugeSeries(ctx, nil, 30)
	require.NoError(t, err)
	require.Empty(t, empty, "空 names 应短路返回")

	empty, err = repo.GaugeSeries(ctx, []string{"db_size_kb"}, 0)
	require.NoError(t, err)
	require.Empty(t, empty, "days<=0 应短路返回")
}

// days=1 takes only today. The window lower bound includes today, so a -0
// day must not drop it.
func TestGaugeSeriesSingleDayWindow(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewSystemMetricRepoSQLite(db)

	insertGaugeAt(t, db, ymd(0), "db_size_kb", 8192, 1)
	insertGaugeAt(t, db, ymd(1), "db_size_kb", 8000, 1)

	points, err := repo.GaugeSeries(ctx, []string{"db_size_kb"}, 1)
	require.NoError(t, err)
	require.Len(t, points, 1, "days=1 应只含今天")
	require.Equal(t, ymd(0), points[0].Date.Format("2006-01-02"))
	require.EqualValues(t, 8192, points[0].Value)
}

// Compile-time check that the interface is fully implemented.
var _ domain.SystemMetricsRepository = (*SystemMetricRepoSQLite)(nil)
