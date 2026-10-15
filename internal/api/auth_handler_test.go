package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"paperly/internal/auth"
	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/service"
)

// memAuthUserRepo is a multi-row user repository (login by id and the accounts
// list both need multiple rows).
type memAuthUserRepo struct {
	byID   map[int64]*domain.User
	nextID int64
}

func newMemAuthUserRepo() *memAuthUserRepo {
	return &memAuthUserRepo{byID: map[int64]*domain.User{}}
}

func (m *memAuthUserRepo) GetFirst(context.Context) (*domain.User, error) {
	var first *domain.User
	for _, u := range m.byID {
		if first == nil || u.ID < first.ID {
			first = u
		}
	}
	if first == nil {
		return nil, domain.ErrNotFound
	}
	return first, nil
}

func (m *memAuthUserRepo) GetByID(_ context.Context, id int64) (*domain.User, error) {
	u, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return u, nil
}

func (m *memAuthUserRepo) List(_ context.Context) ([]*domain.User, error) {
	out := make([]*domain.User, 0, len(m.byID))
	for _, u := range m.byID {
		out = append(out, u)
	}
	return out, nil
}

func (m *memAuthUserRepo) Insert(_ context.Context, u *domain.User) (int64, error) {
	m.nextID++
	cp := *u
	cp.ID = m.nextID
	m.byID[cp.ID] = &cp
	return cp.ID, nil
}

func (m *memAuthUserRepo) ExistsByDisplayName(_ context.Context, name string, excludeID int64) (bool, error) {
	for _, u := range m.byID {
		if u.ID != excludeID && u.DisplayName == name {
			return true, nil
		}
	}
	return false, nil
}

func (m *memAuthUserRepo) UpdateDisplayName(_ context.Context, id int64, name string) (int64, error) {
	u, ok := m.byID[id]
	if !ok {
		return 0, domain.ErrNotFound
	}
	u.DisplayName = name
	return 1, nil
}

func (m *memAuthUserRepo) UpdatePinHash(_ context.Context, id int64, hash string) (int64, error) {
	u, ok := m.byID[id]
	if !ok {
		return 0, domain.ErrNotFound
	}
	u.PinHash = hash
	u.PinVersion++
	return 1, nil
}

// memAuthKidRepo implements domain.KidRepository (auth-side only; filters by status/name).
type memAuthKidRepo struct {
	byID map[int64]*domain.Kid
}

func newMemAuthKidRepo() *memAuthKidRepo {
	return &memAuthKidRepo{byID: map[int64]*domain.Kid{}}
}

func (m *memAuthKidRepo) Insert(_ context.Context, k *domain.Kid) (int64, error) {
	id := int64(len(m.byID) + 1)
	cp := *k
	cp.ID = id
	m.byID[id] = &cp
	return id, nil
}

func (m *memAuthKidRepo) GetByID(_ context.Context, id int64) (*domain.Kid, error) {
	k, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return k, nil
}

func (m *memAuthKidRepo) List(_ context.Context, f domain.ListKidsFilter) ([]*domain.Kid, error) {
	out := make([]*domain.Kid, 0, len(m.byID))
	for _, k := range m.byID {
		if f.Status != 0 && k.Status != f.Status {
			continue
		}
		if f.Status == 0 && !f.IncludeDeleted && k.Status == domain.KidStatusDeleted {
			continue
		}
		if f.Grade > 0 && k.Grade != f.Grade {
			continue
		}
		out = append(out, k)
	}
	return out, nil
}

func (m *memAuthKidRepo) Update(_ context.Context, k *domain.Kid) error {
	cur, ok := m.byID[k.ID]
	if !ok {
		return domain.ErrNotFound
	}
	cur.Name, cur.Grade, cur.Gender, cur.Avatar = k.Name, k.Grade, k.Gender, k.Avatar
	return nil
}

func (m *memAuthKidRepo) SoftDelete(_ context.Context, id int64) error {
	cur, ok := m.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	cur.Status = domain.KidStatusDeleted
	return nil
}

func (m *memAuthKidRepo) Restore(_ context.Context, id int64) error {
	cur, ok := m.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	cur.Status = domain.KidStatusActive
	return nil
}

