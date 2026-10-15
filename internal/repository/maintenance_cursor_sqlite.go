package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"paperly/internal/domain"
)

// MaintenanceCursorRepoSQLite implements domain.MaintenanceCursorRepository on SQLite.
type MaintenanceCursorRepoSQLite struct {
	baseRepo
}

func NewMaintenanceCursorRepoSQLite(db *DB) *MaintenanceCursorRepoSQLite {
	return &MaintenanceCursorRepoSQLite{baseRepo: baseRepo{db: db}}
}

// WithTx returns a shallow copy bound to tx; its statements then run inside that
// transaction.
func (r *MaintenanceCursorRepoSQLite) WithTx(tx any) domain.MaintenanceCursorRepository {
	return &MaintenanceCursorRepoSQLite{baseRepo: r.mustBindTx(tx)}
}

// Get returns domain.ErrNotFound if the cursor is absent; the service supplies a default
// on first run.
func (r *MaintenanceCursorRepoSQLite) Get(ctx context.Context, taskName string) (*domain.MaintenanceCursor, error) {
	var c domain.MaintenanceCursor
	err := r.readExt().GetContext(ctx, &c, `
SELECT id, task_name, last_id, last_run_at, last_status, updated_at
FROM cursors WHERE task_name = ?`, taskName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get maintenance cursor %s: %w", taskName, err)
	}
	return &c, nil
}

// Upsert inserts or updates the row by task_name.
//
// last_run_at is passed through as-is so NULL stays NULL: DailyMaintenance writes a
// timestamp every batch, and the struct zero value would otherwise store
// "0001-01-01 00:00:00" and pollute the health page.
func (r *MaintenanceCursorRepoSQLite) Upsert(ctx context.Context, c *domain.MaintenanceCursor) error {
	_, err := r.writeExt().ExecContext(ctx, `
INSERT INTO cursors (task_name, last_id, last_run_at, last_status, updated_at)
VALUES (?, ?, ?, ?, datetime('now'))
ON CONFLICT(task_name) DO UPDATE SET
    last_id     = excluded.last_id,
    last_run_at = excluded.last_run_at,
    last_status = excluded.last_status,
    updated_at  = excluded.updated_at`,
		c.TaskName, c.LastID, c.LastRunAt, c.LastStatus)
	if err != nil {
		return fmt.Errorf("upsert maintenance cursor %s: %w", c.TaskName, err)
	}
	return nil
}

// List returns all cursors ordered by task_name, for the health page.
func (r *MaintenanceCursorRepoSQLite) List(ctx context.Context) ([]*domain.MaintenanceCursor, error) {
	var out []*domain.MaintenanceCursor
	err := r.readExt().SelectContext(ctx, &out, `
SELECT id, task_name, last_id, last_run_at, last_status, updated_at
FROM cursors ORDER BY task_name`)
	if err != nil {
		return nil, fmt.Errorf("list maintenance cursors: %w", err)
	}
	return out, nil
}
