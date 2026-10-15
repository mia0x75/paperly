package service

import (
	"sync"
	"time"
)

// Login throttling parameters: maxLoginFails consecutive failures lock for loginLockDuration.
const (
	maxLoginFails     = 5
	loginLockDuration = 15 * time.Minute
)

type loginBucket struct {
	fails       int
	lockedUntil time.Time
}

// LoginThrottle limits failed logins by account key (parent display_name / kid name),
// counting consecutive failures, locking for 15 minutes once the cap is hit, and resetting on success.
//
// Pure in-memory: cleared on restart (acceptable for a single-box deployment);
// keys are never persisted or logged. Time comes from the injected now so tests can advance the clock.
type LoginThrottle struct {
	mu      sync.Mutex
	buckets map[string]*loginBucket
	now     func() time.Time
}

func newLoginThrottle(now func() time.Time) *LoginThrottle {
	return &LoginThrottle{buckets: map[string]*loginBucket{}, now: now}
}

// IsLocked reports whether the key is currently locked. Expired lock buckets are also cleared,
// so residual counts do not retrigger a lock the moment it expires; unlocked count buckets are kept.
func (t *LoginThrottle) IsLocked(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buckets[key]
	if b == nil {
		return false
	}
	if b.lockedUntil.After(t.now()) {
		return true
	}
	if !b.lockedUntil.IsZero() {
		// Was locked but now expired: clear the bucket so failures recount from zero.
		delete(t.buckets, key)
	}
	return false
}

// RecordFailure records a login failure; once the cap is reached it locks (no further counting while locked).
func (t *LoginThrottle) RecordFailure(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	b := t.buckets[key]
	if b == nil {
		b = &loginBucket{}
		t.buckets[key] = b
	}
	if b.lockedUntil.After(t.now()) {
		return
	}
	b.fails++
	if b.fails >= maxLoginFails {
		b.fails = 0
		b.lockedUntil = t.now().Add(loginLockDuration)
	}
}

// Reset clears the key's failure record (called on successful login).
func (t *LoginThrottle) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.buckets, key)
}

// Invite endpoint throttling: each key (per IP) is allowed at most 5 attempts per fixed window.
// access_code is a login credential; verify/setup are not counted in the login throttle bucket (otherwise an attacker
// abusing invite codes could lock a legitimate kid's login key); a separate per-IP fixed window is used instead.
const (
	inviteWindow     = time.Minute
	inviteMaxPerMin  = 5
	inviteBucketsCap = 1024
)

type windowBucket struct {
	start time.Time
	count int
}

// WindowThrottle is a fixed-window throttle: requests after the (limit+1)-th within the window are rejected,
// and the count resets after the window slides.
//
// Simpler than a sliding window; the cost is that a burst may pass at the boundary (acceptable for a single-box anti-brute-force case).
// The key is supplied by the caller (IP or business key) and is independent of LoginThrottle's per-account lock.
// Pure in-memory; cleared on restart.
type WindowThrottle struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]*windowBucket
	now     func() time.Time
}

func newWindowThrottle(limit int, window time.Duration, now func() time.Time) *WindowThrottle {
	return &WindowThrottle{
		limit:   limit,
		window:  window,
		buckets: map[string]*windowBucket{},
		now:     now,
	}
}

// Allow reports whether the key may pass now, incrementing once on pass (within the same fixed window).
// Lazy cleanup: expired buckets are deleted on access; when the bucket count exceeds the cap all are reset (a single-box deployment has a finite IP count).
func (t *WindowThrottle) Allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	b := t.buckets[key]
	if b == nil || now.Sub(b.start) >= t.window {
		if len(t.buckets) >= inviteBucketsCap {
			t.buckets = map[string]*windowBucket{}
		}
		t.buckets[key] = &windowBucket{start: now, count: 1}
		return true
	}
	b.count++
	return b.count <= t.limit
}
