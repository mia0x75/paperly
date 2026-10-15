package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"paperly/internal/auth"
	"paperly/internal/domain"
	"paperly/internal/service"
)

// inviteEnv assembles the kid invite-flow endpoints: /kid/invite/verify + /setup.
// The endpoints are anonymously accessible and only rate-limited by source IP—access_code
// is the invitation credential and must not be logged.
func newInviteEnv(t *testing.T) *echo.Echo {
	t.Helper()
	kids := newMemAuthKidRepo()
	_, err := kids.Insert(context.Background(), &domain.Kid{
		Name:       "小明",
		Grade:      9,
		Status:     domain.KidStatusActive,
		AccessCode: "abc123",
		PinHash:    service.PinPlaceholder,
		PinVersion: 1,
	})
	require.NoError(t, err)

	store, err := auth.LoadOrCreate(t.TempDir())
	require.NoError(t, err)
	authSvc := service.NewAuthService(newMemAuthUserRepo(), kids, store)

	e := echo.New()
	g := e.Group("/api")
	NewKidInviteHandler(authSvc).RegisterRoutes(g)
	return e
}

func inviteJSON(e *echo.Echo, path string, payload string) (int, map[string]any) {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func TestInviteVerifyValidCode(t *testing.T) {
	e := newInviteEnv(t)
	status, body := inviteJSON(e, "/api/kid/invite/verify", `{"code":"abc123"}`)
	require.Equal(t, http.StatusOK, status)
	data := body["data"].(map[string]any)
	require.Equal(t, true, data["valid"])
	require.Equal(t, "小明", data["name"])
	_, hasKidID := data["kid_id"]
	require.False(t, hasKidID, "verify 不得返回 kid_id，避免提前暴露「码→孩子」绑定")
}

func TestInviteVerifyInvalidCodeIsStill200(t *testing.T) {
	e := newInviteEnv(t)
	status, body := inviteJSON(e, "/api/kid/invite/verify", `{"code":"nope1234"}`)
	require.Equal(t, http.StatusOK, status, "无效码返回 200 {valid:false}，不向探询者区分存在性")
	data := body["data"].(map[string]any)
	require.Equal(t, false, data["valid"])
	_, hasName := data["name"]
	require.False(t, hasName)
}

func TestInviteVerifyMissingCodeIsInvalid(t *testing.T) {
	e := newInviteEnv(t)
	// An empty code and an invalid code share the same semantics: 200 {valid:false}.
	// The probe does not distinguish "no code given" from "wrong code"; the frontend
	// always shows "link invalid".
	status, body := inviteJSON(e, "/api/kid/invite/verify", `{}`)
	require.Equal(t, http.StatusOK, status)
	data := body["data"].(map[string]any)
	require.Equal(t, false, data["valid"])
}

func TestInviteSetupSuccess(t *testing.T) {
	e := newInviteEnv(t)
	status, body := inviteJSON(e, "/api/kid/invite/setup", `{"code":"abc123","pin":"6666"}`)
	require.Equal(t, http.StatusOK, status, "body=%v", body)
	data := body["data"].(map[string]any)
	require.NotEmpty(t, data["token"])
	kd := data["kid"].(map[string]any)
	require.Equal(t, "小明", kd["name"])
	require.NotZero(t, kd["id"])
	// The service layer separately covers "can log in right after setup"; here we only
	// verify the response contract.
}

func TestInviteSetupInvalidOrUnknownRejected(t *testing.T) {
	e := newInviteEnv(t)
	status, _ := inviteJSON(e, "/api/kid/invite/setup", `{"code":"abc123","pin":""}`)
	require.Equal(t, http.StatusBadRequest, status, "空 PIN 应 400")

	status, body := inviteJSON(e, "/api/kid/invite/setup", `{"code":"nope1234","pin":"6666"}`)
	require.Equal(t, http.StatusBadRequest, status)
	require.Contains(t, body["message"], "invalid or expired access code",
		"未知码与不可用码统一文案，状态枚举不区分")
}

func TestInviteRateLimitedPerIP(t *testing.T) {
	e := newInviteEnv(t)
	// 5 requests/minute pass; the 6th from the same IP gets 429 (verify and setup
	// share one bucket).
	for i := 0; i < 5; i++ {
		status, _ := inviteJSON(e, "/api/kid/invite/verify", `{"code":"nope1234"}`)
		require.Equal(t, http.StatusOK, status, "第 %d 次应放行", i+1)
	}
	status, body := inviteJSON(e, "/api/kid/invite/setup", `{"code":"abc123","pin":"6666"}`)
	require.Equal(t, http.StatusTooManyRequests, status, "body=%v", body)
	require.Contains(t, body["message"], "too many attempts, please retry later")
}
