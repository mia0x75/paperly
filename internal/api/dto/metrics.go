package dto

import (
	"paperly/internal/health"
	"paperly/internal/service"
)

// StorageCardSummary is the home-page "storage" card.
//
// AssetsBytes and DiskUsedRatio are **deliberately two different quantities** — do not merge them
// into a single "storage used" figure: the former is how much the parents' exams occupy, the latter
// is how much of the whole disk is used.
type StorageCardSummary struct {
	// AssetsBytes bytes of the assets directory (exam originals).
	AssetsBytes int64 `json:"assets_bytes"`
	// AssetsHuman pre-formatted, e.g. "12.4 MB".
	AssetsHuman string `json:"assets_human"`
	// DiskTotal / DiskFree capacity of the filesystem holding the data directory.
	DiskTotal uint64 `json:"disk_total"`
	DiskFree  uint64 `json:"disk_free"`
	// DiskUsedRatio 0..1; 0 when DiskSupported=false (platform does not support statfs).
	DiskUsedRatio float64 `json:"disk_used_ratio"`
	DiskSupported bool    `json:"disk_supported"`
}

// DatabaseCardSummary is the home-page "database" card.
type DatabaseCardSummary struct {
	// SizeBytes bytes of the main database file.
	SizeBytes int64 `json:"size_bytes"`
	// SizeHuman pre-formatted, e.g. "8.2 MB".
	SizeHuman string `json:"size_human"`
	// LastBackupAt "2006-01-02 15:04:05"; empty string = never backed up
	// (not 1970, so the frontend can show "never backed up").
	LastBackupAt string `json:"last_backup_at"`
	// SchemaTag schema version display tag, e.g. "v1.0.0".
	SchemaTag string `json:"schema_tag"`
}

// HealthItemSummary is one row of the system health card (from health.Report's 11 items).
type HealthItemSummary struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Value  string `json:"value"`
	Detail string `json:"detail"`
}

// StorageTrendPointSummary is one data point of the storage-growth trend line chart.
//
// db_size_kb / assets_size_kb being null means no sample was taken that day: the frontend renders
// a gap rather than a 0 (a 0 would punch a fake valley into the trend line,
// more misleading than a gap).
type StorageTrendPointSummary struct {
	// Date "2006-01-02" (local timezone, consistent with the gauge write scope).
	Date string `json:"date"`
	// DBSizeKB daily snapshot of the main DB size (main file only, excludes -wal/-shm).
	DBSizeKB *int64 `json:"db_size_kb"`
	// AssetsSizeKB daily snapshot of the assets directory size.
	AssetsSizeKB *int64 `json:"assets_size_kb"`
}

// StorageTrendResponse is the response body for GET /api/metrics/storage-trend.
type StorageTrendResponse struct {
	// Days the actual query window (days after clamping, not the raw input).
	Days int `json:"days"`
	// Points ascending by metric_date; missing days do not appear (the gauge is
	// written only on maintenance days).
	Points []StorageTrendPointSummary `json:"points"`
}

// MetricsSummaryResponse is the response body for GET /api/metrics/summary.
//
// Contains the truth cards that have a data source: database / storage / system health.
type MetricsSummaryResponse struct {
	Database DatabaseCardSummary `json:"database"`
	Storage  StorageCardSummary  `json:"storage"`
	Health   []HealthItemSummary `json:"health"`
	// CheckedAt sampling time of the health items (local timezone).
	CheckedAt string `json:"checked_at"`
}

func NewMetricsSummaryResponse(
	db service.DatabaseInfo,
	storage health.StorageUsage,
	report *health.Report,
) MetricsSummaryResponse {
	items := make([]HealthItemSummary, 0, len(report.Checks))
	for _, c := range report.Checks {
		items = append(items, HealthItemSummary{
			Key: c.Key, Title: c.Title, Status: string(c.Status), Value: c.Value, Detail: c.Detail,
		})
	}
	return MetricsSummaryResponse{
		Database:  NewDatabaseCardSummary(db),
		Storage:   NewStorageCardSummary(storage),
		Health:    items,
		CheckedAt: report.CheckedAt.Local().Format("2006-01-02 15:04:05"),
	}
}

func NewDatabaseCardSummary(db service.DatabaseInfo) DatabaseCardSummary {
	out := DatabaseCardSummary{
		SizeBytes: db.SizeBytes,
		SizeHuman: health.HumanBytes(db.SizeBytes),
		SchemaTag: db.SchemaTag,
	}
	if db.HasBackup && !db.LastBackupAt.IsZero() {
		out.LastBackupAt = db.LastBackupAt.Local().Format("2006-01-02 15:04:05")
	}
	return out
}

func NewStorageCardSummary(s health.StorageUsage) StorageCardSummary {
	return StorageCardSummary{
		AssetsBytes:   s.AssetsBytes,
		AssetsHuman:   s.HumanAssets(),
		DiskTotal:     s.DiskTotal,
		DiskFree:      s.DiskFree,
		DiskUsedRatio: s.DiskUsedRatio(),
		DiskSupported: s.DiskSupported,
	}
}
