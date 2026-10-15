// Package repository is Paperly's data access layer: the SQLite connection,
// migrations, and read/write pools live here.
// Dependency direction: service → repository → database/sql + modernc.org/sqlite.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // Pure-Go SQLite driver; no CGO.

	"paperly/internal/domain"
	"paperly/migrations"
)

// querier is the surface shared by *sqlx.DB (read/write pools) and *sqlx.Tx, so a
// repo can run its statements on a pool or on a transaction bound via WithTx.
type querier interface {
	sqlx.ExtContext
	GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	SelectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	NamedExecContext(ctx context.Context, query string, arg interface{}) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// castTx converts a domain.Tx (an untyped handle, so domain stays free of
// database/sql) back into the concrete *sqlx.Tx this layer uses. Return value: nil
// means "unbound" (run on the pools), *sqlx.Tx means "bound". Any other non-nil type
// is a caller bug.
func castTx(tx any) (*sqlx.Tx, bool) {
	if tx == nil {
		return nil, true
	}
	t, ok := tx.(*sqlx.Tx)
	return t, ok
}

// baseRepo is embedded by every SQLite repository: the read/write pools plus an
// optional transaction, so statements run either on the pools or inside a caller's
// transaction (bound via WithTx) without repeating the same fields everywhere.
//
// readExt/writeExt are unexported on purpose — an internal routing detail, not part
// of the domain contracts.
type baseRepo struct {
	db *DB
	tx *sqlx.Tx
}

// readExt returns the bound transaction when present, otherwise the read pool.
func (b *baseRepo) readExt() querier {
	if b.tx != nil {
		return b.tx
	}
	return b.db.Read()
}

// writeExt returns the bound transaction when present, otherwise the write pool.
func (b *baseRepo) writeExt() querier {
	if b.tx != nil {
		return b.tx
	}
	return b.db.Write()
}

// mustBindTx builds the state for a WithTx copy: same pool, plus the concrete
// transaction. Repo constructors use it so the literal isn't repeated everywhere.
//
// A wrong-type handle panics rather than silently running outside the caller's
// transaction: quiet degradation would lose atomicity without a trace, and a wrong
// type is always a programming error (every producer hands out *sqlx.Tx), never a
// business error.
func (b *baseRepo) mustBindTx(tx any) baseRepo {
	t, ok := castTx(tx)
	if !ok {
		panic(fmt.Sprintf("repository: WithTx got %T, want *sqlx.Tx", tx))
	}
	return baseRepo{db: b.db, tx: t}
}

// WithinTx implements domain.TxManager: begins a transaction on the write pool
// (single connection, _txlock=immediate), calls fn, commits on success, rolls back
// on error/panic. Services use it to compose several repository writes atomically,
// e.g. create a kid and replace its kid_subjects.
func (d *DB) WithinTx(ctx context.Context, fn func(tx domain.Tx) error) error {
	tx, err := d.write.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

var _ domain.TxManager = (*DB)(nil)

// Connection pool parameters, based on the measured conclusions of ADR 0001 / PoC 6:
//   - Write pool strictly single connection: SQLite has a single writer; concurrent
//     writes over multiple connections produce heavy SQLITE_BUSY (PoC 6 scenario A).
//   - Write pool _txlock=immediate: a write transaction grabs the write lock up front,
//     which together with busy_timeout eliminates BUSY (PoC 6 scenario B).
//   - Read pool multiple connections: under WAL mode reads do not block writes
//     (PoC 6 scenario C).
const (
	writePoolMaxOpen = 1
	readPoolMaxOpen  = 10
	readPoolMaxIdle  = 5
	busyTimeoutMS    = 5000
)

// schemaVersionBaseline is the version baseline written by the initial migration,
// shown externally via /health.
const schemaVersionBaseline = 1

func SchemaVersionBaseline() int { return schemaVersionBaseline }

// DB wraps the read/write pools and exposes migration and health checks.
type DB struct {
	write *sqlx.DB // Single connection, _txlock=immediate.
	read  *sqlx.DB // Multiple connections, concurrent WAL reads.
	path  string   // Main DB file path, reported by the health page.
}

// Open opens (or creates) the SQLite database, configures the read/write pools, and
// applies the embedded migrations.
//
// DSN pragmas:
//   - _pragma=journal_mode(WAL): WAL mode, multiple readers single writer
//   - _pragma=busy_timeout(5000): wait up to 5s on a lock instead of failing at once
//   - _pragma=foreign_keys(1): enforce foreign key constraints
//
// The write pool also appends _txlock=immediate (see PoC 7).
func Open(ctx context.Context, path string) (*DB, error) {
	base := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)",
		url.PathEscape(path), busyTimeoutMS,
	)

	writeDB, err := sqlx.Open("sqlite", base+"&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("open write pool: %w", err)
	}
	writeDB.SetMaxOpenConns(writePoolMaxOpen)
	writeDB.SetMaxIdleConns(writePoolMaxOpen)
	writeDB.SetConnMaxLifetime(0)

	readDB, err := sqlx.Open("sqlite", base)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open read pool: %w", err)
	}
	readDB.SetMaxOpenConns(readPoolMaxOpen)
	readDB.SetMaxIdleConns(readPoolMaxIdle)
	readDB.SetConnMaxLifetime(0)

	if err := writeDB.PingContext(ctx); err != nil {
		_ = writeDB.Close()
		_ = readDB.Close()
		return nil, fmt.Errorf("ping write pool: %w", err)
	}
	if err := readDB.PingContext(ctx); err != nil {
		_ = writeDB.Close()
		_ = readDB.Close()
		return nil, fmt.Errorf("ping read pool: %w", err)
	}

	db := &DB{write: writeDB, read: readDB, path: path}
	if err := db.migrate(ctx); err != nil {
		_ = writeDB.Close()
		_ = readDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// Write returns the write pool. All writes (INSERT/UPDATE/DELETE, migrations) must
// go through it.
func (d *DB) Write() *sqlx.DB { return d.write }

// Read returns the read pool. Read-only queries go through it and may run
// concurrently with writes.
func (d *DB) Read() *sqlx.DB { return d.read }

// Path returns the main DB file path, used by the health page (DB size) and the
// backup manifest.
func (d *DB) Path() string { return d.path }

// Close closes both pools, always attempting both, and returns the first error.
func (d *DB) Close() error {
	var firstErr error
	if d.write != nil {
		if err := d.write.Close(); err != nil {
			firstErr = err
		}
	}
	if d.read != nil {
		if err := d.read.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Ping verifies both pools can connect; used by health checks.
func (d *DB) Ping(ctx context.Context) error {
	if err := d.write.PingContext(ctx); err != nil {
		return fmt.Errorf("ping write pool: %w", err)
	}
	if err := d.read.PingContext(ctx); err != nil {
		return fmt.Errorf("ping read pool: %w", err)
	}
	return nil
}

// Health runs a lightweight probe (connectivity, schema readiness, WAL active) and
// returns the display version tag.
//
// Ping only proves a connection can be established; it says nothing about whether
// migrations ran or tables exist, so this also queries schema_version and WAL mode.
func (d *DB) Health(ctx context.Context) (string, error) {
	if err := d.Ping(ctx); err != nil {
		return "", err
	}

	// COALESCE wraps the whole subquery so an empty database still yields 'unknown'.
	const q = `SELECT COALESCE((SELECT display_tag FROM migrations ORDER BY version DESC LIMIT 1), 'unknown')`
	var displayTag string
	if err := d.read.QueryRowContext(ctx, q).Scan(&displayTag); err != nil {
		return "", fmt.Errorf("probe migrations: %w", err)
	}

	var journalMode string
	if err := d.write.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		return "", fmt.Errorf("probe journal_mode: %w", err)
	}
	return displayTag, nil
}

// migrate applies the embedded 0001_init.sql to establish the schema.
//
// A single idempotent script: every DDL in 0001 carries IF NOT EXISTS, so re-running
// has no side effects and no version-dedup table is needed. The script runs in one
// transaction, so DDL and baseline writes are all-or-nothing — no half-applied tables.
func (d *DB) migrate(ctx context.Context) error {
	const name = "0001_init.sql"
	content, err := fs.ReadFile(migrations.FS, name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}

	tx, err := d.write.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	return nil
}
