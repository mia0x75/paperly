package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"paperly/internal/domain"
)

// All writes use date('now','localtime') so daily aggregation aligns with the parent's local
// "today". Using UTC would shift 0:00–8:00 (UTC+8) into the previous day and misalign
// counter/gauge partitions.
type SystemMetricRepoSQLite struct {
	baseRepo
}

func NewSystemMetricRepoSQLite(db *DB) *SystemMetricRepoSQLite {
	return &SystemMetricRepoSQLite{baseRepo: baseRepo{db: db}}
}

func (r *SystemMetricRepoSQLite) WithTx(tx any) domain.SystemMetricsRepository {
	return &SystemMetricRepoSQLite{baseRepo: r.mustBindTx(tx)}
}

// Upsert; increments same-day counter by 1 (persists across process restarts).
func (r *SystemMetricRepoSQLite) Inc(ctx context.Context, name string) error {
	_, err := r.writeExt().ExecContext(ctx, `
INSERT INTO metrics (metric_name, metric_date, count, is_gauge, updated_at)
VALUES (?, date('now','localtime'), 1, 0, datetime('now','localtime'))
ON CONFLICT(metric_name, metric_date) DO UPDATE
SET count = count + 1, updated_at = excluded.updated_at`, name)
	if err != nil {
		return fmt.Errorf("bump system metric %s: %w", name, err)
	}
	return nil
}

// Upsert; sets same-day counter to max(current, n) (e.g. write-queue wait time).
func (r *SystemMetricRepoSQLite) SetMax(ctx context.Context, name string, n int) error {
	_, err := r.writeExt().ExecContext(ctx, `
INSERT INTO metrics (metric_name, metric_date, count, is_gauge, updated_at)
VALUES (?, date('now','localtime'), ?, 0, datetime('now','localtime'))
ON CONFLICT(metric_name, metric_date) DO UPDATE
SET count = MAX(count, excluded.count), updated_at = excluded.updated_at`, name, n)
	if err != nil {
		return fmt.Errorf("set max system metric %s: %w", name, err)
	}
	return nil
}

// Upsert as gauge (is_gauge=1); overwrites same-day value with excluded.count (not additive)
// so re-running maintenance does not accumulate.
func (r *SystemMetricRepoSQLite) SetGauge(ctx context.Context, name string, value int64) error {
	_, err := r.writeExt().ExecContext(ctx, `
INSERT INTO metrics (metric_name, metric_date, count, is_gauge, updated_at)
VALUES (?, date('now','localtime'), ?, 1, datetime('now','localtime'))
ON CONFLICT(metric_name, metric_date) DO UPDATE
SET count = excluded.count, updated_at = excluded.updated_at`, name, value)
	if err != nil {
		return fmt.Errorf("set gauge metric %s: %w", name, err)
	}
	return nil
}

// Totals last days days; restricted to is_gauge=0 (excludes size snapshots).
func (r *SystemMetricRepoSQLite) SumSince(ctx context.Context, name string, days int) (int, error) {
	var total sql.NullInt64
	err := r.readExt().QueryRowContext(ctx, `
SELECT COALESCE(SUM(count), 0) FROM metrics
WHERE metric_name = ? AND is_gauge = 0 AND metric_date >= date('now', ?)`,
		name, fmt.Sprintf("-%d day", days)).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum system metric %s: %w", name, err)
	}
	return int(total.Int64), nil
}

func (r *SystemMetricRepoSQLite) MaxSince(ctx context.Context, name string, days int) (int, error) {
	var mx sql.NullInt64
	err := r.readExt().QueryRowContext(ctx, `
SELECT COALESCE(MAX(count), 0) FROM metrics
WHERE metric_name = ? AND is_gauge = 0 AND metric_date >= date('now', ?)`,
		name, fmt.Sprintf("-%d day", days)).Scan(&mx)
	if err != nil {
		return 0, fmt.Errorf("max system metric %s: %w", name, err)
	}
	return int(mx.Int64), nil
}

// Reads updated_at (not metric_date) to preserve minute-level precision.
func (r *SystemMetricRepoSQLite) LatestAt(ctx context.Context, name string) (time.Time, bool, error) {
	var ts sql.NullString
	err := r.readExt().QueryRowContext(ctx, `
SELECT MAX(updated_at) FROM metrics WHERE metric_name = ?`, name).Scan(&ts)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("latest system metric %s: %w", name, err)
	}
	if !ts.Valid || ts.String == "" {
		return time.Time{}, false, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", ts.String, time.Local)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse metric timestamp %q: %w", ts.String, err)
	}
	return parsed, true, nil
}

// Cleans up expired counter rows (retention policy counter tier).
func (r *SystemMetricRepoSQLite) DeleteOlderThan(ctx context.Context, cutoff string) (int64, error) {
	return r.deleteOlderThan(ctx, cutoff, 0, "counter")
}

// Cleans up expired gauge rows (retention policy gauge tier).
func (r *SystemMetricRepoSQLite) DeleteGaugeOlderThan(ctx context.Context, cutoff string) (int64, error) {
	return r.deleteOlderThan(ctx, cutoff, 1, "gauge")
}

// Per-day gauge snapshots over the last days days (inclusive of today), ordered by date ASC.
// cutoff is computed in the local timezone on the Go side (same as maintenance.cleanup)
// because date('now','-N day') uses UTC and is misaligned with the write side's
// date('now','localtime') around local midnight. Empty names or days<=0 returns empty set.
func (r *SystemMetricRepoSQLite) GaugeSeries(ctx context.Context, names []string, days int) ([]domain.GaugePoint, error) {
	if len(names) == 0 || days <= 0 {
		return []domain.GaugePoint{}, nil
	}
	cutoff := time.Now().AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
	args := make([]any, 0, len(names)+1)
	for _, n := range names {
		args = append(args, n)
	}
	args = append(args, cutoff)

	out := make([]domain.GaugePoint, 0, days*len(names))
	err := r.readExt().SelectContext(ctx, &out, `
SELECT metric_date, metric_name, count
FROM metrics
WHERE is_gauge = 1 AND metric_name IN (`+placeholders+`) AND metric_date >= ?
ORDER BY metric_date ASC, metric_name ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("gauge series %v: %w", names, err)
	}
	return out, nil
}

func (r *SystemMetricRepoSQLite) deleteOlderThan(ctx context.Context, cutoff string, isGauge int, kind string) (int64, error) {
	res, err := r.writeExt().ExecContext(ctx,
		`DELETE FROM metrics WHERE metric_date < ? AND is_gauge = ?`, cutoff, isGauge)
	if err != nil {
		return 0, fmt.Errorf("delete expired %s metrics: %w", kind, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read deleted rows: %w", err)
	}
	return n, nil
}
