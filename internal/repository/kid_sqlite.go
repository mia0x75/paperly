package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"paperly/internal/domain"
)

// kidColumns lists the query fields explicitly; SELECT * is forbidden (AGENTS.md
// SQLite rules).
const kidColumns = `id, name, grade, gender, avatar,
	access_code, pin_hash, pin_version, status, track, created_at, updated_at`

// KidRepoSQLite implements domain.KidRepository on SQLite. Row scans go through sqlx
// (GetContext/SelectContext); SQL is hand-written (AGENTS.md rules 9/10).
type KidRepoSQLite struct {
	baseRepo
}

var _ domain.KidRepository = (*KidRepoSQLite)(nil)

func NewKidRepoSQLite(db *DB) *KidRepoSQLite {
	return &KidRepoSQLite{baseRepo: baseRepo{db: db}}
}

// WithTx returns a shallow copy bound to tx; its statements then run inside that
// transaction.
func (r *KidRepoSQLite) WithTx(tx any) domain.KidRepository {
	return &KidRepoSQLite{baseRepo: r.mustBindTx(tx)}
}

// Insert writes a kid and returns the auto-increment id, via the write pool. NamedExec
// maps params through domain.Kid's db tags (no PO layer, rule 11).
func (r *KidRepoSQLite) Insert(ctx context.Context, s *domain.Kid) (int64, error) {
	const q = `INSERT INTO kids
		(name, grade, gender, avatar, access_code, pin_hash, pin_version, status, track)
		VALUES (:name, :grade, :gender, :avatar,
			:access_code, :pin_hash, :pin_version, :status, :track)`
	res, err := r.writeExt().NamedExecContext(ctx, q, s)
	if err != nil {
		return 0, fmt.Errorf("insert kid: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("kid last insert id: %w", err)
	}
	return id, nil
}

// GetByID returns domain.ErrNotFound if the kid is absent; uses the read pool.
func (r *KidRepoSQLite) GetByID(ctx context.Context, id int64) (*domain.Kid, error) {
	var s domain.Kid
	err := r.readExt().GetContext(ctx, &s,
		`SELECT `+kidColumns+` FROM kids WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get kid by id: %w", err)
	}
	return &s, nil
}

// List returns kids matching the filter, excluding deleted (status=3) by default; uses
// the read pool.
func (r *KidRepoSQLite) List(ctx context.Context, filter domain.ListKidsFilter) ([]*domain.Kid, error) {
	q := `SELECT ` + kidColumns + ` FROM kids WHERE 1=1`
	var args []any

	if filter.Status != 0 {
		q += ` AND status = ?`
		args = append(args, filter.Status)
	} else if !filter.IncludeDeleted {
		q += ` AND status != ?`
		args = append(args, domain.KidStatusDeleted)
	}
	if filter.Grade > 0 {
		q += ` AND grade = ?`
		args = append(args, filter.Grade)
	}
	q += ` ORDER BY id ASC`

	var kids []*domain.Kid
	if err := r.readExt().SelectContext(ctx, &kids, q, args...); err != nil {
		return nil, fmt.Errorf("list kids: %w", err)
	}
	if kids == nil {
		kids = make([]*domain.Kid, 0)
	}
	return kids, nil
}

// Update writes the kid's mutable fields (name/grade/gender/avatar) and bumps
// updated_at via the write pool. access_code/pin_hash have dedicated paths of their own.
func (r *KidRepoSQLite) Update(ctx context.Context, s *domain.Kid) error {
	res, err := r.writeExt().NamedExecContext(ctx, `
UPDATE kids SET
	name = :name, grade = :grade, gender = :gender, avatar = :avatar, track = :track,
	updated_at = CURRENT_TIMESTAMP
WHERE id = :id`, s)
	if err != nil {
		return fmt.Errorf("update kid: %w", err)
	}
	return affectedOrNotFound(res, "kid")
}

// SoftDelete sets status to deleted(3) via the write pool.
func (r *KidRepoSQLite) SoftDelete(ctx context.Context, id int64) error {
	const q = `UPDATE kids SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	res, err := r.writeExt().ExecContext(ctx, q, domain.KidStatusDeleted, id)
	if err != nil {
		return fmt.Errorf("soft delete kid: %w", err)
	}
	return affectedOrNotFound(res, "kid")
}

// ExistsByAccessCode reports whether access_code is taken; uses the read pool.
func (r *KidRepoSQLite) ExistsByAccessCode(ctx context.Context, code string) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM kids WHERE access_code = ?)`
	var exists bool
	if err := r.readExt().GetContext(ctx, &exists, q, code); err != nil {
		return false, fmt.Errorf("exists access_code: %w", err)
	}
	return exists, nil
}

// Restore flips a soft-deleted kid back to active via the write pool. It matches only
// status=deleted(3) rows, so anything else falls through affectedOrNotFound → ErrNotFound
// (the service distinguishes states up front).
func (r *KidRepoSQLite) Restore(ctx context.Context, id int64) error {
	const q = `UPDATE kids SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = ?`
	res, err := r.writeExt().ExecContext(ctx, q, domain.KidStatusActive, id, domain.KidStatusDeleted)
	if err != nil {
		return fmt.Errorf("restore kid: %w", err)
	}
	return affectedOrNotFound(res, "kid")
}

// Graduate marks an enrolled kid as graduated (status=2), triggered by the graduation
// branch of grade advancement (raw_target > 12). It matches only status=active(1) rows;
// already-graduated or soft-deleted rows fall through to ErrNotFound, since the
// idempotency gate guarantees a single advancement. Uses the write pool.
func (r *KidRepoSQLite) Graduate(ctx context.Context, id int64) error {
	const q = `UPDATE kids SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = ?`
	res, err := r.writeExt().ExecContext(ctx, q, domain.KidStatusGraduated, id, domain.KidStatusActive)
	if err != nil {
		return fmt.Errorf("graduate kid: %w", err)
	}
	return affectedOrNotFound(res, "kid")
}

// ExistsByName reports whether the name is taken (unique table-wide, regardless of
// status). Uses the read pool. excludeID > 0 excludes that row for update scenarios.
// Comparison is exact; the caller has already TrimSpace'd.
func (r *KidRepoSQLite) ExistsByName(ctx context.Context, name string, excludeID int64) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM kids WHERE name = ? AND id != ?)`
	var exists bool
	if err := r.readExt().GetContext(ctx, &exists, q, name, excludeID); err != nil {
		return false, fmt.Errorf("exists kid name: %w", err)
	}
	return exists, nil
}

