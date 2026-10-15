package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"paperly/internal/auth"
	"paperly/internal/domain"
	"paperly/internal/service"
)

// onbMemKidRepo is the in-memory kid repo for onboarding handler tests; only the
// paths the handler actually touches are implemented.
type onbMemKidRepo struct {
	kids []*domain.Kid
}

func (m *onbMemKidRepo) GetByID(context.Context, int64) (*domain.Kid, error) {
	return nil, domain.ErrNotFound
}
func (m *onbMemKidRepo) Insert(_ context.Context, k *domain.Kid) (int64, error) {
	m.kids = append(m.kids, k)
	return int64(len(m.kids)), nil
}
func (m *onbMemKidRepo) List(context.Context, domain.ListKidsFilter) ([]*domain.Kid, error) {
	return m.kids, nil
}
func (m *onbMemKidRepo) Update(context.Context, *domain.Kid) error                { return nil }
func (m *onbMemKidRepo) Graduate(context.Context, int64) error                    { return nil }
func (m *onbMemKidRepo) UpdatePinHash(context.Context, int64, string) error       { return nil }
func (m *onbMemKidRepo) SoftDelete(context.Context, int64) error                  { return nil }
func (m *onbMemKidRepo) Restore(context.Context, int64) error                     { return nil }
func (m *onbMemKidRepo) UpdateAccessCode(context.Context, int64, string) error    { return nil }
func (m *onbMemKidRepo) ExistsByAccessCode(context.Context, string) (bool, error) { return false, nil }
func (m *onbMemKidRepo) ExistsByName(context.Context, string, int64) (bool, error) {
	return false, nil
}
func (m *onbMemKidRepo) GetByAccessCode(context.Context, string) (*domain.Kid, error) {
	return nil, domain.ErrNotFound
}

type onbMemCursorRepo struct {
	c *domain.MaintenanceCursor
}

func (m *onbMemCursorRepo) Get(_ context.Context, _ string) (*domain.MaintenanceCursor, error) {
	if m.c == nil {
		return nil, domain.ErrNotFound
	}
	return m.c, nil
}
func (m *onbMemCursorRepo) Upsert(_ context.Context, c *domain.MaintenanceCursor) error {
	m.c = c
	return nil
}
func (m *onbMemCursorRepo) List(context.Context) ([]*domain.MaintenanceCursor, error) {
	if m.c == nil {
		return nil, nil
	}
	return []*domain.MaintenanceCursor{m.c}, nil
}

// includeDefaultUser controls whether a parent is pre-seeded (simulates first run /
// already profiled).
// The handler relies on authSvc to issue the default token, so tests use a real JWT key.
func newOnbHandlerEnv(t *testing.T, includeDefaultUser bool, kids []*domain.Kid) (*echo.Echo, *onbMemCursorRepo) {
	userRepo := &memUserRepo{}
	if includeDefaultUser {
		userRepo.user = &domain.User{ID: 1, DisplayName: "妈妈", PinVersion: 1, Status: domain.UserStatusActive}
	}
	cursorRepo := &onbMemCursorRepo{}
	kidRepo := &onbMemKidRepo{kids: kids}
	svc := service.NewOnboardingService(userRepo, kidRepo, cursorRepo)

	jwtStore, err := auth.LoadOrCreate(t.TempDir())
	require.NoError(t, err)
	authSvc := service.NewAuthService(userRepo, kidRepo, jwtStore)

	e := echo.New()
	NewOnboardingHandler(svc, authSvc).RegisterRoutes(e.Group("/api"))
	return e, cursorRepo
}

func TestOnboardingRoutesRegistered(t *testing.T) {
	e, _ := newOnbHandlerEnv(t, false, nil)
	got := map[string]bool{}
	for _, r := range e.Router().Routes() {
		got[r.Method+" "+r.Path] = true
	}
	require.True(t, got["GET /api/onboarding/status"])
	require.True(t, got["POST /api/onboarding/complete"])
}

func TestOnboardingStatusNotDoneOnEmpty(t *testing.T) {
	e, _ := newOnbHandlerEnv(t, false, nil)
	rec := doSettingsJSON(t, e, http.MethodGet, "/api/onboarding/status", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, false, decodeData(t, rec)["done"])
}

func TestOnboardingStatusDoneWhenAllPresent(t *testing.T) {
	e, cursorRepo := newOnbHandlerEnv(t, true, []*domain.Kid{{ID: 1, Name: "小明"}})
	now := time.Now()
	cursorRepo.c = &domain.MaintenanceCursor{
		TaskName:   domain.CursorTaskOnboardingDone,
		LastStatus: domain.CursorStatusCompleted,
		LastRunAt:  &now,
	}

	rec := doSettingsJSON(t, e, http.MethodGet, "/api/onboarding/status", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, true, decodeData(t, rec)["done"])
}

func TestOnboardingCompleteWritesMarker(t *testing.T) {
	e, cursorRepo := newOnbHandlerEnv(t, true, []*domain.Kid{{ID: 1, Name: "小明"}})
	rec := doSettingsJSON(t, e, http.MethodPost, "/api/onboarding/complete", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, true, decodeData(t, rec)["done"])
	require.NotNil(t, cursorRepo.c)
	require.Equal(t, domain.CursorTaskOnboardingDone, cursorRepo.c.TaskName)
	require.Equal(t, domain.CursorStatusCompleted, cursorRepo.c.LastStatus)
}

