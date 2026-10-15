package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	"paperly/internal/service"
)

// memKidSubjectRepo is an in-memory fake of domain.KidSubjectRepository for handler tests.
type memKidSubjectRepo struct {
	byKid map[int64][]int64
}

func newMemKidSubjectRepo() *memKidSubjectRepo {
	return &memKidSubjectRepo{byKid: map[int64][]int64{}}
}

func (m *memKidSubjectRepo) ListSubjectIDs(_ context.Context, kidID int64) ([]int64, error) {
	return append([]int64(nil), m.byKid[kidID]...), nil
}

func (m *memKidSubjectRepo) ReplaceAll(_ context.Context, kidID int64, subjectIDs []int64) error {
	m.byKid[kidID] = append([]int64(nil), subjectIDs...)
	return nil
}

func newKidSubjectEnv(t *testing.T) (*echo.Echo, *memKidSubjectRepo) {
	t.Helper()
	kids := newMemAuthKidRepo()
	kids.byID[1] = &domain.Kid{ID: 1, Name: "小明", Grade: 9, Status: domain.KidStatusActive}
	subjects := &memSubjectRepo{rows: []*domain.Subject{
		{ID: 1, Code: "chinese", Name: "语文", SortOrder: 1, Mandatory: 1, GradeMask: domain.MaskAll},
		{ID: 2, Code: "math", Name: "数学", SortOrder: 2, Mandatory: 1, GradeMask: domain.MaskAll},
		{ID: 3, Code: "physics", Name: "物理", SortOrder: 3, GradeMask: domain.StageJunior | domain.TrackScience},
	}}
	kidSub := newMemKidSubjectRepo()
	e := echo.New()
	g := e.Group("/api")
	NewKidSubjectHandler(service.NewKidSubjectService(kids, subjects, kidSub)).RegisterRoutes(g)
	NewSubjectHandler(service.NewSubjectServiceWithKids(subjects, kids, kidSub)).RegisterRoutes(g)
	return e, kidSub
}

func TestKidSubjectHandlerListEmptyWhenUnconfigured(t *testing.T) {
	e, _ := newKidSubjectEnv(t)
	rec := doSettingsJSON(t, e, http.MethodGet, "/api/kids/1/subjects", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, decodeDataList(t, rec))
}

func TestKidSubjectHandlerSetThenList(t *testing.T) {
	e, _ := newKidSubjectEnv(t)

	rec := doSettingsJSON(t, e, http.MethodPut, "/api/kids/1/subjects", `{"subject_ids":[1,2,3]}`)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = doSettingsJSON(t, e, http.MethodGet, "/api/kids/1/subjects", "")
	require.Equal(t, http.StatusOK, rec.Code)
	rows := decodeDataList(t, rec)
	require.Len(t, rows, 3)
}

func TestKidSubjectHandlerSetAddsMandatory(t *testing.T) {
	e, kidSub := newKidSubjectEnv(t)

	// subject_ids omits both mandatory subjects: the server force-includes them and the
	// PUT still succeeds, so a stale/buggy client can never drop a core subject.
	rec := doSettingsJSON(t, e, http.MethodPut, "/api/kids/1/subjects", `{"subject_ids":[3]}`)
	require.Equal(t, http.StatusOK, rec.Code)

	// Whitelist is repaired to include 语文/数学 alongside the requested physics.
	require.ElementsMatch(t, []int64{1, 2, 3}, kidSub.byKid[1])

	rec = doSettingsJSON(t, e, http.MethodGet, "/api/kids/1/subjects", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, decodeDataList(t, rec), 3)
}

func TestKidSubjectHandlerListKidNotFound(t *testing.T) {
	e, _ := newKidSubjectEnv(t)
	rec := doSettingsJSON(t, e, http.MethodGet, "/api/kids/999/subjects", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, CodeNotFound, decodeErr(t, rec).Code)
}

// TestSubjectHandlerListByKid: ?kid_id resolves the whitelist when configured.
func TestSubjectHandlerListByKid(t *testing.T) {
	e, kidSub := newKidSubjectEnv(t)
	kidSub.byKid[1] = []int64{1}

	rec := doSettingsJSON(t, e, http.MethodGet, "/api/subjects?kid_id=1", "")
	require.Equal(t, http.StatusOK, rec.Code)
	rows := decodeDataList(t, rec)
	require.Len(t, rows, 1)
	require.Equal(t, float64(1), rows[0]["id"])
	require.Equal(t, float64(1), rows[0]["mandatory"])
}

func TestSubjectHandlerListByKidInvalid(t *testing.T) {
	e, _ := newKidSubjectEnv(t)
	rec := doSettingsJSON(t, e, http.MethodGet, "/api/subjects?kid_id=abc", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func (m *memKidSubjectRepo) WithTx(tx domain.Tx) domain.KidSubjectRepository { return m }
