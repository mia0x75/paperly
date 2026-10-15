package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"paperly/internal/domain"
)

// Auth domain errors: the api layer maps them to HTTP status codes (wrong PIN /
// missing account -> 401, rate-limited -> 429, other invalid input -> 400).
var (
	// ErrLoginInvalid means invalid credentials (wrong PIN or missing account); it is
	// always 401 to prevent enumeration.
	ErrLoginInvalid = errors.New("invalid login credentials")
	// ErrLoginLocked means rate-limited; the API returns 429.
	ErrLoginLocked = errors.New("login temporarily locked")
	// ErrAccountInactive means the account state forbids login (parent deactivated /
	// kid soft-deleted).
	ErrAccountInactive = errors.New("account inactive")
	// ErrPinVersionMismatch means the token pin_version differs from the current one,
	// so old sessions expire after a PIN change.
	ErrPinVersionMismatch = errors.New("pin version mismatch")
	// ErrPinMismatch means the current-PIN check failed in a logged-in PIN change. It
	// stays separate from ErrLoginInvalid so the API can return 400 without clearing
	// tokens or redirecting to login — the user is already logged in.
	ErrPinMismatch = errors.New("current pin mismatch")
)

// TokenSigner issues parent/kid JWTs. AuthService only depends on issuance;
// authorization and verification stay in middleware.
type TokenSigner interface {
	SignParent(userID int64, displayName string, pinVersion int, now time.Time) (string, error)
	SignKid(kidID int64, pinVersion int, now time.Time) (string, error)
}

type AuthService struct {
	users    domain.UserRepository
	kids     domain.KidRepository
	signer   TokenSigner
	throttle *LoginThrottle
	// inviteThrottle is a per-IP fixed-window limiter for /api/kid/invite/*, independent
	// from login throttling.
	inviteThrottle *WindowThrottle
	now            func() time.Time
}

func NewAuthService(users domain.UserRepository, kids domain.KidRepository, signer TokenSigner) *AuthService {
	return &AuthService{
		users:          users,
		kids:           kids,
		signer:         signer,
		throttle:       newLoginThrottle(time.Now),
		inviteThrottle: newWindowThrottle(inviteMaxPerMin, inviteWindow, time.Now),
		now:            time.Now,
	}
}

// Throttle keys: parents by display_name, kids by name. Attempts against missing
// accounts are not counted — there is no guessable PIN, and sharing a bucket with
// real accounts would cause collateral lockouts.
func parentLoginKey(displayName string) string { return "parent:" + displayName }
func kidLoginKey(name string) string           { return "kid:" + name }

// LoginParent logs in a parent with PIN (/api/auth/login); on success it issues a
// 90-day JWT.
func (s *AuthService) LoginParent(ctx context.Context, userID int64, pin string) (string, *domain.User, error) {
	if userID <= 0 {
		return "", nil, ErrLoginInvalid
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", nil, ErrLoginInvalid
		}
		return "", nil, fmt.Errorf("auth login parent lookup: %w", err)
	}
	key := parentLoginKey(u.DisplayName)
	if s.throttle.IsLocked(key) {
		return "", nil, ErrLoginLocked
	}
	if u.Status != domain.UserStatusActive {
		return "", nil, ErrAccountInactive
	}
	if !VerifyPin(u.PinHash, pin) {
		s.throttle.RecordFailure(key)
		return "", nil, ErrLoginInvalid
	}
	s.throttle.Reset(key)
	token, err := s.signer.SignParent(u.ID, u.DisplayName, u.PinVersion, s.now())
	if err != nil {
		return "", nil, fmt.Errorf("auth sign parent token: %w", err)
	}
	return token, u, nil
}

