package domain

import (
	"context"
	"errors"
	"time"
)

// System-level ops types. SQL implementations live in repository/ops_sqlite.go and friends.

// Maintenance cursor status (last_status).
const (
	CursorStatusPending   = 0
	CursorStatusRunning   = 1
	CursorStatusCompleted = 2
	CursorStatusFailed    = 3
)

// CursorTaskOnboardingDone is the cursor task marking first-start onboarding completion.
//
// One-time marker: last_status=Completed means onboarding finished. Combined with the
// "users>=1 and kids>=1" check (see OnboardingService.Status), a data wipe still pulls the user
// back to onboarding instead of letting an empty-shell system run bare.
const CursorTaskOnboardingDone = "onboarding_done"

// System-level domain errors.
var (
	// ErrOnboardingCompleted means onboarding already reached its terminal state (marker exists,
	// users/kids non-empty); completing again is a replay, which the api layer maps to 409.
	ErrOnboardingCompleted = errors.New("onboarding already completed")
)

// system_metrics retention (tiered: counters 90 days, gauges 365 days).
const (
	// MetricRetentionDays is the retention for counter metrics.
	MetricRetentionDays = 90
	// MetricGaugeRetentionDays is the retention for gauge metrics. A year at daily granularity is
	// only 365 rows, and 90 days is too short to show a volume slope.
	MetricGaugeRetentionDays = 365
)

// system_metrics metric names (centralized to avoid typos across the codebase).
const (
	// recent_window parse failure (AC1, reported by the mastery chain).
	MetricRecentWindowParseFail = "recent_window_parse_fail"
	// successful backups.
	MetricBackupSuccess = "backup_success"
	// failed backups.
	MetricBackupFail = "backup_fail"
	// three-chain write version-conflict rollbacks.
	MetricWriteConflictRollback = "write_conflict_rollback"
	// max write-queue wait in ms (day's max; read via MaxSince).
	MetricWriteQueueMaxWaitMS = "write_queue_max_wait_ms"
	// daily maintenance interrupted by ingestion.
	MetricMaintenanceInterrupted = "maintenance_interrupted"
)

// DatabaseStatsRepository is the narrow query interface the home "database" card needs.
//
// Like PaperStatsCounter: the home page wants only the DB size and version tag, and must not hand
// the metrics layer side-effecting/heavy operations (VacuumInto, IntegrityCheck) to get them.
type DatabaseStatsRepository interface {
	// DBSize returns the main database file size in bytes.
	DBSize(ctx context.Context) (int64, error)
	// SchemaDisplayTag returns the display tag of the latest schema version.
	SchemaDisplayTag(ctx context.Context) (string, error)
}

// MetricBackupLastAt is the "last backup time" metric name.
//
// system_metrics keys on (metric_name, metric_date) and can't store a timestamp, but each row has
// updated_at. Convention: on a successful backup write a count=1 row for backup_last_at, so
// "last backup time" = MAX(updated_at) WHERE metric_name='backup_last_at' (minute-level precision,
// reusing the existing table).
const MetricBackupLastAt = "backup_last_at"

// MaintenanceCursor is the daily maintenance cursor.
type MaintenanceCursor struct {
	ID         int64      `db:"id"`
	TaskName   string     `db:"task_name"`
	LastID     int64      `db:"last_id"`
	LastRunAt  *time.Time `db:"last_run_at"`
	LastStatus int        `db:"last_status"`
	UpdatedAt  time.Time  `db:"updated_at"`
}

