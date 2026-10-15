package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The key is generated on first call (0600, KeyBytes length), and the same key
// is read back on a second start so a token signed by one Store verifies with
// another.
func TestLoadOrCreateGeneratesKeyOnce(t *testing.T) {
	dir := t.TempDir()
	s1, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("first LoadOrCreate: %v", err)
	}
	p := filepath.Join(dir, KeyName)
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("key file mode = %o, want 600", got)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	if len(raw) != KeyBytes {
		t.Fatalf("key length = %d, want %d", len(raw), KeyBytes)
	}

	s2, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("second LoadOrCreate: %v", err)
	}
	// Fixed future timestamp as the exp baseline (avoids coupling to the wall
	// clock, which would misjudge "expired").
	now := time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC)
	tok, err := s1.SignParent(1, "家长", 2, now)
	if err != nil {
		t.Fatalf("sign with s1: %v", err)
	}
	if _, err := s2.Verify(tok); err != nil {
		t.Fatalf("verify with s2 (same key): %v", err)
	}
}

// An abnormal key file length is rejected rather than silently reused.
func TestLoadOrCreateRejectsBadKeyLength(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, KeyName), []byte("tooshort"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(dir); err == nil {
		t.Fatal("want error for invalid key length, got nil")
	}
}

// The parent payload round-trips, including exp ~= now+90d.
func TestSignAndVerifyParent(t *testing.T) {
	s, _ := LoadOrCreate(t.TempDir())
	now := time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC)
	tok, err := s.SignParent(7, "家长A", 3, now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Typ != TypParent || got.Sub != 7 || got.DN != "家长A" || got.PV != 3 {
		t.Fatalf("claims mismatch: %+v", got)
	}
	if want := now.Add(TokenTTL).Unix(); got.Exp != want {
		t.Fatalf("exp = %d, want %d", got.Exp, want)
	}
}

// The kid payload round-trips (no DN).
func TestSignAndVerifyKid(t *testing.T) {
	s, _ := LoadOrCreate(t.TempDir())
	tok, err := s.SignKid(11, 1, time.Now())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Typ != TypKid || got.Sub != 11 || got.PV != 1 || got.DN != "" {
		t.Fatalf("claims mismatch: %+v", got)
	}
}

// An expired token returns ErrTokenExpired (assertable category).
func TestVerifyExpiredToken(t *testing.T) {
	s, _ := LoadOrCreate(t.TempDir())
	now := time.Now()
	past := now.Add(-TokenTTL - 2*time.Minute)
	tok, err := s.SignParent(7, "家长A", 3, past)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := s.Verify(tok); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("want ErrTokenExpired, got %v", err)
	}
}

// A truncated payload causes a signature failure -> ErrTokenInvalid.
func TestVerifyTamperedToken(t *testing.T) {
	s, _ := LoadOrCreate(t.TempDir())
	tok, _ := s.SignParent(7, "家长A", 3, time.Now())
	parts := strings.Split(tok, ".")
	parts[1] = parts[1][:len(parts[1])-1]
	if _, err := s.Verify(strings.Join(parts, ".")); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("want ErrTokenInvalid, got %v", err)
	}
}

// A signature from a different key -> ErrTokenInvalid.
func TestVerifyWrongKey(t *testing.T) {
	s1, _ := LoadOrCreate(t.TempDir())
	s2, _ := LoadOrCreate(t.TempDir())
	tok, _ := s1.SignParent(7, "家长A", 3, time.Now())
	if _, err := s2.Verify(tok); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("want ErrTokenInvalid, got %v", err)
	}
}

// A non-parent/kid payload is rejected even if the signature verifies.
func TestVerifyUnsupportedTyp(t *testing.T) {
	s, _ := LoadOrCreate(t.TempDir())
	tok, err := s.sign(&Claims{Typ: "admin", Sub: 7, PV: 1, Exp: time.Now().Add(TokenTTL).Unix()})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := s.Verify(tok); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("want ErrTokenInvalid, got %v", err)
	}
}

// Non-token text -> ErrTokenInvalid.
func TestVerifyGarbage(t *testing.T) {
	s, _ := LoadOrCreate(t.TempDir())
	if _, err := s.Verify("not-a-jwt"); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("want ErrTokenInvalid, got %v", err)
	}
}