// LoginKid logs in a kid with PIN (/api/kid/auth/login); on success it issues a 90-day JWT.
// A kid without a PIN only has the placeholder hash, so VerifyPin is always false —
// same response as a wrong PIN.
func (s *AuthService) LoginKid(ctx context.Context, kidID int64, pin string) (string, *domain.Kid, error) {
	if kidID <= 0 {
		return "", nil, ErrLoginInvalid
	}
	k, err := s.kids.GetByID(ctx, kidID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", nil, ErrLoginInvalid
		}
		return "", nil, fmt.Errorf("auth login kid lookup: %w", err)
	}
	key := kidLoginKey(k.Name)
	if s.throttle.IsLocked(key) {
		return "", nil, ErrLoginLocked
	}
	if k.Status != domain.KidStatusActive && k.Status != domain.KidStatusGraduated {
		return "", nil, ErrAccountInactive
	}
	if !VerifyPin(k.PinHash, pin) {
		s.throttle.RecordFailure(key)
		return "", nil, ErrLoginInvalid
	}
	s.throttle.Reset(key)
	token, err := s.signer.SignKid(k.ID, k.PinVersion, s.now())
	if err != nil {
		return "", nil, fmt.Errorf("auth sign kid token: %w", err)
	}
	return token, k, nil
}

// CurrentParent returns the current parent by user_id (GET /api/me parent branch). If
// the account no longer exists it returns domain.ErrNotFound (the window where it was
// deleted between middleware and handler), mapped to 401.
func (s *AuthService) CurrentParent(ctx context.Context, userID int64) (*domain.User, error) {
	if userID <= 0 {
		return nil, domain.ErrNotFound
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("auth current parent: %w", err)
	}
	return u, nil
}

// CurrentKid returns the current kid by kid_id (GET /api/me kid branch).
func (s *AuthService) CurrentKid(ctx context.Context, kidID int64) (*domain.Kid, error) {
	if kidID <= 0 {
		return nil, domain.ErrNotFound
	}
	k, err := s.kids.GetByID(ctx, kidID)
	if err != nil {
		return nil, fmt.Errorf("auth current kid: %w", err)
	}
	return k, nil
}

// ParentValid lets middleware live-validate a parent token: status must be active,
// and pin_version must match the token.
func (s *AuthService) ParentValid(ctx context.Context, userID int64, pinVersion int) error {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return ErrAccountInactive
		}
		return fmt.Errorf("auth parent valid: %w", err)
	}
	if u.Status != domain.UserStatusActive {
		return ErrAccountInactive
	}
	if u.PinVersion != pinVersion {
		return ErrPinVersionMismatch
	}
	return nil
}

// KidValid lets middleware live-validate a kid token: status must be active/graduated,
// with matching pin_version.
func (s *AuthService) KidValid(ctx context.Context, kidID int64, pinVersion int) error {
	k, err := s.kids.GetByID(ctx, kidID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return ErrAccountInactive
		}
		return fmt.Errorf("auth kid valid: %w", err)
	}
	if k.Status != domain.KidStatusActive && k.Status != domain.KidStatusGraduated {
		return ErrAccountInactive
	}
	if k.PinVersion != pinVersion {
		return ErrPinVersionMismatch
	}
	return nil
}

// ListAccounts returns the login-able parent accounts (status=active) for the login
// page to select from.
func (s *AuthService) ListAccounts(ctx context.Context) ([]*domain.User, error) {
	all, err := s.users.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth list accounts: %w", err)
	}
	out := make([]*domain.User, 0, len(all))
	for _, u := range all {
		if u.Status == domain.UserStatusActive {
			out = append(out, u)
		}
	}
	return out, nil
}

// ListKidAccounts returns the login-able kids (excluding deleted) for the kid login
// page to select from. Kids without a PIN (placeholder hash) are listed too — the
// list does not reveal PIN state, and login uniformly fails; the official way to set
// a PIN is the parent-initiated /invite/:code flow.
func (s *AuthService) ListKidAccounts(ctx context.Context) ([]*domain.Kid, error) {
	kids, err := s.kids.List(ctx, domain.ListKidsFilter{})
	if err != nil {
		return nil, fmt.Errorf("auth list logins: %w", err)
	}
	return kids, nil
}

// IssueDefaultToken issues a JWT for the default parent (the registered one with the
// smallest id), for direct entry when onboarding completes (complete returns the
// token, so the frontend can skip manual login and go straight to the dashboard).
//
// If no parent is registered (edge cases like onboarding being aborted) it returns
// domain.ErrNotFound; the caller skips issuance but still returns done=true, and the
// frontend falls back to manual login so onboarding does not get stuck.
func (s *AuthService) IssueDefaultToken(ctx context.Context) (string, *domain.User, error) {
	u, err := s.users.GetFirst(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", nil, domain.ErrNotFound
		}
		return "", nil, fmt.Errorf("auth issue default token lookup: %w", err)
	}
	if u.Status != domain.UserStatusActive {
		return "", nil, ErrAccountInactive
	}
	token, err := s.signer.SignParent(u.ID, u.DisplayName, u.PinVersion, s.now())
	if err != nil {
		return "", nil, fmt.Errorf("auth sign parent token: %w", err)
	}
	return token, u, nil
}