func (m *memAuthKidRepo) Graduate(_ context.Context, id int64) error {
	cur, ok := m.byID[id]
	if !ok || cur.Status != domain.KidStatusActive {
		return domain.ErrNotFound
	}
	cur.Status = domain.KidStatusGraduated
	return nil
}

func (m *memAuthKidRepo) ExistsByAccessCode(_ context.Context, code string) (bool, error) {
	for _, k := range m.byID {
		if k.AccessCode == code {
			return true, nil
		}
	}
	return false, nil
}

func (m *memAuthKidRepo) ExistsByName(_ context.Context, name string, excludeID int64) (bool, error) {
	for _, k := range m.byID {
		if k.ID != excludeID && k.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (m *memAuthKidRepo) GetByAccessCode(_ context.Context, code string) (*domain.Kid, error) {
	for _, k := range m.byID {
		if k.AccessCode == code && k.Status == domain.KidStatusActive {
			return k, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *memAuthKidRepo) UpdateAccessCode(_ context.Context, id int64, code string) error {
	cur, ok := m.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	cur.AccessCode = code
	return nil
}

func (m *memAuthKidRepo) UpdatePinHash(_ context.Context, id int64, hash string) error {
	cur, ok := m.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	cur.PinHash = hash
	cur.PinVersion++
	return nil
}

func testPinHash(t *testing.T, pin string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

type authEnv struct {
	e       *echo.Echo
	jwt     *auth.Store
	users   *memAuthUserRepo
	user    *domain.User
	kidRepo *memAuthKidRepo
}

// newAuthEnv gives each case its own environment so rate-limit buckets do not leak between cases.
func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	users := newMemAuthUserRepo()
	kids := newMemAuthKidRepo()

	user, err := users.Insert(context.Background(), &domain.User{
		DisplayName: "家长A",
		PinHash:     testPinHash(t, "1234"),
		PinVersion:  1,
		Status:      domain.UserStatusActive,
	})
	require.NoError(t, err)
	// Disabled account: must not appear in /auth/parent-accounts.
	_, err = users.Insert(context.Background(), &domain.User{
		DisplayName: "停用家长",
		PinHash:     testPinHash(t, "9999"),
		PinVersion:  1,
		Status:      0,
	})
	require.NoError(t, err)

	kid1, err := kids.Insert(context.Background(), &domain.Kid{
		Name:       "小明",
		Grade:      9,
		Status:     domain.KidStatusActive,
		PinHash:    testPinHash(t, "5678"),
		PinVersion: 1,
	})
	require.NoError(t, err)
	kid2, err := kids.Insert(context.Background(), &domain.Kid{
		Name:       "毕业",
		Grade:      6,
		Status:     domain.KidStatusGraduated,
		PinHash:    testPinHash(t, "1111"),
		PinVersion: 1,
	})
	require.NoError(t, err)
	_, err = kids.Insert(context.Background(), &domain.Kid{
		Name:       "被删",
		Grade:      7,
		Status:     domain.KidStatusDeleted,
		PinHash:    testPinHash(t, "2222"),
		PinVersion: 1,
	})
	require.NoError(t, err)
	// kid1/kid2 are already persisted at construction time (memAuthKidRepo.Insert wrote them), so the
	// test reads kidRepo directly by id; no second map needs to be kept in the env.
	_, _ = kid1, kid2

	store, err := auth.LoadOrCreate(t.TempDir())
	require.NoError(t, err)
	authSvc := service.NewAuthService(users, kids, store)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// The /api/me* group has three entry points: GET /me and POST /me/pin are on requireAny,
	// POST /me/rename is requireParent only (a kid token gets 401).
	requireAny := httpmw.RequireAnyAudience(log, store, authSvc)
	requireParent := httpmw.RequireAudience(log, store, authSvc, httpmw.AudienceParent)

	e := echo.New()
	g := e.Group("/api")
	// Existing endpoints registered (regression #16: still accessible without a token).
	NewKidHandler(service.NewKidService(kids, nil, nil, nil)).RegisterRoutes(g)
	NewUserHandler(service.NewUserService(users)).RegisterRoutes(g)
	NewAuthHandler(authSvc, requireAny, requireParent).RegisterRoutes(g)

	return &authEnv{
		e:       e,
		jwt:     store,
		users:   users,
		user:    &domain.User{ID: user, DisplayName: "家长A", PinVersion: 1, Status: domain.UserStatusActive},
		kidRepo: kids,
	}
}

func requestJSON(t *testing.T, e *echo.Echo, method, target, token string, payload map[string]any) (int, map[string]any) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		body = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, target, body)
	if payload != nil {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	}
	return rec.Code, out
}

func (env *authEnv) token(t *testing.T, typ string, sub int64, pv int, expired bool) string {
	t.Helper()
	when := time.Now()
	if expired {
		when = when.Add(-auth.TokenTTL - 2*time.Minute)
	}
	var (
		tok string
		err error
	)
	switch typ {
	case auth.TypParent:
		tok, err = env.jwt.SignParent(sub, "家长A", pv, when)
	case auth.TypKid:
		tok, err = env.jwt.SignKid(sub, pv, when)
	default:
		t.Fatal("bad typ", typ)
	}
	require.NoError(t, err)
	return tok
}

func TestAuthEndpoints(t *testing.T) {
	env := newAuthEnv(t)
	parentTok := env.token(t, auth.TypParent, env.user.ID, env.user.PinVersion, false)
	kidTok := env.token(t, auth.TypKid, 1, 1, false)
	cases := []struct {
		name       string
		method     string
		path       string
		token      string
		body       map[string]any
		wantStatus int
		wantCode   float64
		check      func(t *testing.T, body map[string]any)
	}{
		{
			name: "家长/孩子登录面", method: http.MethodPost, path: "/api/auth/login",
			body:       map[string]any{"user_id": 1, "pin": "1234"},
			wantStatus: http.StatusOK, wantCode: 0,
			check: func(t *testing.T, b map[string]any) {
				d := b["data"].(map[string]any)
				require.NotEmpty(t, d["token"])
				u := d["user"].(map[string]any)
				require.Equal(t, float64(1), u["id"])
				require.Equal(t, "家长A", u["display_name"])
			},
		},
		{
			name: "家长登录 PIN 错", method: http.MethodPost, path: "/api/auth/login",
			body:       map[string]any{"user_id": 1, "pin": "9999"},
			wantStatus: http.StatusUnauthorized, wantCode: CodeUnauthorized,
		},
		{
			name: "家长登录账号不存在", method: http.MethodPost, path: "/api/auth/login",
			body:       map[string]any{"user_id": 999, "pin": "1234"},
			wantStatus: http.StatusUnauthorized, wantCode: CodeUnauthorized,
		},
		{
			name: "家长登录缺 pin", method: http.MethodPost, path: "/api/auth/login",
			body:       map[string]any{"user_id": 1},
			wantStatus: http.StatusBadRequest, wantCode: CodeValidation,
		},
		{
			name: "孩子登录 PIN 对上", method: http.MethodPost, path: "/api/kid/auth/login",
			body:       map[string]any{"user_id": 1, "pin": "5678"},
			wantStatus: http.StatusOK, wantCode: 0,
			check: func(t *testing.T, b map[string]any) {
				d := b["data"].(map[string]any)
				require.NotEmpty(t, d["token"])
				k := d["kid"].(map[string]any)
				require.Equal(t, float64(1), k["id"])
				require.Equal(t, "小明", k["name"])
				require.Equal(t, float64(9), k["grade"])
			},
		},
		{
			name: "孩子登录 PIN 错", method: http.MethodPost, path: "/api/kid/auth/login",
			body:       map[string]any{"user_id": 1, "pin": "0000"},
			wantStatus: http.StatusUnauthorized, wantCode: CodeUnauthorized,
		},
		{
			name: "孩子登录账号软删", method: http.MethodPost, path: "/api/kid/auth/login",
			body:       map[string]any{"user_id": 3, "pin": "2222"},
			wantStatus: http.StatusUnauthorized, wantCode: CodeUnauthorized,
		},
		{
			name: "孩子登录缺 kid_id", method: http.MethodPost, path: "/api/kid/auth/login",
			body:       map[string]any{"pin": "5678"},
			wantStatus: http.StatusBadRequest, wantCode: CodeValidation,
		},
		{
			name: "/me 家长 token → MeParent", method: http.MethodGet, path: "/api/me", token: parentTok,
			wantStatus: http.StatusOK, wantCode: 0,
			check: func(t *testing.T, b map[string]any) {
				d := b["data"].(map[string]any)
				require.Equal(t, float64(1), d["id"])
				require.Equal(t, "家长A", d["display_name"])
			},
		},
		{
			name: "/me 孩子 token → MeKid", method: http.MethodGet, path: "/api/me", token: kidTok,
			wantStatus: http.StatusOK, wantCode: 0,
			check: func(t *testing.T, b map[string]any) {
				d := b["data"].(map[string]any)
				require.Equal(t, float64(1), d["id"])
				require.Equal(t, "小明", d["name"])
				require.Equal(t, float64(9), d["grade"])
			},
		},
		{
			name: "/me 无 token 拒", method: http.MethodGet, path: "/api/me",
			wantStatus: http.StatusUnauthorized, wantCode: CodeUnauthorized,
		},
		{
			name: "/me 过期 token 拒", method: http.MethodGet, path: "/api/me",
			token:      env.token(t, auth.TypParent, env.user.ID, 1, true),
			wantStatus: http.StatusUnauthorized, wantCode: CodeUnauthorized,
		},
		{
			name: "#16 回归：既有 /api/kids 不带 token 仍 200", method: http.MethodGet, path: "/api/kids",
			wantStatus: http.StatusOK, wantCode: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body := requestJSON(t, env.e, tc.method, tc.path, tc.token, tc.body)
			require.Equal(t, tc.wantStatus, code, "status（body=%v）", body)
			require.Equal(t, tc.wantCode, body["code"], "业务吗，body=%v", body)
			if tc.check != nil {
				tc.check(t, body)
			}
		})
	}
}

func TestAuthAccountsAndLogins(t *testing.T) {
	env := newAuthEnv(t)
	t.Run("accounts 只列 active", func(t *testing.T) {
		code, body := requestJSON(t, env.e, http.MethodGet, "/api/auth/parent-accounts", "", nil)
		require.Equal(t, http.StatusOK, code)
		items := body["data"].([]any)
		require.Len(t, items, 1)
		it := items[0].(map[string]any)
		require.Equal(t, float64(1), it["id"])
		require.Equal(t, "家长A", it["display_name"])
	})
	t.Run("auth/kid-accounts 排除软删", func(t *testing.T) {
		code, body := requestJSON(t, env.e, http.MethodGet, "/api/auth/kid-accounts", "", nil)
		require.Equal(t, http.StatusOK, code)
		items := body["data"].([]any)
		require.Len(t, items, 2) // active + graduated listed; soft-deleted excluded
	})
}

// TestAuthAccountsPinSet covers the P4 fix: /auth/parent-accounts exposes pin_set
// (empty string and placeholder "!" → false, a real bcrypt → true) and does not echo pin_hash.
func TestAuthAccountsPinSet(t *testing.T) {
	env := newAuthEnv(t)
	_, err := env.users.Insert(context.Background(), &domain.User{
		DisplayName: "无PIN家长", PinHash: "", PinVersion: 1, Status: domain.UserStatusActive,
	})
	require.NoError(t, err)
	_, err = env.users.Insert(context.Background(), &domain.User{
		DisplayName: "占位PIN家长", PinHash: service.PinPlaceholder, PinVersion: 1, Status: domain.UserStatusActive,
	})
	require.NoError(t, err)

	code, body := requestJSON(t, env.e, http.MethodGet, "/api/auth/parent-accounts", "", nil)
	require.Equal(t, http.StatusOK, code)
	items := body["data"].([]any)
	require.Len(t, items, 3) // with-PIN + no-PIN + placeholder-PIN; deactivated accounts filtered by service

	pinSet := map[string]bool{}
	for _, raw := range items {
		it, ok := raw.(map[string]any)
		require.True(t, ok)
		name, _ := it["display_name"].(string)
		pinSet[name], _ = it["pin_set"].(bool)
		_, hasHash := it["pin_hash"]
		require.False(t, hasHash, "accounts 响应不得包含 pin_hash")
	}
	require.True(t, pinSet["家长A"], "真实 bcrypt 哈希应 pin_set=true")
	require.False(t, pinSet["无PIN家长"], "空串哈希应 pin_set=false")
	require.False(t, pinSet["占位PIN家长"], "占位值 ! 应 pin_set=false")
}

// TestAuthLoginThrottle verifies lockout after 5 consecutive failures; the 6th attempt returns 429.
func TestAuthLoginThrottle(t *testing.T) {
	env := newAuthEnv(t)
	for i := 0; i < 5; i++ {
		code, _ := requestJSON(t, env.e, http.MethodPost, "/api/auth/login", "",
			map[string]any{"user_id": 1, "pin": "wrong"})
		require.Equal(t, http.StatusUnauthorized, code)
	}
	code, body := requestJSON(t, env.e, http.MethodPost, "/api/auth/login", "",
		map[string]any{"user_id": 1, "pin": "1234"})
	require.Equal(t, http.StatusTooManyRequests, code)
	require.Equal(t, float64(CodeLoginLocked), body["code"])
}

// TestAuthPinVersionMismatch verifies old tokens (stale pin_version) are immediately invalid
// after a PIN change.
func TestAuthPinVersionMismatch(t *testing.T) {
	env := newAuthEnv(t)
	env.users.byID[1].PinVersion = 2 // simulate a prior PIN change
	stale := env.token(t, auth.TypParent, env.user.ID, 1, false)
	code, body := requestJSON(t, env.e, http.MethodGet, "/api/me", stale, nil)
	require.Equal(t, http.StatusUnauthorized, code)
	require.Equal(t, float64(CodeUnauthorized), body["code"])
}

// TestAuthBogusToken verifies a forged token (not signed by this key) returns 401.
func TestAuthBogusToken(t *testing.T) {
	env := newAuthEnv(t)
	code, _ := requestJSON(t, env.e, http.MethodGet, "/api/me", "a.b.c", nil)
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestAuthMeParentRename(t *testing.T) {
	env := newAuthEnv(t)
	parentTok := env.token(t, auth.TypParent, env.user.ID, env.user.PinVersion, false)

	t.Run("成功改名并返回新昵称", func(t *testing.T) {
		code, body := requestJSON(t, env.e, http.MethodPost, "/api/me/rename", parentTok,
			map[string]any{"display_name": "爸爸"})
		require.Equal(t, http.StatusOK, code)
		d := body["data"].(map[string]any)
		require.Equal(t, float64(env.user.ID), d["id"])
		require.Equal(t, "爸爸", d["display_name"])
		// Persisted in the repo: GET /api/me also returns the new name.
		code, body = requestJSON(t, env.e, http.MethodGet, "/api/me", parentTok, nil)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "爸爸", body["data"].(map[string]any)["display_name"])
	})

	t.Run("撞已有昵称 40013", func(t *testing.T) {
		code, body := requestJSON(t, env.e, http.MethodPost, "/api/me/rename", parentTok,
			map[string]any{"display_name": "停用家长"})
		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, float64(CodeDuplicateName), body["code"])
	})

	t.Run("空昵称 40010", func(t *testing.T) {
		code, body := requestJSON(t, env.e, http.MethodPost, "/api/me/rename", parentTok,
			map[string]any{"display_name": "  "})
		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, float64(CodeInvalidInput), body["code"])
	})

	t.Run("无 token 401", func(t *testing.T) {
		code, _ := requestJSON(t, env.e, http.MethodPost, "/api/me/rename", "",
			map[string]any{"display_name": "爸爸"})
		require.Equal(t, http.StatusUnauthorized, code)
	})
}

