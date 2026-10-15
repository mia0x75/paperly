// Package auth provides JWT (HS256) key persistence and token signing/parsing.
//
// The key is generated on first start-up (crypto/rand, 32 bytes) and written to
// jwt.key in the data directory (0600), then reused on restarts. Payload is
// defined by Claims: a parent carries typ=parent/sub/dn/pv, a kid typ=kid/sub/pv;
// exp = signing time + 90d (no sliding renewal, no rotation).
//
// Dependency constraints: standard library + golang-jwt/jwt/v5 only; no logging,
// not exposed via DTO.
package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	TypParent = "parent"
	TypKid    = "kid"
)

// TokenTTL is the JWT lifetime: 90 days. Re-login is required on expiry; there is
// no sliding renewal.
const TokenTTL = 90 * 24 * time.Hour

// KeyName is the key file name, placed in the data directory.
const KeyName = "jwt.key"

// KeyBytes is the key length: HS256 requires >=32 bytes, fixed at 32.
const KeyBytes = 32

// Verify errors, categorized so callers can distinguish expiry from other
// invalidity.
var (
	// ErrTokenInvalid indicates a failed signature, invalid payload, or
	// unsupported token type.
	ErrTokenInvalid = errors.New("jwt: invalid token")
	ErrTokenExpired = errors.New("jwt: token expired")
)

// Claims is the JWT payload. Sub is the payload primary key id, PV is
// pin_version (changing the PIN invalidates old sessions), DN is carried only
// by parents.
//
// A custom struct is used instead of embedding jwt.RegisteredClaims, because
// RegisteredClaims.Subject is a string with the same "sub" json key, which
// conflicts with the numeric Sub; the iss/aud/nbf/iat equivalent keys are also
// omitted to keep the payload minimal.
type Claims struct {
	Typ string `json:"typ"`
	Sub int64  `json:"sub"`
	DN  string `json:"dn,omitempty"`
	PV  int    `json:"pv"`
	Exp int64  `json:"exp"`
}

// GetExpirationTime implements jwt.Claims: lets the parser validate exp.
func (c *Claims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.Exp, 0)), nil
}

// GetIssuedAt implements jwt.Claims (iat not issued; returns nil).
func (c *Claims) GetIssuedAt() (*jwt.NumericDate, error) { return nil, nil }

// GetNotBefore implements jwt.Claims (nbf not issued; returns nil).
func (c *Claims) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }

// GetIssuer implements jwt.Claims (iss not issued; returns empty string).
func (c *Claims) GetIssuer() (string, error) { return "", nil }

// GetSubject implements jwt.Claims (sub lives in Claims.Sub; returns empty string).
func (c *Claims) GetSubject() (string, error) { return "", nil }

// GetAudience implements jwt.Claims (aud not issued; returns nil).
func (c *Claims) GetAudience() (jwt.ClaimStrings, error) { return nil, nil }

// Validate implements jwt.ClaimsValidator (the v5 interface method is named
// Validate, not v4's Valid): rejects invalid primary keys/types as a second
// line of defense after signature verification.
func (c *Claims) Validate() error {
	if c.Sub <= 0 {
		return fmt.Errorf("%w: empty subject", ErrTokenInvalid)
	}
	if c.Typ != TypParent && c.Typ != TypKid {
		return fmt.Errorf("%w: unsupported typ %q", ErrTokenInvalid, c.Typ)
	}
	if c.PV < 1 {
		return fmt.Errorf("%w: empty pin version", ErrTokenInvalid)
	}
	return nil
}

type Store struct {
	key  []byte
	path string
}

// LoadOrCreate reads jwt.key from the data directory, or generates it if
// absent (crypto/rand, 32 bytes, 0600). It is idempotent: the same key is
// loaded on subsequent starts so tokens remain verifiable across restarts.
func LoadOrCreate(dir string) (*Store, error) {
	path := filepath.Join(dir, KeyName)
	if b, err := os.ReadFile(path); err == nil {
		if len(b) != KeyBytes {
			return nil, fmt.Errorf("jwt key %s has invalid length %d (want %d)", path, len(b), KeyBytes)
		}
		return &Store{key: b, path: path}, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read jwt key %s: %w", path, err)
	}

	key := make([]byte, KeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate jwt key: %w", err)
	}
	// Explicit 0600: os.OpenFile's perm is tightened by umask, so a follow-up
	// chmod guarantees the "plaintext key readable/writable only by owner"
	// security boundary does not drift with the environment.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create jwt key %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("chmod jwt key %s: %w", path, err)
	}
	if _, err := f.Write(key); err != nil {
		return nil, fmt.Errorf("write jwt key %s: %w", path, err)
	}
	return &Store{key: key, path: path}, nil
}

func (s *Store) SignParent(userID int64, displayName string, pinVersion int, now time.Time) (string, error) {
	return s.sign(&Claims{
		Typ: TypParent,
		Sub: userID,
		DN:  displayName,
		PV:  pinVersion,
		Exp: now.Add(TokenTTL).Unix(),
	})
}

func (s *Store) SignKid(kidID int64, pinVersion int, now time.Time) (string, error) {
	return s.sign(&Claims{
		Typ: TypKid,
		Sub: kidID,
		PV:  pinVersion,
		Exp: now.Add(TokenTTL).Unix(),
	})
}

func (s *Store) sign(c *Claims) (string, error) {
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("sign %s token: %w", c.Typ, err)
	}
	return token, nil
}

// Verify verifies the signature and parses the token.
//
// Failed signature / invalid payload / unsupported type -> ErrTokenInvalid;
// expired -> ErrTokenExpired. Only static validation is done here: the live
// verification of pin_version and status is done by the middleware against
// the DB.
func (s *Store) Verify(token string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return s.key, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}
	if !parsed.Valid {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}