// RenameParent renames the nickname while logged in (POST /api/me/rename, parent
// audience only). Validation matches UserService.UpdateDisplayName (both share
// validateDisplayName), differing only in addressing: here it resolves by the
// user_id in the token, not a "default parent".
func (s *AuthService) RenameParent(ctx context.Context, userID int64, name string) (*domain.User, error) {
	if userID <= 0 {
		return nil, domain.ErrNotFound
	}
	trimmed, err := validateDisplayName(name)
	if err != nil {
		return nil, err
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("auth rename parent lookup: %w", err)
	}
	// Exclude self: renaming to the current name is a valid no-op; clashing with
	// another parent's name is rejected.
	exists, err := s.users.ExistsByDisplayName(ctx, trimmed, u.ID)
	if err != nil {
		return nil, fmt.Errorf("auth rename check name: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("%w: parent display name already exists", domain.ErrDuplicateDisplayName)
	}
	if _, err := s.users.UpdateDisplayName(ctx, u.ID, trimmed); err != nil {
		return nil, fmt.Errorf("auth rename parent: %w", err)
	}
	return s.users.GetByID(ctx, u.ID)
}

// ChangeParentPin changes the parent PIN while logged in (POST /api/me/pin parent branch).
//
// Validates the old PIN (accounts that never set a PIN skip this check and set
// directly on first time) → hashes the new PIN → persists (pin_version+1) → issues a
// new JWT with the new pin_version. The frontend continues seamlessly with the new
// token; other sessions' old tokens get 401 at middleware due to the pv mismatch.
func (s *AuthService) ChangeParentPin(ctx context.Context, userID int64, oldPin, newPin string) (string, *domain.User, error) {
	if userID <= 0 {
		return "", nil, domain.ErrNotFound
	}
	if newPin == "" {
		return "", nil, fmt.Errorf("%w: new PIN cannot be empty", domain.ErrInvalidInput)
	}
	if err := validatePin(newPin); err != nil {
		return "", nil, err
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", nil, fmt.Errorf("auth change pin lookup: %w", err)
	}
	if u.Status != domain.UserStatusActive {
		return "", nil, ErrAccountInactive
	}
	if u.PinHash != "" && !VerifyPin(u.PinHash, oldPin) {
		return "", nil, ErrPinMismatch
	}
	hash, err := hashPin(newPin)
	if err != nil {
		return "", nil, err
	}
	if _, err := s.users.UpdatePinHash(ctx, userID, hash); err != nil {
		return "", nil, fmt.Errorf("auth change pin: %w", err)
	}
	// Re-read to get the incremented pin_version (UpdatePinHash does +1 in the DB),
	// avoiding drift in manual computation.
	u, err = s.users.GetByID(ctx, userID)
	if err != nil {
		return "", nil, fmt.Errorf("auth change pin reload: %w", err)
	}
	token, err := s.signer.SignParent(u.ID, u.DisplayName, u.PinVersion, s.now())
	if err != nil {
		return "", nil, fmt.Errorf("auth sign parent token: %w", err)
	}
	return token, u, nil
}

// ChangeKidPin changes a kid's PIN while logged in (POST /api/me/pin kid branch).
//
// Mirrors the parent branch: verify old PIN → hash → persist (pin_version+1) →
// re-read → new JWT. Old-PIN verification has no pass-through branch: the placeholder
// (no PIN set) is explicitly rejected in VerifyPin, following the same path as a wrong
// PIN — a kid without a PIN cannot reach first-time setup (setting a PIN happens only
// in invite/setup and the parent reset entry). Status must be active/graduated (same
// basis as KidValid).
func (s *AuthService) ChangeKidPin(ctx context.Context, kidID int64, oldPin, newPin string) (string, *domain.Kid, error) {
	if kidID <= 0 {
		return "", nil, domain.ErrNotFound
	}
	if newPin == "" {
		return "", nil, fmt.Errorf("%w: new PIN cannot be empty", domain.ErrInvalidInput)
	}
	if err := validatePin(newPin); err != nil {
		return "", nil, err
	}
	k, err := s.kids.GetByID(ctx, kidID)
	if err != nil {
		return "", nil, fmt.Errorf("auth change kid pin lookup: %w", err)
	}
	if k.Status != domain.KidStatusActive && k.Status != domain.KidStatusGraduated {
		return "", nil, ErrAccountInactive
	}
	if !VerifyPin(k.PinHash, oldPin) {
		return "", nil, ErrPinMismatch
	}
	hash, err := hashPin(newPin)
	if err != nil {
		return "", nil, err
	}
	if err := s.kids.UpdatePinHash(ctx, kidID, hash); err != nil {
		return "", nil, fmt.Errorf("auth change kid pin: %w", err)
	}
	// Re-read to get the incremented pin_version (UpdatePinHash does +1 in the DB),
	// avoiding manual drift.
	k, err = s.kids.GetByID(ctx, kidID)
	if err != nil {
		return "", nil, fmt.Errorf("auth change kid pin reload: %w", err)
	}
	token, err := s.signer.SignKid(k.ID, k.PinVersion, s.now())
	if err != nil {
		return "", nil, fmt.Errorf("auth sign kid token: %w", err)
	}
	return token, k, nil
}

// InviteRateLimited reports whether the client key (IP) has hit the invite endpoint
// limit. It returns true when rate-limited, and the caller should reject the request;
// passing increments the internal count. The limit is a fixed window of 5 per minute.
// verify and setup share one bucket so that alternating verify+setup cannot accumulate
// hits without tripping a single endpoint's threshold.
func (s *AuthService) InviteRateLimited(key string) bool {
	return !s.inviteThrottle.Allow(key)
}

// InviteVerify validates an invite code (/api/kid/invite/verify, anonymous).
//
// Only returns active kids; a miss returns domain.ErrNotFound (the caller converts it
// to {valid:false}, not revealing kid_id and not distinguishing "no such code" from
// "code valid but unusable"). access_code is a login credential; it is consumed only
// here and must never be logged.
func (s *AuthService) InviteVerify(ctx context.Context, code string) (*domain.Kid, error) {
	k, err := s.kids.GetByAccessCode(ctx, code)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("auth invite verify lookup: %w", err)
	}
	return k, nil
}

