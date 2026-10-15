package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"paperly/internal/domain"
)

// stubSigner captures the signing parameters, so tests do not depend on a real JWT key.
type stubSigner struct {
	parentCalled bool
	kidCalled    bool
}

func (s *stubSigner) SignParent(userID int64, displayName string, pinVersion int, now time.Time) (string, error) {
	s.parentCalled = true
	return fmt.Sprintf("parent:%d", userID), nil
}

func (s *stubSigner) SignKid(kidID int64, pinVersion int, now time.Time) (string, error) {
	s.kidCalled = true
	return fmt.Sprintf("kid:%d", kidID), nil
}

// newAuthTest assembles in-memory repos + a stub signer + a controllable clock.
func newAuthTest(user *domain.User, kids []*domain.Kid) (*AuthService, *stubSigner) {
	fakeUser := &fakeUserRepo{user: user}
	fakeKid := newMemRepo()
	for _, k := range kids {
		id, err := fakeKid.Insert(context.Background(), k)
		if err != nil {
			panic(err)
		}
		_ = fakeKid.byID[id]
	}
	signer := &stubSigner{}
	svc := NewAuthService(fakeUser, fakeKid, signer)
	now := time.Now()
	svc.now = func() time.Time { return now }
	svc.throttle.now = func() time.Time { return now }
	svc.inviteThrottle.now = func() time.Time { return now }
	return svc, signer
}

func activeUser(name, pin string) *domain.User {
	u := &domain.User{ID: 1, DisplayName: name, PinVersion: 1, Status: domain.UserStatusActive}
	if pin != "" {
		u.PinHash, _ = hashPin(pin)
	}
	return u
}

func activeKid(name, pin string, status int) *domain.Kid {
	k := &domain.Kid{Name: name, Grade: 9, Status: status, PinVersion: 1}
	if pin == "" {
		k.PinHash = PinPlaceholder
		return k
	}
	h, _ := hashPin(pin)
	k.PinHash = h
	return k
}

func TestLoginParentSuccess(t *testing.T) {
	svc, signer := newAuthTest(activeUser("家长A", "1234"), nil)
	ctx := context.Background()
	token, u, err := svc.LoginParent(ctx, 1, "1234")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !signer.parentCalled {
		t.Fatal("signer not called")
	}
	if token != "parent:1" || u.ID != 1 || u.DisplayName != "家长A" {
		t.Fatalf("unexpected result: token=%q user=%+v", token, u)
	}
}

func TestLoginParentWrongPin(t *testing.T) {
	svc, _ := newAuthTest(activeUser("家长A", "1234"), nil)
	_, _, err := svc.LoginParent(context.Background(), 1, "9999")
	if !errors.Is(err, ErrLoginInvalid) {
		t.Fatalf("want ErrLoginInvalid, got %v", err)
	}
}

func TestLoginParentLocksAfterFiveFails(t *testing.T) {
	svc, _ := newAuthTest(activeUser("家长A", "1234"), nil)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _, err := svc.LoginParent(ctx, 1, "wrong")
		if !errors.Is(err, ErrLoginInvalid) {
			t.Fatalf("fail #%d: want ErrLoginInvalid, got %v", i+1, err)
		}
	}
	_, _, err := svc.LoginParent(ctx, 1, "1234")
	if !errors.Is(err, ErrLoginLocked) {
		t.Fatalf("want ErrLoginLocked after 5 fails, got %v", err)
	}
}

func TestLoginParentSuccessResetsThrottle(t *testing.T) {
	svc, _ := newAuthTest(activeUser("家长A", "1234"), nil)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		_, _, _ = svc.LoginParent(ctx, 1, "wrong")
	}
	if _, _, err := svc.LoginParent(ctx, 1, "1234"); err != nil {
		t.Fatalf("correct pin should login: %v", err)
	}
	// After success the bucket is cleared: another 5 fails are needed to lock again.
	for i := 0; i < 5; i++ {
		_, _, _ = svc.LoginParent(ctx, 1, "wrong")
	}
	if _, _, err := svc.LoginParent(ctx, 1, "1234"); !errors.Is(err, ErrLoginLocked) {
		t.Fatalf("want ErrLoginLocked after second round of fails, got %v", err)
	}
}

func TestLoginParentUnknownID(t *testing.T) {
	svc, _ := newAuthTest(activeUser("家长A", "1234"), nil)
	_, _, err := svc.LoginParent(context.Background(), 999, "1234")
	if !errors.Is(err, ErrLoginInvalid) {
		t.Fatalf("want ErrLoginInvalid, got %v", err)
	}
	if len(svc.throttle.buckets) != 0 {
		t.Fatalf("unknown-ID attempts must not be throttled, buckets=%v", svc.throttle.buckets)
	}
}

