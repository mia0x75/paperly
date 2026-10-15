package service

import (
	"testing"
	"time"
)

func TestThrottleLocksAfterMaxFails(t *testing.T) {
	now := time.Now()
	th := newLoginThrottle(func() time.Time { return now })
	if th.IsLocked("k") {
		t.Fatal("fresh bucket must not be locked")
	}
	for i := 0; i < maxLoginFails; i++ {
		th.RecordFailure("k")
	}
	// Locked right after the max-th failure, matching the login-check order.
	if !th.IsLocked("k") {
		t.Fatal("want locked after maxLoginFails failures")
	}
}

func TestThrottleIgnoresWhileLocked(t *testing.T) {
	now := time.Now()
	th := newLoginThrottle(func() time.Time { return now })
	for i := 0; i < maxLoginFails+3; i++ {
		th.RecordFailure("k")
	}
	// No further counting while locked; fails was already cleared, which does not affect the lock itself.
	if !th.IsLocked("k") {
		t.Fatal("want locked")
	}
}

func TestThrottleLockExpires(t *testing.T) {
	start := time.Now()
	cur := start
	th := newLoginThrottle(func() time.Time { return cur })
	for i := 0; i < maxLoginFails; i++ {
		th.RecordFailure("k")
	}
	if !th.IsLocked("k") {
		t.Fatal("want locked before expiry")
	}
	cur = start.Add(loginLockDuration + time.Minute)
	if th.IsLocked("k") {
		t.Fatal("lock must expire after loginLockDuration")
	}
	// Lock expired: the bucket is removed and failures recount from zero.
	if len(th.buckets) != 0 {
		t.Fatalf("expired bucket must be removed, got %v", th.buckets)
	}
	for i := 0; i < maxLoginFails-1; i++ {
		th.RecordFailure("k")
	}
	if th.IsLocked("k") {
		t.Fatal("must not relock before reaching max failures again")
	}
}

func TestThrottleResetClears(t *testing.T) {
	th := newLoginThrottle(time.Now)
	for i := 0; i < maxLoginFails-1; i++ {
		th.RecordFailure("k")
	}
	if th.IsLocked("k") {
		t.Fatal("must not lock before reaching max")
	}
	th.Reset("k")
	for i := 0; i < maxLoginFails-1; i++ {
		th.RecordFailure("k")
	}
	if th.IsLocked("k") {
		t.Fatal("reset must clear failure count")
	}
	// After Reset the bucket is gone and failures recount from zero (fails == max-1, not doubled).
	if b := th.buckets["k"]; b == nil || b.fails != maxLoginFails-1 {
		t.Fatalf("reset must restart failure count, got %+v", th.buckets["k"])
	}
}

// --- WindowThrottle (per-IP fixed window for the invite endpoint) ---

func TestWindowThrottleAllowsLimitThenRejects(t *testing.T) {
	now := time.Now()
	wt := newWindowThrottle(inviteMaxPerMin, inviteWindow, func() time.Time { return now })
	for i := 0; i < inviteMaxPerMin; i++ {
		if !wt.Allow("ip1") {
			t.Fatalf("attempt #%d should be allowed", i+1)
		}
	}
	if wt.Allow("ip1") {
		t.Fatal("attempt beyond limit should be rejected")
	}
}

func TestWindowThrottleKeysIndependent(t *testing.T) {
	wt := newWindowThrottle(inviteMaxPerMin, inviteWindow, time.Now)
	for i := 0; i < inviteMaxPerMin; i++ {
		wt.Allow("ip1")
	}
	if !wt.Allow("ip2") {
		t.Fatal("different key must not share the bucket")
	}
}

func TestWindowThrottleSlidesAfterWindow(t *testing.T) {
	cur := time.Now()
	wt := newWindowThrottle(inviteMaxPerMin, inviteWindow, func() time.Time { return cur })
	for i := 0; i < inviteMaxPerMin; i++ {
		wt.Allow("ip1")
	}
	if wt.Allow("ip1") {
		t.Fatal("must be limited inside window")
	}
	cur = cur.Add(inviteWindow + time.Second)
	if !wt.Allow("ip1") {
		t.Fatal("window must slide and reset the count")
	}
}