// Each subtest uses its own env: changing the PIN bumps pin_version, so sharing an env would make
// later subtests' "issued with old pv" tokens invalid for all of them — interference.
func TestAuthMeParentPin(t *testing.T) {
	t.Run("改 PIN 成功：返回新 token，旧 token 因 pv 失效", func(t *testing.T) {
		env := newAuthEnv(t)
		parentTok := env.token(t, auth.TypParent, env.user.ID, env.user.PinVersion, false)
		code, body := requestJSON(t, env.e, http.MethodPost, "/api/me/pin", parentTok,
			map[string]any{"old_pin": "1234", "new_pin": "4321"})
		require.Equal(t, http.StatusOK, code)
		newTok, _ := body["data"].(map[string]any)["token"].(string)
		require.NotEmpty(t, newTok, "必须返回新 JWT（方案 B 无缝续用）")

		code, body = requestJSON(t, env.e, http.MethodGet, "/api/me", parentTok, nil)
		require.Equal(t, http.StatusUnauthorized, code)
		require.Equal(t, float64(CodeUnauthorized), body["code"])
		code, _ = requestJSON(t, env.e, http.MethodGet, "/api/me", newTok, nil)
		require.Equal(t, http.StatusOK, code)
		code, _ = requestJSON(t, env.e, http.MethodPost, "/api/auth/login", "",
			map[string]any{"user_id": env.user.ID, "pin": "4321"})
		require.Equal(t, http.StatusOK, code)
	})

	t.Run("旧 PIN 错误返回 400 不触发清 token（登录态业务校验）", func(t *testing.T) {
		env := newAuthEnv(t)
		tok := env.token(t, auth.TypParent, env.user.ID, env.user.PinVersion, false)
		code, body := requestJSON(t, env.e, http.MethodPost, "/api/me/pin", tok,
			map[string]any{"old_pin": "9999", "new_pin": "4321"})
		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, float64(CodeInvalidInput), body["code"])
	})

	t.Run("新 PIN 格式非法 400", func(t *testing.T) {
		env := newAuthEnv(t)
		tok := env.token(t, auth.TypParent, env.user.ID, env.user.PinVersion, false)
		code, body := requestJSON(t, env.e, http.MethodPost, "/api/me/pin", tok,
			map[string]any{"old_pin": "1234", "new_pin": "abc"})
		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, float64(CodeInvalidInput), body["code"])
	})

	t.Run("无 token 401", func(t *testing.T) {
		env := newAuthEnv(t)
		code, _ := requestJSON(t, env.e, http.MethodPost, "/api/me/pin", "",
			map[string]any{"old_pin": "1234", "new_pin": "4321"})
		require.Equal(t, http.StatusUnauthorized, code)
	})
}