func TestLoginParentInactive(t *testing.T) {
	u := activeUser("家长A", "1234")
	u.Status = 0 // deactivated
	svc, _ := newAuthTest(u, nil)
	_, _, err := svc.LoginParent(context.Background(), 1, "1234")
	if !errors.Is(err, ErrAccountInactive) {
		t.Fatalf("want ErrAccountInactive, got %v", err)
	}
}

func TestLoginKidSuccess(t *testing.T) {
	svc, signer := newAuthTest(nil, []*domain.Kid{activeKid("小明", "5678", domain.KidStatusActive)})
	ctx := context.Background()
	token, k, err := svc.LoginKid(ctx, 1, "5678")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !signer.kidCalled || token != "kid:1" || k.Name != "小明" {
		t.Fatalf("unexpected: token=%q kid=%+v", token, k)
	}
}

func TestLoginKidGraduatedAllowed(t *testing.T) {
	svc, _ := newAuthTest(nil, []*domain.Kid{activeKid("毕业", "1111", domain.KidStatusGraduated)})
	if _, _, err := svc.LoginKid(context.Background(), 1, "1111"); err != nil {
		t.Fatalf("graduated kid should login: %v", err)
	}
}

func TestLoginKidDeletedRejected(t *testing.T) {
	svc, _ := newAuthTest(nil, []*domain.Kid{activeKid("删除", "1111", domain.KidStatusDeleted)})
	_, _, err := svc.LoginKid(context.Background(), 1, "1111")
	if !errors.Is(err, ErrAccountInactive) {
		t.Fatalf("want ErrAccountInactive, got %v", err)
	}
}

func TestLoginKidNoPinSet(t *testing.T) {
	k := &domain.Kid{Name: "无PIN", Grade: 9, Status: domain.KidStatusActive, PinVersion: 1, PinHash: PinPlaceholder}
	svc, _ := newAuthTest(nil, []*domain.Kid{k})
	_, _, err := svc.LoginKid(context.Background(), 1, "1111")
	if !errors.Is(err, ErrLoginInvalid) {
		t.Fatalf("want ErrLoginInvalid, got %v", err)
	}
}

func TestLoginKidPlaceholderAnyPinFails(t *testing.T) {
	// A placeholder kid (no PIN set) gets 401 for any PIN, with the exact same response
	// as a wrong PIN, so the "this kid has not set a PIN" account state is not leaked.
	svc, _ := newAuthTest(nil, []*domain.Kid{activeKid("小明", "", domain.KidStatusActive)})
	for _, pin := range []string{"1111", "0000", "5678", "999999"} {
		if _, _, err := svc.LoginKid(context.Background(), 1, pin); !errors.Is(err, ErrLoginInvalid) {
			t.Fatalf("pin=%q: want ErrLoginInvalid, got %v", pin, err)
		}
	}
}

func TestLoginKidLocksAfterFiveFails(t *testing.T) {
	svc, _ := newAuthTest(nil, []*domain.Kid{activeKid("小明", "5678", domain.KidStatusActive)})
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _, _ = svc.LoginKid(ctx, 1, "wrong")
	}
	_, _, err := svc.LoginKid(ctx, 1, "5678")
	if !errors.Is(err, ErrLoginLocked) {
		t.Fatalf("want ErrLoginLocked, got %v", err)
	}
}

func TestParentValid(t *testing.T) {
	svc, _ := newAuthTest(activeUser("家长A", "1234"), nil)
	ctx := context.Background()

	cases := []struct {
		name string
		id   int64
		pv   int
		err  error
	}{
		{"ok", 1, 1, nil},
		{"pv mismatch", 1, 9, ErrPinVersionMismatch},
		{"not found", 2, 1, ErrAccountInactive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := svc.ParentValid(ctx, tc.id, tc.pv); !errors.Is(err, tc.err) {
				t.Fatalf("want %v, got %v", tc.err, err)
			}
		})
	}

	u := activeUser("家长A", "1234")
	u.Status = 0
	svc2, _ := newAuthTest(u, nil)
	if err := svc2.ParentValid(ctx, 1, 1); !errors.Is(err, ErrAccountInactive) {
		t.Fatalf("inactive: want ErrAccountInactive, got %v", err)
	}
}

