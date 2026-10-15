package service

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"

	"golang.org/x/crypto/bcrypt"

	"paperly/internal/domain"
)

// maxAccessCodeRetries is the max retry count for access_code uniqueness collisions.
const maxAccessCodeRetries = 10

// PinPlaceholder is the "no PIN set" placeholder (kids.pin_hash is NOT NULL).
// Written when creating a kid without a PIN, or when a parent clears the PIN, to satisfy NOT NULL.
// It is not a valid bcrypt hash (valid hashes start with "$2"), so VerifyPin always returns false —
// a placeholder kid fails login with any PIN, identically to a wrong PIN, without leaking account state.
// Exported for api/dto to decide pin_set (placeholder means unset); never send it to the frontend.
const PinPlaceholder = "!"

// pinPattern matches a 4-6 digit PIN.
var pinPattern = regexp.MustCompile(`^[0-9]{4,6}$`)

// Use crypto/rand for entropy; rejection sampling avoids modulo bias (32 divides 256, so no bias here).
func generateAccessCode() (string, error) {
	charset := domain.AccessCodeCharset
	n := big.NewInt(int64(len(charset)))
	buf := make([]byte, domain.AccessCodeLength)
	for i := range buf {
		idx, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", fmt.Errorf("rand access_code: %w", err)
		}
		buf[i] = charset[idx.Int64()]
	}
	return string(buf), nil
}

// validatePin checks an optional PIN: empty means unset (allowed, caller writes the placeholder); non-empty must be
// 4-6 digits.
func validatePin(pin string) error {
	if pin == "" {
		return nil
	}
	if !pinPattern.MatchString(pin) {
		return fmt.Errorf("%w: pin must be 4-6 digits", domain.ErrInvalidInput)
	}
	return nil
}

// hashPin slow-hashes a PIN with bcrypt. Empty PIN returns empty (caller decides whether to write the placeholder).
// PINs must be slow-hashed; plaintext/MD5/SHA-1 are forbidden.
func hashPin(pin string) (string, error) {
	if pin == "" {
		return "", nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash pin: %w", err)
	}
	return string(hash), nil
}

// VerifyPin checks whether a PIN matches a bcrypt hash.
//
// The placeholder short-circuits to false: do not rely on bcrypt's error behavior for invalid hashes —
// once the placeholder can be stored in a NOT NULL column, the login path must explicitly reject it.
func VerifyPin(hash, pin string) bool {
	if hash == "" || hash == PinPlaceholder || pin == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pin)) == nil
}
