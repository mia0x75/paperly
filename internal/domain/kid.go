// Package domain holds Paperly's domain models, invariants, and repository contracts.
//
// Innermost layer of a lightweight Clean Architecture: standard library only (context / time etc.),
// no SQL, HTTP or logging dependencies. Repository interfaces are declared here and implemented in
// the repository layer (dependency inversion), so services depend on abstractions.
package domain

import (
	"context"
	"errors"
	"time"
)

// Student gender (kids.gender, nullable): NULL = not filled in.
const (
	KidGenderFemale = 0
	KidGenderMale   = 1
)

// KidTrack is the arts/science track (kids.track, NOT NULL DEFAULT 0): 0 undecided / 1 arts /
// 2 science. Pairs with subjects.mask's high 2 bits to filter subjects for a kid.
const (
	KidTrackUndecided = 0
	KidTrackArts      = 1
	KidTrackScience   = 2
)

// KidStatus: 1 active / 2 graduated (read-only) / 3 soft-deleted. Chinese display happens only in
// DTO/frontend mappings; the DB stores the integer (see DATABASE_DESIGN.md, 0001_init.sql).
const (
	KidStatusActive    = 1
	KidStatusGraduated = 2
	KidStatusDeleted   = 3
)

// AccessCodeCharset drops confusable characters 0/o/1/l/i, leaving 31. 8 chars gives log2(31^8) ≈ 39.6 bit.
const (
	AccessCodeCharset = "23456789abcdefghjkmnpqrstuvwxyz"
	AccessCodeLength  = 8
)

// Domain errors, reused by repository/service and mapped to error codes by the api layer.
var (
	// ErrNotFound: the target entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalidInput: invalid input (empty name, malformed PIN, ...).
	ErrInvalidInput = errors.New("invalid input")
	// ErrAccessCodeCollision: access_code still collides after the max retries.
	ErrAccessCodeCollision = errors.New("access_code collision after max retries")
	// ErrDuplicateName: the kid name is taken (table-wide unique, including deleted/graduated).
	ErrDuplicateName = errors.New("duplicate kid name")
	// ErrInvalidState: the current state forbids the operation (e.g. deleting/restoring a graduated kid).
	ErrInvalidState = errors.New("invalid state")
)

// Kid corresponds to the kids table. gender/avatar are nullable pointers; pin_hash is NOT NULL — with
// no PIN set it holds a placeholder (service-layer PinPlaceholder) rather than NULL.
type Kid struct {
	ID         int64     `db:"id"`
	Name       string    `db:"name"`
	Grade      int       `db:"grade"`
	Gender     *int      `db:"gender"` // 1=male / 0=female / nil=unset
	Avatar     *string   `db:"avatar"`
	AccessCode string    `db:"access_code"`
	PinHash    string    `db:"pin_hash"` // bcrypt hash, or a placeholder when no PIN is set (non-empty)
	PinVersion int       `db:"pin_version"`
	Status     int       `db:"status"`
	Track      int       `db:"track"` // 0 undecided / 1 arts / 2 science (see KidTrack* consts)
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

// ListKidsFilter is the kid-list filter.
type ListKidsFilter struct {
	// Status matches exactly that status; 0 = no filter (deleted excluded by default).
	Status int
	Grade  int // 0 = no filter
	// When IncludeDeleted is true and Status == 0, deleted kids are not filtered out — the settings
	// page lists them so they can be restored.
	IncludeDeleted bool
}

// KidRepository is the kid data-access contract.
type KidRepository interface {
	Insert(ctx context.Context, s *Kid) (int64, error)
	GetByID(ctx context.Context, id int64) (*Kid, error)
	List(ctx context.Context, filter ListKidsFilter) ([]*Kid, error)
	Update(ctx context.Context, s *Kid) error
	SoftDelete(ctx context.Context, id int64) error
	// Restore revives a soft-deleted kid to active. A non-deleted state does not match ->
	// ErrNotFound (the service validates first for a clearer error).
	Restore(ctx context.Context, id int64) error
	// Graduate sets an active kid to graduated. Only active matches; graduated/deleted ->
	// ErrNotFound. Fired the year after the grade reaches 12.
	Graduate(ctx context.Context, id int64) error
	ExistsByAccessCode(ctx context.Context, code string) (bool, error)
	// ExistsByName reports whether the name is taken (table-wide unique, any status). excludeID > 0
	// excludes that row (update); pass 0 on create.
	ExistsByName(ctx context.Context, name string, excludeID int64) (bool, error)
	// GetByAccessCode looks up a kid for the kid-side invite/login entry. Only active kids;
	// missing/graduated/deleted all return ErrNotFound.
	GetByAccessCode(ctx context.Context, code string) (*Kid, error)

	// UpdateAccessCode overwrites access_code (parent reset); the old code dies immediately. A
	// soft-deleted kid does not match -> ErrNotFound.
	UpdateAccessCode(ctx context.Context, id int64, code string) error
	// UpdatePinHash overwrites pin_hash (bcrypt hash, or a placeholder when no PIN is set; the
	// placeholder never reaches a DTO) and increments pin_version, invalidating all old tokens. A
	// soft-deleted kid does not match -> ErrNotFound.
	UpdatePinHash(ctx context.Context, id int64, hash string) error

	// WithTx returns a new instance bound to tx.
	WithTx(tx Tx) KidRepository
}
