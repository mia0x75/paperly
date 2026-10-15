// Package metrics holds the operational metric names and the narrow recording
// interface shared across the service, maintenance, and backup layers.
//
// Names are defined once here, so business code references these constants
// rather than writing SQL or building metric-name strings.
//
// counter and gauge have different semantics and must not be mixed (the
// is_gauge semantic contract): counter accumulates, gauge overwrites. Wrong
// semantics will not error, only silently produce absurd numbers like "backup
// ran 1 time today", so the two APIs are named separately rather than collapsed
// into a single Set(name, value).
package metrics

import (
	"context"

	"paperly/internal/domain"
)

// Metric names.
//
// Existing metrics reference the same constants in domain rather than copying
// the literal: writing "backup_success" in two places would eventually
// misspell one character, and a misspelled metric name does not error — it
// just silently has no data forever, which is harder to chase than a crash.
const (
	// -- counter (is_gauge=0, accumulates for the day) --

	// MetricParseFail recent_window parse failure.
	//
	// The name in code predates the design doc: domain already recorded data
	// as "recent_window_parse_fail", and renaming would make old failure counts
	// undiscoverable, so the code name is kept.
	MetricParseFail = domain.MetricRecentWindowParseFail
	// Backup success count.
	MetricBackupSuccess = domain.MetricBackupSuccess
	// Backup failure count.
	MetricBackupFail = domain.MetricBackupFail
	// Daily maintenance completion count.
	MetricMaintenanceSuccess = "maintenance_success"
	// Daily maintenance failure count.
	MetricMaintenanceFail = "maintenance_fail"

	// -- gauge (is_gauge=1, overwrite with the day's snapshot) --

	// MetricDBSizeKB main DB file size (KB).
	MetricDBSizeKB = "db_size_kb"
	// MetricAssetsSizeKB assets directory usage (KB).
	MetricAssetsSizeKB = "assets_size_kb"
	// MetricFSFreeKB free space on the data directory's filesystem (KB).
	MetricFSFreeKB = "fs_free_kb"
	// MetricFSTotalKB total capacity of the data directory's filesystem (KB).
	MetricFSTotalKB = "fs_total_kb"
)

// Repo is the narrow interface needed for recording (a subset of
// domain.SystemMetricsRepository).
//
// A subset rather than the whole interface: this lets upper-layer tests inject
// a fake implementation without bringing up a real DB.
type Repo interface {
	// Inc increments today's counter of the same name.
	Inc(ctx context.Context, name string) error
	// SetGauge overwrites today's gauge with value.
	SetGauge(ctx context.Context, name string, value int64) error
}
