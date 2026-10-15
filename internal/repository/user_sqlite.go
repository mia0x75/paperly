package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"paperly/internal/domain"
)

// Explicit column list; SELECT * is forbidden (AGENTS.md SQLite rules).
const userColumns = `id, display_name, pin_hash, pin_version, status, created_at, updated_at`

type UserRepoSQLite struct {
	baseRepo
}

var _ domain.UserRepository = (*UserRepoSQLite)(nil)

func NewUserRepoSQLite(db *DB) *UserRepoSQLite {
	return &UserRepoSQLite{baseRepo: baseRepo{db: db}}
}

func (r *UserRepoSQLite) WithTx(tx any) domain.UserRepository {
	return &UserRepoSQLite{baseRepo: r.mustBindTx(tx)}
}

// Returns the parent account with the smallest id (default parent, used by
// IssueDefaultToken on first startup). Uses the read pool.
func (r *UserRepoSQLite) GetFirst(ctx context.Context) (*domain.User, error) {
	var u domain.User
	err := r.readExt().GetContext(ctx, &u,
		`SELECT `+userColumns+` FROM users ORDER BY id ASC LIMIT 1`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get first user: %w", err)
	}
	return &u, nil
}

func (r *UserRepoSQLite) GetByID(ctx context.Context, id int64) (*domain.User, error) {
	var u domain.User
	err := r.readExt().GetContext(ctx, &u,
		`SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user by id %d: %w", id, err)
	}
	return &u, nil
}

// Returns all parent accounts (ordered id ASC). Uses the read pool.
func (r *UserRepoSQLite) List(ctx context.Context) ([]*domain.User, error) {
	var out []*domain.User
	err := r.readExt().SelectContext(ctx, &out,
		`SELECT `+userColumns+` FROM users ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return out, nil
}

// Creates a new parent account. Uses the write pool. UNIQUE constraint on display_name maps
// to ErrDuplicateDisplayName as the final line of defense against concurrent writes.
func (r *UserRepoSQLite) Insert(ctx context.Context, u *domain.User) (int64, error) {
	res, err := r.writeExt().ExecContext(ctx, `
INSERT INTO users (display_name, pin_hash, pin_version, status)
VALUES (?, ?, ?, ?)`, u.DisplayName, u.PinHash, u.PinVersion, u.Status)
	if err != nil {
		if isUniqueDisplayNameErr(err) {
			return 0, domain.ErrDuplicateDisplayName
		}
		return 0, fmt.Errorf("insert user: %w", err)
	}
	return res.LastInsertId()
}

// Checks whether the nickname is already taken (globally unique, ignoring status). Uses the read pool.
func (r *UserRepoSQLite) ExistsByDisplayName(ctx context.Context, name string, excludeID int64) (bool, error) {
	var one int
	err := r.readExt().GetContext(ctx, &one, `
SELECT 1 FROM users WHERE display_name = ?
  AND (? <= 0 OR id != ?)
LIMIT 1`, name, excludeID, excludeID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check user display name: %w", err)
	}
	return true, nil
}

// The modernc driver exposes no assertable type, so match error text for
// "UNIQUE constraint failed: users.display_name" (same approach as kids.name).
func isUniqueDisplayNameErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: users.display_name")
}

func (r *UserRepoSQLite) UpdateDisplayName(ctx context.Context, id int64, name string) (int64, error) {
	res, err := r.writeExt().ExecContext(ctx,
		`UPDATE users SET display_name = ?, updated_at = datetime('now') WHERE id = ?`, name, id)
	if err != nil {
		return 0, fmt.Errorf("update user display_name: %w", err)
	}
	return res.RowsAffected()
}

func (r *UserRepoSQLite) UpdatePinHash(ctx context.Context, id int64, hash string) (int64, error) {
	res, err := r.writeExt().ExecContext(ctx, `
UPDATE users
SET pin_hash    = ?,
    pin_version = pin_version + 1,
    updated_at  = datetime('now')
WHERE id = ?`, hash, id)
	if err != nil {
		return 0, fmt.Errorf("update user pin_hash: %w", err)
	}
	return res.RowsAffected()
}
