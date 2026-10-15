package service

import (
	"context"
	"fmt"
	"time"

	"paperly/internal/domain"
	"paperly/internal/health"
	"paperly/internal/metrics"
)

// LastBackupAtFunc returns the most recent backup time, with ok=false when no
// backup has ever been made.
//
// main.go injects the backup service's method directly (the signatures match). The
// metrics layer does not import the backup package — the homepage needs only a
// timestamp, not the whole backup flow.
type LastBackupAtFunc func(ctx context.Context) (time.Time, bool, error)

// HealthChecker runs a system health check.
//
// Declared in service rather than domain because health.Report belongs to the health
// package, and domain cannot depend on another internal package (that would form a
// cycle with health). *health.Service satisfies it.
type HealthChecker interface {
	Check(ctx context.Context) *health.Report
}

// MetricsService aggregates the "ground-truth" part of the homepage cards.
//
// Only the database / storage / system-health cards get their data from the backend.
type MetricsService struct {
	db domain.DatabaseStatsRepository
	// system_metrics read interface (gauge history series), injected separately
	// from the repo's counting interface so the homepage cards are not handed write
	// capability such as Inc/SetGauge.
	system  domain.SystemMetricsRepository
	health  HealthChecker
	lastAt  LastBackupAtFunc
	dataDir string
}

func NewMetricsService(
	db domain.DatabaseStatsRepository,
	system domain.SystemMetricsRepository,
	healthChecker HealthChecker,
	lastAt LastBackupAtFunc,
	dataDir string,
) *MetricsService {
	return &MetricsService{db: db, system: system, health: healthChecker, lastAt: lastAt, dataDir: dataDir}
}

// DatabaseInfo is the raw data for the database card (formatting happens in the DTO layer).
type DatabaseInfo struct {
	SizeBytes int64
	// LastBackupAt is only meaningful when HasBackup=true.
	LastBackupAt time.Time
	HasBackup    bool
	// Schema version label shown in the database card.
	SchemaTag string
}

func (s *MetricsService) DatabaseInfo(ctx context.Context) (DatabaseInfo, error) {
	size, err := s.db.DBSize(ctx)
	if err != nil {
		return DatabaseInfo{}, fmt.Errorf("database size: %w", err)
	}
	tag, err := s.db.SchemaDisplayTag(ctx)
	if err != nil {
		return DatabaseInfo{}, fmt.Errorf("schema display tag: %w", err)
	}
	info := DatabaseInfo{SizeBytes: size, SchemaTag: tag}
	if s.lastAt != nil {
		at, ok, err := s.lastAt(ctx)
		if err != nil {
			return DatabaseInfo{}, fmt.Errorf("last backup at: %w", err)
		}
		info.LastBackupAt, info.HasBackup = at, ok
	}
	return info, nil
}

// Storage returns the storage card data: assets usage plus the usage rate of the
// filesystem hosting the data directory.
func (s *MetricsService) Storage() health.StorageUsage {
	return health.MeasureStorage(s.dataDir)
}

// HealthReport runs a health check (system-health card data source).
//
// Reuses health.Service instead of adding a second implementation of the same
// metrics: two implementations could disagree, leaving parents unsure which to trust.
func (s *MetricsService) HealthReport(ctx context.Context) *health.Report {
	return s.health.Check(ctx)
}

// StorageTrendPoint is one data point of the storage trend: a snapshot of two gauges
// for a given day.
//
// A nil value means the metric was not sampled (no row for that day) — the caller
// renders a gap and must not fill 0, since unsampled is not the same as zero.
type StorageTrendPoint struct {
	// Date is "2006-01-02" in local time.
	Date string
	// DBKB is the db_size_kb snapshot for the day; nil when unsampled.
	DBKB *int64
	// AssetsKB is the assets_size_kb snapshot for the day; nil when unsampled.
	AssetsKB *int64
}

// StorageTrend returns storage snapshots for the last days days (including today),
// ascending by date, without filling gaps.
//
// The source is the daily-maintenance gauge rows (retained for 365 days), which answer
// "growth trend" rather than "current value" — the live value is already available
// via /metrics/summary and the settings page.
func (s *MetricsService) StorageTrend(ctx context.Context, days int) ([]StorageTrendPoint, error) {
	rows, err := s.system.GaugeSeries(ctx, []string{
		metrics.MetricDBSizeKB,
		metrics.MetricAssetsSizeKB,
	}, days)
	if err != nil {
		return nil, fmt.Errorf("storage trend: %w", err)
	}

	// Long table → wide table pivot. The repo returns rows ascending by metric_date;
	// points are placed in first-appearance order, and a second metric on the same day
	// fills into the existing row. The key is the calendar day: columns are DATETIME
	// but only day granularity is used.
	points := make([]StorageTrendPoint, 0, len(rows))
	byDate := make(map[string]int, len(rows))
	for _, row := range rows {
		day := row.Date.Format("2006-01-02")
		idx, ok := byDate[day]
		if !ok {
			idx = len(points)
			byDate[day] = idx
			points = append(points, StorageTrendPoint{Date: day})
		}
		v := row.Value
		switch row.Name {
		case metrics.MetricDBSizeKB:
			points[idx].DBKB = &v
		case metrics.MetricAssetsSizeKB:
			points[idx].AssetsKB = &v
		}
	}
	return points, nil
}