func TestOnboardingCompleteIssuesToken(t *testing.T) {
	e, _ := newOnbHandlerEnv(t, true, []*domain.Kid{{ID: 1, Name: "小明"}})
	rec := doSettingsJSON(t, e, http.MethodPost, "/api/onboarding/complete", "")
	require.Equal(t, http.StatusOK, rec.Code)
	d := decodeData(t, rec)
	require.Equal(t, true, d["done"])
	require.NotEmpty(t, d["token"], "完成引导应签发默认家长 JWT")
	u, ok := d["user"].(map[string]any)
	require.True(t, ok, "应返回签发对象 user")
	require.Equal(t, "妈妈", u["display_name"])
}

func TestOnboardingCompleteRejectsEmptyState(t *testing.T) {
	// No parent: must not pass the completion gate.
	e, _ := newOnbHandlerEnv(t, false, nil)
	rec := doSettingsJSON(t, e, http.MethodPost, "/api/onboarding/complete", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeInvalidState, decodeErr(t, rec).Code)

	e2, _ := newOnbHandlerEnv(t, true, nil)
	rec = doSettingsJSON(t, e2, http.MethodPost, "/api/onboarding/complete", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeInvalidState, decodeErr(t, rec).Code)
}

// TestOnboardingCompleteReplayGuard covers the replay safety gate:
// POST /api/onboarding/complete is only allowed for "first onboarding" and "reset scenarios";
// when onboarding is already a complete terminal state (marker present + both users/kids
// non-empty) it rejects with 409, closing the anonymous-replay entry to swap for a parent JWT.
func TestOnboardingCompleteReplayGuard(t *testing.T) {
	setDone := func(cursorRepo *onbMemCursorRepo) {
		now := time.Now()
		cursorRepo.c = &domain.MaintenanceCursor{
			TaskName:   domain.CursorTaskOnboardingDone,
			LastStatus: domain.CursorStatusCompleted,
			LastRunAt:  &now,
		}
	}

	t.Run("场景1 无标记 + users/kids 齐：首次引导放行 200", func(t *testing.T) {
		e, _ := newOnbHandlerEnv(t, true, []*domain.Kid{{ID: 1, Name: "小明"}})
		rec := doSettingsJSON(t, e, http.MethodPost, "/api/onboarding/complete", "")
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, true, decodeData(t, rec)["done"])
	})

	t.Run("场景2 标记在 + users 空：重置场景过安全闸（不 409）", func(t *testing.T) {
		// After the safety gate passes, the existing pre-check returns 400 invalid_state
		// (users=0 while a reset rebuild is incomplete). Key assertion: it does **not**
		// return 409—i.e. it is not blocked by the "repeat completion" gate.
		e, cursorRepo := newOnbHandlerEnv(t, false, []*domain.Kid{{ID: 1, Name: "小明"}})
		setDone(cursorRepo)
		rec := doSettingsJSON(t, e, http.MethodPost, "/api/onboarding/complete", "")
		require.NotEqual(t, http.StatusConflict, rec.Code, "users 空不属重放，禁走 409")
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, CodeInvalidState, decodeErr(t, rec).Code)
	})

	t.Run("场景3 标记在 + kids 空：重置场景过安全闸（不 409）", func(t *testing.T) {
		e, cursorRepo := newOnbHandlerEnv(t, true, nil)
		setDone(cursorRepo)
		rec := doSettingsJSON(t, e, http.MethodPost, "/api/onboarding/complete", "")
		require.NotEqual(t, http.StatusConflict, rec.Code, "kids 空不属重放，禁走 409")
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, CodeInvalidState, decodeErr(t, rec).Code)
	})

	t.Run("场景4 标记在 + users/kids 齐：重放拒绝 409", func(t *testing.T) {
		e, cursorRepo := newOnbHandlerEnv(t, true, []*domain.Kid{{ID: 1, Name: "小明"}})
		setDone(cursorRepo)
		rec := doSettingsJSON(t, e, http.MethodPost, "/api/onboarding/complete", "")
		require.Equal(t, http.StatusConflict, rec.Code)
		er := decodeErr(t, rec)
		require.Equal(t, CodeConflict, er.Code)
		require.NotEmpty(t, er.Message)
	})

	t.Run("场景4 连打两次：首次 200、第二次 409", func(t *testing.T) {
		// End-to-end reproduction of the attack surface: anonymously complete one real
		// onboarding, then replay it verbatim.
		e, _ := newOnbHandlerEnv(t, true, []*domain.Kid{{ID: 1, Name: "小明"}})
		rec := doSettingsJSON(t, e, http.MethodPost, "/api/onboarding/complete", "")
		require.Equal(t, http.StatusOK, rec.Code)

		rec = doSettingsJSON(t, e, http.MethodPost, "/api/onboarding/complete", "")
		require.Equal(t, http.StatusConflict, rec.Code, "第二次必须 409")
		require.Equal(t, CodeConflict, decodeErr(t, rec).Code)
		// The replay must not issue a token again.
		require.NotContains(t, rec.Body.String(), `"token"`)
	})
}

func (m *onbMemKidRepo) WithTx(tx domain.Tx) domain.KidRepository { return m }

func (m *onbMemCursorRepo) WithTx(tx domain.Tx) domain.MaintenanceCursorRepository { return m }