// GetByAccessCode looks up an active kid by access_code (student passwordless login).
// Only status=active(1) is returned; graduated and deleted both surface as ErrNotFound
// with no distinction. Uses the read pool.
func (r *KidRepoSQLite) GetByAccessCode(ctx context.Context, code string) (*domain.Kid, error) {
	var s domain.Kid
	err := r.readExt().GetContext(ctx, &s,
		`SELECT `+kidColumns+` FROM kids WHERE access_code = ? AND status = ?`,
		code, domain.KidStatusActive)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get kid by access_code: %w", err)
	}
	return &s, nil
}

// UpdateAccessCode overwrites access_code on a parent reset; the old code stops working
// immediately. Uses the write pool. The WHERE also requires status != deleted(3): a
// soft-deleted kid must not be reset again, matching the service's visibility.
func (r *KidRepoSQLite) UpdateAccessCode(ctx context.Context, id int64, code string) error {
	const q = `UPDATE kids SET access_code = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status != ?`
	res, err := r.writeExt().ExecContext(ctx, q, code, id, domain.KidStatusDeleted)
	if err != nil {
		return fmt.Errorf("update access_code: %w", err)
	}
	return affectedOrNotFound(res, "kid")
}

// UpdatePinHash overwrites pin_hash (a bcrypt hash, or a placeholder when no PIN is set)
// and bumps pin_version, invalidating all old tokens. Uses the write pool.
func (r *KidRepoSQLite) UpdatePinHash(ctx context.Context, id int64, hash string) error {
	const q = `UPDATE kids SET pin_hash = ?, pin_version = pin_version + 1, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status != ?`
	res, err := r.writeExt().ExecContext(ctx, q, hash, id, domain.KidStatusDeleted)
	if err != nil {
		return fmt.Errorf("update pin_hash: %w", err)
	}
	return affectedOrNotFound(res, "kid")
}