// TestAuthMeRenameKidTokenRejected covers the contract that POST /api/me/rename is only mounted to
// requireParent: a kid token hits the middleware and returns 401 (not 404, not 400).
func TestAuthMeRenameKidTokenRejected(t *testing.T) {
	env := newAuthEnv(t)
	kidTok := env.token(t, auth.TypKid, 1, 1, false)
	code, body := requestJSON(t, env.e, http.MethodPost, "/api/me/rename", kidTok,
		map[string]any{"display_name": "孩子改名"})
	require.Equal(t, http.StatusUnauthorized, code)
	require.Equal(t, float64(CodeUnauthorized), body["code"])
}

// TestAuthKidChangePin covers the kid branch of POST /api/me/pin
// (AuthService.ChangeKidPin): old-PIN check, pin_version bump invalidates old tokens, and the new
// JWT continues seamlessly.
// Each subtest uses its own env: changing the PIN bumps pin_version, sharing an env would interfere.
func TestAuthKidChangePin(t *testing.T) {
	t.Run("改 PIN 成功：返回新 token，旧 token 因 pv 失效", func(t *testing.T) {
		env := newAuthEnv(t)
		kidTok := env.token(t, auth.TypKid, 1, 1, false)
		code, body := requestJSON(t, env.e, http.MethodPost, "/api/me/pin", kidTok,
			map[string]any{"old_pin": "5678", "new_pin": "8765"})
		require.Equal(t, http.StatusOK, code)
		newTok, _ := body["data"].(map[string]any)["token"].(string)
		require.NotEmpty(t, newTok, "必须返回新 JWT（无缝续用）")

		code, body = requestJSON(t, env.e, http.MethodGet, "/api/me", kidTok, nil)
		require.Equal(t, http.StatusUnauthorized, code)
		require.Equal(t, float64(CodeUnauthorized), body["code"])
		code, body = requestJSON(t, env.e, http.MethodGet, "/api/me", newTok, nil)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "小明", body["data"].(map[string]any)["name"])
		code, _ = requestJSON(t, env.e, http.MethodPost, "/api/kid/auth/login", "",
			map[string]any{"user_id": 1, "pin": "8765"})
		require.Equal(t, http.StatusOK, code)
	})

	t.Run("旧 PIN 错误返回 400 不触发清 token", func(t *testing.T) {
		env := newAuthEnv(t)
		kidTok := env.token(t, auth.TypKid, 1, 1, false)
		code, body := requestJSON(t, env.e, http.MethodPost, "/api/me/pin", kidTok,
			map[string]any{"old_pin": "9999", "new_pin": "8765"})
		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, float64(CodeInvalidInput), body["code"])
	})

	// The placeholder always fails VerifyPin, returning the same route error as a wrong old PIN
	// (ErrPinMismatch -> 400 CodeInvalidInput), so no token derived from the placeholder can ever
	// set a PIN (security hole closed).
	t.Run("占位值（未设 PIN）拒绝，不得免验设 PIN", func(t *testing.T) {
		env := newAuthEnv(t)
		kidID, err := env.kidRepo.Insert(context.Background(), &domain.Kid{
			Name: "无PIN", Grade: 9, Status: domain.KidStatusActive,
			PinVersion: 1, PinHash: service.PinPlaceholder,
		})
		require.NoError(t, err)
		tok := env.token(t, auth.TypKid, kidID, 1, false)
		code, body := requestJSON(t, env.e, http.MethodPost, "/api/me/pin", tok,
			map[string]any{"old_pin": "", "new_pin": "2468"})
		require.Equal(t, http.StatusBadRequest, code, "占位值必须被拒，body=%v", body)
		require.Equal(t, float64(CodeInvalidInput), body["code"])
		require.NotContains(t, body, "token", "拒绝时不得签发新 JWT")

		// Repo untouched: the placeholder is not rewritten and pin_version is not bumped.
		k, err := env.kidRepo.GetByID(context.Background(), kidID)
		require.NoError(t, err)
		require.Equal(t, service.PinPlaceholder, k.PinHash)
		require.Equal(t, 1, k.PinVersion)
	})

	t.Run("无 token 401", func(t *testing.T) {
		env := newAuthEnv(t)
		code, _ := requestJSON(t, env.e, http.MethodPost, "/api/me/pin", "",
			map[string]any{"old_pin": "5678", "new_pin": "8765"})
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("家长 token 走 parent 分支不串台", func(t *testing.T) {
		env := newAuthEnv(t)
		parentTok := env.token(t, auth.TypParent, env.user.ID, env.user.PinVersion, false)
		code, _ := requestJSON(t, env.e, http.MethodPost, "/api/me/pin", parentTok,
			map[string]any{"old_pin": "1234", "new_pin": "4321"})
		require.Equal(t, http.StatusOK, code)
	})
}

// TestAuthOldPaths404 verifies the path consolidation: all 9 legacy paths now return a real 404.
// Assert the status code, not the business code — these routes no longer exist, so echo returns
// a plain 404 without the envelope, and tokens must be ignored (middleware is route-scoped, so a
// missing route never reaches the middleware anyway).
func TestAuthOldPaths404(t *testing.T) {
	env := newAuthEnv(t)
	parentTok := env.token(t, auth.TypParent, env.user.ID, env.user.PinVersion, false)
	kidTok := env.token(t, auth.TypKid, 1, 1, false)
	cases := []struct {
		method string
		path   string
		token  string
	}{
		{http.MethodGet, "/api/users/me", parentTok},
		{http.MethodPut, "/api/users/me", parentTok},
		{http.MethodPost, "/api/users/me/pin", parentTok},
		{http.MethodGet, "/api/me/parent", parentTok},
		{http.MethodGet, "/api/me/kid", kidTok},
		{http.MethodPost, "/api/me/parent/rename", parentTok},
		{http.MethodPost, "/api/me/parent/pin", parentTok},
		{http.MethodGet, "/api/users/accounts", ""},
		{http.MethodGet, "/api/kid/logins", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			env.e.ServeHTTP(rec, req)
			require.Equal(t, http.StatusNotFound, rec.Code, "旧路径必须真 404，body=%q", rec.Body.String())
		})
	}
}

func (m *memAuthUserRepo) WithTx(tx domain.Tx) domain.UserRepository { return m }

func (m *memAuthKidRepo) WithTx(tx domain.Tx) domain.KidRepository { return m }