func TestKidValid(t *testing.T) {
	svc, _ := newAuthTest(nil, []*domain.Kid{
		activeKid("小明", "5678", domain.KidStatusActive),
		{Name: "毕业", Grade: 9, Status: domain.KidStatusGraduated, PinVersion: 1, PinHash: "x"},
		{Name: "被删", Grade: 9, Status: domain.KidStatusDeleted, PinVersion: 1, PinHash: "x"},
	})
	ctx := context.Background()

	if err := svc.KidValid(ctx, 1, 1); err != nil {
		t.Fatalf("active kid: %v", err)
	}
	if err := svc.KidValid(ctx, 2, 1); err != nil {
		t.Fatalf("graduated kid: %v", err)
	}
	if err := svc.KidValid(ctx, 3, 1); !errors.Is(err, ErrAccountInactive) {
		t.Fatalf("deleted kid: want ErrAccountInactive, got %v", err)
	}
	if err := svc.KidValid(ctx, 1, 9); !errors.Is(err, ErrPinVersionMismatch) {
		t.Fatalf("pv mismatch: want ErrPinVersionMismatch, got %v", err)
	}
	if err := svc.KidValid(ctx, 42, 1); !errors.Is(err, ErrAccountInactive) {
		t.Fatalf("not found: want ErrAccountInactive, got %v", err)
	}
}

func TestCurrentParentAndKid(t *testing.T) {
	svc, _ := newAuthTest(activeUser("家长A", "1234"), []*domain.Kid{activeKid("小明", "5678", domain.KidStatusActive)})
	ctx := context.Background()
	if _, err := svc.CurrentParent(ctx, 1); err != nil {
		t.Fatalf("current parent: %v", err)
	}
	if _, err := svc.CurrentParent(ctx, 2); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown parent: want ErrNotFound, got %v", err)
	}
	if _, err := svc.CurrentKid(ctx, 1); err != nil {
		t.Fatalf("current kid: %v", err)
	}
	if _, err := svc.CurrentKid(ctx, 42); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown kid: want ErrNotFound, got %v", err)
	}
}

func TestListAccountsFiltersInactive(t *testing.T) {
	ctx := context.Background()
	// fakeUserRepo is single-row; switch to onbUserRepo (a multi-row in-memory repo).
	multi := &onbUserRepo{}
	svc := NewAuthService(multi, newMemRepo(), &stubSigner{})
	if _, err := multi.Insert(ctx, &domain.User{ID: 1, DisplayName: "家长A", PinHash: "x", PinVersion: 1, Status: domain.UserStatusActive}); err != nil {
		t.Fatal(err)
	}
	if _, err := multi.Insert(ctx, &domain.User{ID: 2, DisplayName: "停用", PinHash: "x", PinVersion: 1, Status: 0}); err != nil {
		t.Fatal(err)
	}
	out, err := svc.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].DisplayName != "家长A" {
		t.Fatalf("want only active account, got %+v", out)
	}
}

func TestListKidAccountsExcludesDeleted(t *testing.T) {
	ctx := context.Background()
	svc, _ := newAuthTest(nil, []*domain.Kid{
		activeKid("小明", "5678", domain.KidStatusActive),
		activeKid("毕业", "1111", domain.KidStatusGraduated),
		activeKid("被删", "2222", domain.KidStatusDeleted),
	})
	out, err := svc.ListKidAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 logins (exclude deleted), got %d", len(out))
	}
}

func TestChangeParentPinSuccess(t *testing.T) {
	svc, signer := newAuthTest(activeUser("家长A", "1234"), nil)
	ctx := context.Background()
	token, u, err := svc.ChangeParentPin(ctx, 1, "1234", "4321")
	if err != nil {
		t.Fatalf("change pin: %v", err)
	}
	if !signer.parentCalled {
		t.Fatal("signer not called")
	}
	if token != "parent:1" {
		t.Fatalf("want parent:1, got %q", token)
	}
	if u.PinVersion != 2 {
		t.Fatalf("pin_version should bump to 2, got %d", u.PinVersion)
	}
	if u.PinHash == "" || !VerifyPin(u.PinHash, "4321") {
		t.Fatal("new pin should be hashed and match")
	}
	// Credentials changed: the old PIN no longer logs in, the new one does
	// (pv+1 invalidates all old tokens).
	if _, _, err := svc.LoginParent(ctx, 1, "1234"); !errors.Is(err, ErrLoginInvalid) {
		t.Fatalf("old pin must no longer login, got %v", err)
	}
	if _, _, err := svc.LoginParent(ctx, 1, "4321"); err != nil {
		t.Fatalf("new pin must login: %v", err)
	}
}