// SystemMetricsRepository reads/writes the system_metrics table.
//
// Writes upsert: counters accumulate, gauges overwrite. Reads aggregate (Sum/Max/Latest) for the
// three health-check needs: "last 7 days", "today's max", "most recent time".
//
// is_gauge: 0=counter (count=count+1, read SUM), 1=gauge (count=excluded.count, read that day's
// snapshot). SumSince/MaxSince skip gauge rows so gauges never pollute event counts.
type SystemMetricsRepository interface {
	// Inc adds 1 to today's counter for name.
	Inc(ctx context.Context, name string) error
	// SetMax sets today's counter to max(n, current) (for write-queue wait time).
	SetMax(ctx context.Context, name string, n int) error
	// SetGauge overwrites today's gauge with value.
	//
	// Overwrite, not accumulate: a rerun (manual trigger, retry) that kept accumulating would
	// double the volume and put a meaningless spike on the trend chart.
	SetGauge(ctx context.Context, name string, value int64) error
	// SumSince sums a counter over the last days (inclusive of today), 0 when no rows.
	SumSince(ctx context.Context, name string, days int) (int, error)
	// MaxSince returns a counter's max over the last days, 0 when no rows.
	MaxSince(ctx context.Context, name string, days int) (int, error)
	// LatestAt returns the metric's most recent write time, zero value + false when no rows.
	LatestAt(ctx context.Context, name string) (time.Time, bool, error)
	// DeleteOlderThan clears counter rows before cutoff, returning the deletion count.
	DeleteOlderThan(ctx context.Context, cutoff string) (int64, error)
	// DeleteGaugeOlderThan clears gauge rows before cutoff, returning the deletion count.
	DeleteGaugeOlderThan(ctx context.Context, cutoff string) (int64, error)
	// GaugeSeries returns per-day snapshots of the gauges over the last days (inclusive of today),
	// metric_date ascending. Missing days are not filled — the read side must not fill 0, since a
	// missing sample is not a volume of 0 (it would punch a fake trough into the chart).
	GaugeSeries(ctx context.Context, names []string, days int) ([]GaugePoint, error)
	// WithTx returns a new instance bound to tx.
	WithTx(tx Tx) SystemMetricsRepository
}

// GaugePoint is one per-day snapshot of a gauge (long-table: one row per day per metric, pivoted
// by the read side).
type GaugePoint struct {
	// Date is metric_date, stored at day granularity (date('now','localtime')); only the calendar
	// day is used.
	Date time.Time `db:"metric_date"`
	// Name is metric_name.
	Name string `db:"metric_name"`
	// Value is the day's snapshot (the count column).
	Value int64 `db:"count"`
}

// MaintenanceCursorRepository is the maintenance_cursor table interface.
type MaintenanceCursorRepository interface {
	// Get fetches the cursor; domain.ErrNotFound when missing (the caller supplies a first-run default).
	Get(ctx context.Context, taskName string) (*MaintenanceCursor, error)
	// Upsert inserts or updates by task_name.
	Upsert(ctx context.Context, c *MaintenanceCursor) error
	// List returns all cursors, ordered by task_name (for the health page).
	List(ctx context.Context) ([]*MaintenanceCursor, error)
	// WithTx returns a new instance bound to tx.
	WithTx(tx Tx) MaintenanceCursorRepository
}

// OpsRepository is the database-level ops interface.
//
// These are "library-level actions" with no entity ownership, hence a separate interface rather
// than a business repository — this also keeps arbitrary SQL execution out of the service layer.
type OpsRepository interface {
	// VacuumInto runs SQLite's VACUUM INTO for a consistent snapshot (atomic, no downtime).
	// destPath must not exist (VACUUM INTO refuses to overwrite).
	VacuumInto(ctx context.Context, destPath string) error
	// Optimize runs PRAGMA optimize (attached to daily maintenance).
	Optimize(ctx context.Context) error
	// IntegrityCheck runs PRAGMA quick_check on the main database; "ok" means pass.
	IntegrityCheck(ctx context.Context) (string, error)
	// VerifyFileIntegrity opens path independently and runs PRAGMA integrity_check (main DB untouched).
	VerifyFileIntegrity(ctx context.Context, path string) (string, error)
	// DBSize returns the main database file size in bytes.
	DBSize(ctx context.Context) (int64, error)
	// SchemaDisplayTag returns the display_tag of the latest migrations row.
	//
	// Empty-database fallback: a NULL value or 0 rows yields "unknown" via an outer COALESCE
	// (display_tag ships in the baseline migration, so there is no old-schema probe).
	SchemaDisplayTag(ctx context.Context) (string, error)
	// ListSchemaMigrations returns applied version records, version DESC.
	ListSchemaMigrations(ctx context.Context) ([]*SchemaMigration, error)
	// WithTx returns a new instance bound to tx.
	WithTx(tx Tx) OpsRepository
}

// SchemaMigration is one applied migration, corresponding to the migrations table.
type SchemaMigration struct {
	Version    int       `db:"version"`
	Name       string    `db:"name"`
	DisplayTag *string   `db:"display_tag"`
	AppliedAt  time.Time `db:"applied_at"`
}
