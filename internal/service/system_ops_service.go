package service

import (
	"context"
	"fmt"

	"paperly/internal/domain"
)

// SystemOpsService serves read-only queries for the database structure version and task cursors.
//
// Why a dedicated service: these two data sources live in two separate repositories (ops / maintenance_cursor),
// but on the settings page they form a single "migrations and cursors" block. Folding them into AdminService
// would make the ops layer depend on backup, health, and two repositories at once; a separate service leaves
// AdminHandler needing only one extra field.
//
// Entirely read-only: running migrations and advancing cursors are the Worker's responsibility,
// and the settings page only displays them. No "run migration manually" button is provided —
// migrations must be executed by the startup flow under write-pause + full backup.
type SystemOpsService struct {
	ops     domain.OpsRepository
	cursors domain.MaintenanceCursorRepository
}

// NewSystemOpsService constructs the system ops query service.
func NewSystemOpsService(ops domain.OpsRepository, cursors domain.MaintenanceCursorRepository) *SystemOpsService {
	return &SystemOpsService{ops: ops, cursors: cursors}
}

// SchemaMigrations returns the applied migration records (version DESC).
func (s *SystemOpsService) SchemaMigrations(ctx context.Context) ([]*domain.SchemaMigration, error) {
	rows, err := s.ops.ListSchemaMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list schema migrations: %w", err)
	}
	return rows, nil
}

// Cursors returns all task cursors (in task_name dictionary order).
func (s *SystemOpsService) Cursors(ctx context.Context) ([]*domain.MaintenanceCursor, error) {
	rows, err := s.cursors.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list cursors: %w", err)
	}
	return rows, nil
}