func TestChangeParentPinFreshSetSkipsOld(t *testing.T) {
	// A PIN was never set: an empty old_pin should set it directly, without checking the old PIN.
	svc, _ := newAuthTest(activeUser("家长A", ""), nil)
	if _, _, err := svc.ChangeParentPin(context.Background(), 1, "", "1234"); err != nil {
		t.Fatalf("first-time set should skip old-pin check: %v", err)
	}
}

func TestChangeParentPinWrongOld(t *testing.T) {
	svc, _ := newAuthTest(activeUser("家长A", "1234"), nil)
	_, _, err := svc.ChangeParentPin(context.Background(), 1, "9999", "4321")
	if !errors.Is(err, ErrPinMismatch) {
		t.Fatalf("want ErrPinMismatch, got %v", err)
	}
}

func TestChangeParentPinInvalidNewPin(t *testing.T) {
	svc, _ := newAuthTest(activeUser("家长A", "1234"), nil)
	ctx := context.Background()
	if _, _, err := svc.ChangeParentPin(ctx, 1, "1234", ""); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("empty new pin: want ErrInvalidInput, got %v", err)
	}
	if _, _, err := svc.ChangeParentPin(ctx, 1, "1234", "12ab"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("non-digit new pin: want ErrInvalidInput, got %v", err)
	}
}

func TestChangeParentPinInactive(t *testing.T) {
	u := activeUser("家长A", "1234")
	u.Status = 0
	svc, _ := newAuthTest(u, nil)
	_, _, err := svc.ChangeParentPin(context.Background(), 1, "1234", "4321")
	if !errors.Is(err, ErrAccountInactive) {
		t.Fatalf("want ErrAccountInactive, got %v", err)
	}
}

func TestChangeParentPinUnknownID(t *testing.T) {
	svc, _ := newAuthTest(activeUser("家长A", "1234"), nil)
	_, _, err := svc.ChangeParentPin(context.Background(), 42, "1234", "4321")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestRenameParentSuccess(t *testing.T) {
	svc, _ := newAuthTest(activeUser("家长A", "1234"), nil)
	u, err := svc.RenameParent(context.Background(), 1, "爸爸")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if u.DisplayName != "爸爸" {
		t.Fatalf("want 爸爸, got %q", u.DisplayName)
	}
}

func TestRenameParentInvalidName(t *testing.T) {
	svc, _ := newAuthTest(activeUser("家长A", "1234"), nil)
	ctx := context.Background()
	if _, err := svc.RenameParent(ctx, 1, "  "); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("blank name: want ErrInvalidInput, got %v", err)
	}
	if _, err := svc.RenameParent(ctx, 1, "这是一个超过二十四个字的家长昵称啊啊啊啊啊啊啊啊啊啊啊啊啊啊啊啊啊啊"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("overlong name: want ErrInvalidInput, got %v", err)
	}
}

func TestRenameParentDuplicate(t *testing.T) {
	ctx := context.Background()
	multi := &onbUserRepo{}
	if _, err := multi.Insert(ctx, &domain.User{ID: 1, DisplayName: "家长A", PinHash: "x", PinVersion: 1, Status: domain.UserStatusActive}); err != nil {
		t.Fatal(err)
	}
	if _, err := multi.Insert(ctx, &domain.User{ID: 2, DisplayName: "爸爸", PinHash: "y", PinVersion: 1, Status: domain.UserStatusActive}); err != nil {
		t.Fatal(err)
	}
	svc := NewAuthService(multi, newMemRepo(), &stubSigner{})
	_, err := svc.RenameParent(ctx, 1, "爸爸")
	if !errors.Is(err, domain.ErrDuplicateDisplayName) {
		t.Fatalf("want ErrDuplicateDisplayName, got %v", err)
	}
}

func TestRenameParentUnknownID(t *testing.T) {
	svc, _ := newAuthTest(activeUser("家长A", "1234"), nil)
	_, err := svc.RenameParent(context.Background(), 42, "爸爸")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestIssueDefaultToken(t *testing.T) {
	svc, signer := newAuthTest(activeUser("家长A", "1234"), nil)
	token, u, err := svc.IssueDefaultToken(context.Background())
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !signer.parentCalled {
		t.Fatal("signer not called")
	}
	if token != "parent:1" || u.DisplayName != "家长A" {
		t.Fatalf("unexpected: token=%q user=%+v", token, u)
	}
}

func TestIssueDefaultTokenEmpty(t *testing.T) {
	svc, _ := newAuthTest(nil, nil)
	_, _, err := svc.IssueDefaultToken(context.Background())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("no active parent: want ErrNotFound, got %v", err)
	}
}

