package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	"paperly/internal/health"
	"paperly/internal/service"
)

// fakeDBStats implements domain.DatabaseStatsRepository.
type fakeDBStats struct {
	size    int64
	tag     string
	sizeErr error
}

func (f *fakeDBStats) DBSize(context.Context) (int64, error) { return f.size, f.sizeErr }
func (f *fakeDBStats) SchemaDisplayTag(context.Context) (string, error) {
	return f.tag, nil
}

// fakeSystemMetrics implements domain.SystemMetricsRepository (used by storage-trend).
type fakeSystemMetrics struct {
	rows []domain.GaugePoint
	err  error
}

func (f *fakeSystemMetrics) Inc(context.Context, string) error             { return nil }
func (f *fakeSystemMetrics) SetMax(context.Context, string, int) error     { return nil }
func (f *fakeSystemMetrics) SetGauge(context.Context, string, int64) error { return nil }
func (f *fakeSystemMetrics) SumSince(context.Context, string, int) (int, error) {
	return 0, nil
}
func (f *fakeSystemMetrics) MaxSince(context.Context, string, int) (int, error) {
	return 0, nil
}
func (f *fakeSystemMetrics) LatestAt(context.Context, string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}
func (f *fakeSystemMetrics) DeleteOlderThan(context.Context, string) (int64, error) {
	return 0, nil
}
func (f *fakeSystemMetrics) DeleteGaugeOlderThan(context.Context, string) (int64, error) {
	return 0, nil
}
func (f *fakeSystemMetrics) GaugeSeries(context.Context, []string, int) ([]domain.GaugePoint, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

// identMW is a pass-through middleware: handler tests bypass the real gate, the audience is
// guaranteed by each test case; the gate is wired in main.go, which is wiring rather than
// handler behavior.
func identMW(next echo.HandlerFunc) echo.HandlerFunc { return next }

// staticHealth implements service.HealthChecker, returning a fixed report.
type staticHealth struct{ report *health.Report }

func (s staticHealth) Check(context.Context) *health.Report { return s.report }

func sampleHealthReport() *health.Report {
	return &health.Report{
		Status:    health.StatusOK,
		CheckedAt: time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC),
		Checks: []health.Check{
			{Key: "database", Title: "数据库", Status: health.StatusOK, Value: "8.2 MB"},
			{Key: "schema", Title: "数据版本", Status: health.StatusWarn, Value: "v1.0.0"},
		},
	}
}

func newMetricsTestServer(
	db *fakeDBStats,
	lastAt service.LastBackupAtFunc,
) *echo.Echo {
	svc := service.NewMetricsService(db, &fakeSystemMetrics{}, staticHealth{report: sampleHealthReport()}, lastAt, "")
	e := echo.New()
	NewMetricsHandler(svc, identMW, identMW).RegisterRoutes(e.Group("/api"))
	return e
}

func neverBackedUp(context.Context) (time.Time, bool, error) { return time.Time{}, false, nil }

func TestMetricsSummaryReturnsTruthfulCards(t *testing.T) {
	backedUp := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	e := newMetricsTestServer(
		&fakeDBStats{size: 2048, tag: "v1.0.0"},
		func(context.Context) (time.Time, bool, error) { return backedUp, true, nil },
	)

	code, body := getJSON(t, e, "/api/metrics/summary")
	require.Equal(t, http.StatusOK, code)
	require.EqualValues(t, 0, body["code"])

	data, ok := body["data"].(map[string]any)
	require.True(t, ok)

	db, ok := data["database"].(map[string]any)
	require.True(t, ok, "数据库卡")
	require.EqualValues(t, 2048, db["size_bytes"])
	require.Equal(t, "2.0 KB", db["size_human"])
	// The time is shown to the parent in local timezone; assert with the same conversion
	// so the test is not tied to a specific TZ.
	require.Equal(t, backedUp.Local().Format("2006-01-02 15:04:05"), db["last_backup_at"])
	require.Equal(t, "v1.0.0", db["schema_tag"])

	storage, ok := data["storage"].(map[string]any)
	require.True(t, ok, "存储卡")
	require.Equal(t, "0 B", storage["assets_human"])

	items, ok := data["health"].([]any)
	require.True(t, ok, "体检行")
	require.Len(t, items, 2, "health 透传全部检查项")
	require.NotEmpty(t, data["checked_at"])
}

// Never backed up → last_backup_at is an empty string (not 1970), so the frontend can show
// "never backed up".
func TestMetricsSummaryNeverBackedUp(t *testing.T) {
	e := newMetricsTestServer(&fakeDBStats{}, neverBackedUp)

	_, body := getJSON(t, e, "/api/metrics/summary")
	data, _ := body["data"].(map[string]any)
	db, ok := data["database"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "", db["last_backup_at"])
}

