package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/jmoiron/sqlx"

	"paperly/internal/domain"
)

type OpsRepoSQLite struct {
	baseRepo
}

func NewOpsRepoSQLite(db *DB) *OpsRepoSQLite {
	return &OpsRepoSQLite{baseRepo: baseRepo{db: db}}
}

// WithTx returns a shallow copy bound to tx; its statements then run inside that transaction.
func (r *OpsRepoSQLite) WithTx(tx any) domain.OpsRepository {
	return &OpsRepoSQLite{baseRepo: r.mustBindTx(tx)}
}

// Uses the write pool: VACUUM needs a write lock and must be serialized with other writes
// (AGENTS.md write-queue model).
//
// VACUUM INTO accepts a literal file name, not a bound parameter (SQLite limitation), so the
// path must be spliced into SQL. We reject only the SQL string terminator (') and NUL, not all
// special characters: paths like `C:\Program Files\paperly` are valid and must stay backupable.
func (r *OpsRepoSQLite) VacuumInto(ctx context.Context, destPath string) error {
	if err := validateBackupPath(destPath); err != nil {
		return err
	}
	// VACUUM INTO refuses to overwrite, so clear any leftover (e.g. a half-written artifact).
	if err := os.Remove(destPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale backup target: %w", err)
	}
	//nolint:gosec // Path validated: contains no single quote (the only char that can close an SQL string literal) and no NUL.
	if _, err := r.writeExt().ExecContext(ctx, `VACUUM INTO '`+destPath+`'`); err != nil {
		return fmt.Errorf("vacuum into %s: %w", destPath, err)
	}
	return nil
}

// PRAGMA optimize (SQLite's recommended lightweight statistics update).
func (r *OpsRepoSQLite) Optimize(ctx context.Context) error {
	//nolint:gosec // Fixed statement, no variable splicing.
	if _, err := r.writeExt().ExecContext(ctx, `PRAGMA optimize`); err != nil {
		return fmt.Errorf("pragma optimize: %w", err)
	}
	return nil
}

// quick_check on the main database (health check item 1).
func (r *OpsRepoSQLite) IntegrityCheck(ctx context.Context) (string, error) {
	var result string
	if err := r.readExt().QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		return "", fmt.Errorf("quick check main db: %w", err)
	}
	return result, nil
}

// Opens the backup file as a standalone, read-only SQLite database and runs a full
// integrity_check (backup step 2). Separate connection: it does not touch the main pool.
func (r *OpsRepoSQLite) VerifyFileIntegrity(ctx context.Context, path string) (string, error) {
	return verifyFileIntegrity(ctx, path)
}

func verifyFileIntegrity(ctx context.Context, path string) (string, error) {
	if err := validateBackupPath(path); err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("stat backup file: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&mode=ro", url.PathEscape(path), busyTimeoutMS)
	conn, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return "", fmt.Errorf("open backup file: %w", err)
	}
	defer func() { _ = conn.Close() }()

	var result string
	if err := conn.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return "", fmt.Errorf("integrity check backup file: %w", err)
	}
	return result, nil
}

// Main DB file size only (health check item 1); excludes -wal / -shm side files, which may
// still hold un-checkpointed pages. The health page needs only a rough size, so we skip the
// extra checkpoint, which would block writes.
func (r *OpsRepoSQLite) DBSize(ctx context.Context) (int64, error) {
	info, err := os.Stat(r.db.Path())
	if err != nil {
		return 0, fmt.Errorf("stat database file: %w", err)
	}
	return info.Size(), nil
}

// The outer COALESCE over a subquery is needed because COALESCE(display_tag,'unknown')
// alone cannot rescue a 0-row result set; an empty table still yields "unknown".
func (r *OpsRepoSQLite) SchemaDisplayTag(ctx context.Context) (string, error) {
	const q = `SELECT COALESCE((SELECT display_tag FROM migrations ORDER BY version DESC LIMIT 1), 'unknown')`
	var displayTag string
	if err := r.readExt().QueryRowContext(ctx, q).Scan(&displayTag); err != nil {
		return "unknown", fmt.Errorf("probe schema display_tag: %w", err)
	}
	return displayTag, nil
}

// Returns applied version records (version DESC). Uses the read pool.
func (r *OpsRepoSQLite) ListSchemaMigrations(ctx context.Context) ([]*domain.SchemaMigration, error) {
	var out []*domain.SchemaMigration
	if err := r.readExt().SelectContext(ctx, &out,
		`SELECT version, name, display_tag, applied_at FROM migrations ORDER BY version DESC`); err != nil {
		return nil, fmt.Errorf("list schema migrations: %w", err)
	}
	if out == nil {
		out = make([]*domain.SchemaMigration, 0)
	}
	return out, nil
}

// VACUUM INTO's target path cannot be a bound parameter (SQLite limitation), so it must be
// spliced into SQL text. The only criterion that matters is whether the path can close the
// SQL string literal early — the single quote. Semicolons, parentheses, backslashes, and
// newlines are harmless inside a literal.
//
// Deliberately not a strict "[A-Za-z0-9_./-]" allowlist: that would block backup for installs
// under paths like `C:\Program Files\Paperly\data`, which are common on Windows. NUL is a
// separate case: it cannot appear in a SQLite path, so its presence signals an upstream bug.
func validateBackupPath(path string) error {
	if path == "" {
		return errors.New("backup path is empty")
	}
	if strings.ContainsRune(path, '\'') {
		return fmt.Errorf("backup path contains single quote, which would break SQL literal: %q", path)
	}
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("backup path contains NUL byte: %q", path)
	}
	// Newline/carriage-return must not enter the SQL text: even if they do not affect the
	// statement structure, they would deform the logs and manifest.
	if strings.ContainsAny(path, "\n\r") {
		return fmt.Errorf("backup path contains newline: %q", path)
	}
	return nil
}

var (
	_ domain.OpsRepository               = (*OpsRepoSQLite)(nil)
	_ domain.SystemMetricsRepository     = (*SystemMetricRepoSQLite)(nil)
	_ domain.MaintenanceCursorRepository = (*MaintenanceCursorRepoSQLite)(nil)
)