func TestIssueDefaultTokenInactive(t *testing.T) {
	u := activeUser("家长A", "1234")
	u.Status = 0
	svc, _ := newAuthTest(u, nil)
	_, _, err := svc.IssueDefaultToken(context.Background())
	if !errors.Is(err, ErrAccountInactive) {
		t.Fatalf("want ErrAccountInactive, got %v", err)
	}
}

// inviteKid builds a kid with an access_code. memKidRepo indexes by access_code into byCode.
func inviteKid(name, code, pin string, status int) *domain.Kid {
	k := activeKid(name, pin, status)
	k.AccessCode = code
	return k
}

func TestInviteVerifyValid(t *testing.T) {
	svc, _ := newAuthTest(nil, []*domain.Kid{inviteKid("小明", "abc123", "", domain.KidStatusActive)})
	k, err := svc.InviteVerify(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if k.Name != "小明" {
		t.Fatalf("want 小明, got %+v", k)
	}
}

func TestInviteVerifyUnknownCode(t *testing.T) {
	svc, _ := newAuthTest(nil, nil)
	if _, err := svc.InviteVerify(context.Background(), "nope1234"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestInviteVerifyGraduatedOrDeletedHides(t *testing.T) {
	// Graduated/deleted kids' GetByAccessCode must also return ErrNotFound — so "no such code"
	// cannot be told apart from "code valid but unusable", which would let account states
	// be enumerated.
	svc, _ := newAuthTest(nil, []*domain.Kid{
		inviteKid("毕业", "grad001", "", domain.KidStatusGraduated),
		inviteKid("被删", "delaa01", "", domain.KidStatusDeleted),
	})
	svc.inviteThrottle.now = func() time.Time { return time.Now() }
	k, err := svc.InviteVerify(context.Background(), "grad001")
	if k != nil || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("graduated: want ErrNotFound, got kid=%+v err=%v", k, err)
	}
	k, err = svc.InviteVerify(context.Background(), "delaa01")
	if k != nil || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted: want ErrNotFound, got kid=%+v err=%v", k, err)
	}
}

func TestInviteVerifyRateLimitedPerIP(t *testing.T) {
	svc, _ := newAuthTest(nil, []*domain.Kid{inviteKid("小明", "abc123", "", domain.KidStatusActive)})
	ctx := context.Background()
	// The 6th attempt from the same IP is rejected, while other IPs are unaffected.
	for i := 0; i < 5; i++ {
		if svc.InviteRateLimited("192.0.2.1") {
			t.Fatalf("attempt #%d should be allowed", i+1)
		}
	}
	if !svc.InviteRateLimited("192.0.2.1") {
		t.Fatal("6th attempt should be limited")
	}
	if svc.InviteRateLimited("192.0.2.2") {
		t.Fatal("different IP must not be limited")
	}
	// verify business still hits normally (only the entry rate limiter rejected the caller).
	if _, err := svc.InviteVerify(ctx, "abc123"); err != nil {
		t.Fatalf("verify should not be throttled internally: %v", err)
	}
}

func TestInviteSetupSuccess(t *testing.T) {
	svc, signer := newAuthTest(nil, []*domain.Kid{inviteKid("小明", "abc123", "", domain.KidStatusActive)})
	ctx := context.Background()
	token, k, err := svc.InviteSetup(ctx, "abc123", "6666")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if !signer.kidCalled || token != "kid:1" || k.Name != "小明" {
		t.Fatalf("unexpected: token=%q kid=%+v", token, k)
	}
	if k.PinVersion != 2 {
		t.Fatalf("pin_version should bump to 2, got %d", k.PinVersion)
	}
	if !VerifyPin(k.PinHash, "6666") {
		t.Fatal("PIN should be hashed and match")
	}
	// Set up is enough to log in: evidences closed loop
	// (placeholder -> valid hash -> successful login).
	if _, _, err := svc.LoginKid(ctx, 1, "6666"); err != nil {
		t.Fatalf("new pin should login: %v", err)
	}
}

func TestInviteSetupRejectsBadPin(t *testing.T) {
	svc, _ := newAuthTest(nil, []*domain.Kid{inviteKid("小明", "abc123", "", domain.KidStatusActive)})
	ctx := context.Background()
	for _, pin := range []string{"", "123", "abc", "1234567"} {
		if _, _, err := svc.InviteSetup(ctx, "abc123", pin); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("pin=%q: want ErrInvalidInput, got %v", pin, err)
		}
	}
}

func TestInviteSetupUnknownCodeRejected(t *testing.T) {
	svc, _ := newAuthTest(nil, nil)
	ctx := context.Background()
	if _, _, err := svc.InviteSetup(ctx, "nope1234", "6666"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