func TestMetricsSummaryDBFailureIs500(t *testing.T) {
	e := newMetricsTestServer(
		&fakeDBStats{sizeErr: context.Canceled}, neverBackedUp)

	code, body := getJSON(t, e, "/api/metrics/summary")
	require.Equal(t, http.StatusInternalServerError, code)
	require.EqualValues(t, CodeInternal, body["code"])
}

// When the platform does not support statfs, the frontend hides the usage rate via
// disk_supported=false; the flag must be passed through unchanged and must not become true
// just because DiskTotal=0.
func TestStorageCardPropagatesUnsupportedFlag(t *testing.T) {
	e := newMetricsTestServer(&fakeDBStats{}, neverBackedUp)

	_, body := getJSON(t, e, "/api/metrics/summary")
	data, _ := body["data"].(map[string]any)
	storage, ok := data["storage"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, storage["disk_supported"])
}

func newStorageTrendServer(rows []domain.GaugePoint) *echo.Echo {
	svc := service.NewMetricsService(
		&fakeDBStats{},
		&fakeSystemMetrics{rows: rows},
		staticHealth{report: sampleHealthReport()},
		neverBackedUp,
		"",
	)
	e := echo.New()
	NewMetricsHandler(svc, identMW, identMW).RegisterRoutes(e.Group("/api"))
	return e
}

// Normal case: dates ascending, both fields present, envelope shape correct (the frontend
// toLineSeries contract).
func TestStorageTrendReturnsAscendingPointsWithNullForMissing(t *testing.T) {
	assets := int64(1048576)
	e := newStorageTrendServer([]domain.GaugePoint{
		{Date: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), Name: "assets_size_kb", Value: assets},
		{Date: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), Name: "db_size_kb", Value: 8192},
		{Date: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), Name: "assets_size_kb", Value: 1048576},
		// 10-08 has no db_size_kb row → the frontend must receive null (gap), not 0.
	})

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/storage-trend?days=30", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var env struct {
		Code int `json:"code"`
		Data struct {
			Days   int `json:"days"`
			Points []struct {
				Date         string `json:"date"`
				DBSizeKB     *int64 `json:"db_size_kb"`
				AssetsSizeKB *int64 `json:"assets_size_kb"`
			} `json:"points"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, 0, env.Code, "成功信封 code=0")
	require.Equal(t, 30, env.Data.Days)
	require.Len(t, env.Data.Points, 2, "按日聚合成行")

	require.Equal(t, "2026-10-07", env.Data.Points[0].Date, "升序")
	require.NotNil(t, env.Data.Points[0].DBSizeKB)
	require.EqualValues(t, 8192, *env.Data.Points[0].DBSizeKB)
	require.NotNil(t, env.Data.Points[0].AssetsSizeKB)

	require.Equal(t, "2026-10-08", env.Data.Points[1].Date)
	require.Nil(t, env.Data.Points[1].DBSizeKB, "缺采字段必须序列化为 null，绝不能是 0")
	require.NotNil(t, env.Data.Points[1].AssetsSizeKB)
}

// days parameter: default 30, invalid returns 30, out-of-range clamps into
// [1, MetricGaugeRetentionDays].
func TestStorageTrendDaysClamp(t *testing.T) {
	e := newStorageTrendServer(nil)
	cases := []struct {
		raw  string // raw query; empty means omitted
		want int
	}{
		{"", 30},
		{"7", 7},
		{"0", 1},      // lower bound clamps to 1 (days=0 would push cutoff past today)
		{"-5", 1},     // negative likewise
		{"9999", 365}, // gauge only retains 365 days; there is no data beyond that
		{"abc", 30},   // non-integer falls back to default
	}
	for _, tc := range cases {
		target := "/api/metrics/storage-trend"
		if tc.raw != "" {
			target += "?days=" + tc.raw
		}
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "days=%q 应始终 200（钳制而非拒绝）", tc.raw)

		var env struct {
			Data struct {
				Days int `json:"days"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
		require.Equal(t, tc.want, env.Data.Days, "days=%q", tc.raw)
	}
}

// Repo failure maps to 500, not leaked as a 200 empty chart (an empty chart would be read
// by the frontend as "no data").
func TestStorageTrendRepoErrorMapsInternal(t *testing.T) {
	svc := service.NewMetricsService(
		&fakeDBStats{},
		&fakeSystemMetrics{err: errors.New("db locked")},
		staticHealth{report: sampleHealthReport()},
		neverBackedUp,
		"",
	)
	e := echo.New()
	NewMetricsHandler(svc, identMW, identMW).RegisterRoutes(e.Group("/api"))

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/storage-trend", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), `"points":[]`, "故障不得伪装成空数据")
}

func (m *fakeSystemMetrics) WithTx(tx domain.Tx) domain.SystemMetricsRepository { return m }
