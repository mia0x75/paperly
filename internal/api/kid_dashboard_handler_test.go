package api

import (
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

	"paperly/internal/auth"
	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/service"
)

// kidDashEnv assembles the minimal kid-side /kid/dashboard environment: a real JWT
// Store plus middleware.
type kidDashEnv struct {
	jwt   *auth.Store
	kidID int64
}

// newKidDashEnv returns an echo instance and a signing key.
// The kid ID comes from the token claim, not a query param — a credential-driven
// session.
func newKidDashEnv(t *testing.T) (*kidDashEnv, *echo.Echo) {
	t.Helper()
	kids := newMemAuthKidRepo()
	red := &domain.Kid{
		Name:       "小红",
		Grade:      8,
		Status:     domain.KidStatusActive,
		PinHash:    testPinHash(t, "5678"),
		PinVersion: 1,
	}
	kidID, err := kids.Insert(context.Background(), red)
	require.NoError(t, err)

	store, err := auth.LoadOrCreate(t.TempDir())
	require.NoError(t, err)
	authSvc := service.NewAuthService(newMemAuthUserRepo(), kids, store)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	requireKid := httpmw.RequireAudience(log, store, authSvc, httpmw.AudienceKid)

	svc := service.NewDashboardService(&dashRepoStub{
		counts: domain.ScopeCounts{ExamTotal: 5, ExamPages: 11},
	}, kidsWith(&domain.Kid{ID: kidID, Name: "小红", Status: domain.KidStatusActive}), fixedTestNow)

	e := echo.New()
	g := e.Group("/api")
	g.GET("/kid/dashboard", NewDashboardHandler(svc).KidOverview, requireKid)
	return &kidDashEnv{jwt: store, kidID: kidID}, e
}

func (env *kidDashEnv) kidToken(t *testing.T, kidID int64, pv int) string {
	t.Helper()
	tok, err := env.jwt.SignKid(kidID, pv, time.Now())
	require.NoError(t, err)
	return tok
}

func serveKidDashboard(e *echo.Echo, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/kid/dashboard", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestKidDashboardReturnsKidCountsWithoutGlobalData(t *testing.T) {
	env, e := newKidDashEnv(t)
	rec := serveKidDashboard(e, env.kidToken(t, env.kidID, 1))
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	var out dashboardEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	// The kid side has no global-data authorization: the payload carries this kid's
	// counts only, and the contract has no global denominator at all.
	require.Equal(t, 5, out.Data.Exams.Total)
	require.Equal(t, 11, out.Data.ExamPages)
	if body := rec.Body.String(); containsAllKey(body) {
		t.Fatalf("孩子端响应含 all 键: %s", body)
	}
	// Credentials never enter the response: access_code / pin_hash must not be echoed back.
	for _, forbidden := range []string{"access_code", "pin_hash"} {
		require.NotContains(t, rec.Body.String(), forbidden)
	}
}

func TestKidDashboardRequiresToken(t *testing.T) {
	_, e := newKidDashEnv(t)
	rec := serveKidDashboard(e, "")
	require.Equal(t, http.StatusUnauthorized, rec.Code, "无 token 必须 401")
}

func TestKidDashboardRejectsParentToken(t *testing.T) {
	env, e := newKidDashEnv(t)
	tok, err := env.jwt.SignParent(1, "家长A", 1, time.Now())
	require.NoError(t, err)
	rec := serveKidDashboard(e, tok)
	require.Equal(t, http.StatusUnauthorized, rec.Code, "家长 token 打孩子端必须 401")
}

func TestKidDashboardRejectsStalePinVersion(t *testing.T) {
	env, e := newKidDashEnv(t)
	rec := serveKidDashboard(e, env.kidToken(t, env.kidID, 9))
	require.Equal(t, http.StatusUnauthorized, rec.Code, "pin_version 过期 token 必须 401")
}

func TestKidDashboardRejectsUnknownKid(t *testing.T) {
	env, e := newKidDashEnv(t)
	rec := serveKidDashboard(e, env.kidToken(t, 42, 1))
	require.Equal(t, http.StatusUnauthorized, rec.Code, "认证表里不存在的孩子必须 401")
}