// InviteSetup sets a kid's PIN and issues the kid JWT (/api/kid/invite/setup, anonymous).
//
// This is the first write of a kid's identity: set PIN (bcrypt) → pin_version+1 →
// issue the JWT with the new version (same path as a parent PIN change). Any old token
// is invalidated immediately by the pv mismatch. On failure returns: ErrInvalidInput
// (PIN format/empty), domain.ErrNotFound (invalid invite code).
func (s *AuthService) InviteSetup(ctx context.Context, code, pin string) (string, *domain.Kid, error) {
	if pin == "" {
		return "", nil, fmt.Errorf("%w: pin is required", domain.ErrInvalidInput)
	}
	if err := validatePin(pin); err != nil {
		return "", nil, err
	}
	k, err := s.kids.GetByAccessCode(ctx, code)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", nil, domain.ErrNotFound
		}
		return "", nil, fmt.Errorf("auth invite setup lookup: %w", err)
	}
	hash, err := hashPin(pin)
	if err != nil {
		return "", nil, err
	}
	if err := s.kids.UpdatePinHash(ctx, k.ID, hash); err != nil {
		return "", nil, fmt.Errorf("auth invite set pin: %w", err)
	}
	// Re-read to get the incremented pin_version (UpdatePinHash does +1 in the DB),
	// avoiding manual drift.
	k, err = s.kids.GetByID(ctx, k.ID)
	if err != nil {
		return "", nil, fmt.Errorf("auth invite setup reload: %w", err)
	}
	token, err := s.signer.SignKid(k.ID, k.PinVersion, s.now())
	if err != nil {
		return "", nil, fmt.Errorf("auth sign invite kid token: %w", err)
	}
	return token, k, nil
}
