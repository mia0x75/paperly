package domain

import (
	"context"
	"errors"
	"time"
)

// User status: 1 active / 0 disabled (stored as INTEGER, see DATABASE_DESIGN.md, 0001_init.sql).
const (
	UserStatusDisabled = 0
	UserStatusActive   = 1
)

// User is a parent account, corresponding to the users table.
//
// Multiple parents are supported: first startup auto-creates no default account (onboarding guides
// the first registration), hence List and create endpoints. GetFirst keeps "smallest id" semantics,
// used by IssueDefaultToken to issue the default parent JWT once onboarding completes.
//
// An empty PinHash means "no PIN set", not NULL: users.pin_hash is NOT NULL (unlike kids.pin_hash),
// so representing "unset" with a pointer would force a NULL write and hit the constraint.
type User struct {
	ID          int64     `db:"id"`
	DisplayName string    `db:"display_name"`
	PinHash     string    `db:"pin_hash"`
	PinVersion  int       `db:"pin_version"`
	Status      int       `db:"status"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// UserRepository is the users data-access contract.
type UserRepository interface {
	// GetFirst returns the parent with the smallest id (the default parent, used by IssueDefaultToken
	// on first start). "First" rather than GetByID: the default parent is always the first row, and a
	// GetByID would tempt callers to hardcode the id into a URL. ErrNotFound when the table is empty.
	GetFirst(ctx context.Context) (*User, error)
	// GetByID fetches a parent by id (auth: login / /me / middleware liveness). Multi-parent login
	// addresses by id, semantically different from "default parent takes the first row". ErrNotFound
	// when missing.
	GetByID(ctx context.Context, id int64) (*User, error)
	// List returns all parent accounts, id ASC (onboarding parent list and settings page).
	List(ctx context.Context) ([]*User, error)
	// Insert creates a parent and returns the new id. A display_name hitting the UNIQUE constraint
	// should map to ErrDuplicateDisplayName.
	Insert(ctx context.Context, u *User) (int64, error)
	// ExistsByDisplayName reports whether the nickname is taken (table-wide unique). excludeID > 0
	// excludes that row (update); pass 0 on create.
	ExistsByDisplayName(ctx context.Context, name string, excludeID int64) (bool, error)
	// UpdateDisplayName changes the nickname, returning the affected count.
	UpdateDisplayName(ctx context.Context, id int64, name string) (int64, error)
	// UpdatePinHash changes the entry PIN (hash, or empty string to clear) and increments
	// pin_version. The increment supports "invalidate old sessions on PIN change" for a future
	// multi-device login; no consumer today, but the column exists and would otherwise stay at 1.
	UpdatePinHash(ctx context.Context, id int64, hash string) (int64, error)
	// WithTx returns a new instance bound to tx.
	WithTx(tx Tx) UserRepository
}

// UserMaxDisplayNameLen is the parent-nickname length limit (in runes).
const UserMaxDisplayNameLen = 24

// ErrDuplicateDisplayName: the parent nickname is taken (users.display_name table-wide unique).
var ErrDuplicateDisplayName = errors.New("duplicate display name")
