package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"paperly/internal/auth"
	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/service"
)

type memSubjectRepo struct {
	rows     []*domain.Subject
	examRefs map[int64]int64 // subject id -> exam count
	kpRefs   map[int64]int64 // subject id -> knowledge point count
}

func (m *memSubjectRepo) CountExams(_ context.Context, id int64) (int64, error) {
	return m.examRefs[id], nil
}

func (m *memSubjectRepo) CountKnowledgePoints(_ context.Context, id int64) (int64, error) {
	return m.kpRefs[id], nil
}

func (m *memSubjectRepo) ImportSeed(context.Context, []*domain.Subject) (int64, error) {
	return 0, nil
}

func (m *memSubjectRepo) List(context.Context) ([]*domain.Subject, error) { return m.rows, nil }

func (m *memSubjectRepo) GetByID(_ context.Context, id int64) (*domain.Subject, error) {
	for _, s := range m.rows {
		if s.ID == id {
			return s, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *memSubjectRepo) Insert(_ context.Context, s *domain.Subject) (int64, error) {
	m.rows = append(m.rows, s)
	return s.ID, nil
}

func (m *memSubjectRepo) UpdateName(_ context.Context, id int64, name string) (int64, error) {
	s, err := m.GetByID(context.Background(), id)
	if err != nil {
		return 0, err
	}
	s.Name = name
	return 1, nil
}

func (m *memSubjectRepo) ListByStageBit(_ context.Context, bit int) ([]*domain.Subject, error) {
	out := make([]*domain.Subject, 0, len(m.rows))
	for _, s := range m.rows {
		if s.GradeMask&bit != 0 {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *memSubjectRepo) ListFiltered(_ context.Context, stageBit, trackBit int) ([]*domain.Subject, error) {
	out := make([]*domain.Subject, 0, len(m.rows))
	for _, s := range m.rows {
		if stageBit != 0 && s.GradeMask&stageBit == 0 {
			continue
		}
		if trackBit != 0 && s.GradeMask&trackBit == 0 {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (m *memSubjectRepo) ListByIDs(_ context.Context, ids []int64) ([]*domain.Subject, error) {
	want := make(map[int64]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make([]*domain.Subject, 0, len(ids))
	for _, s := range m.rows {
		if want[s.ID] {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *memSubjectRepo) UpdateMask(_ context.Context, id int64, mask int) (int64, error) {
	s, err := m.GetByID(context.Background(), id)
	if err != nil {
		return 0, err
	}
	s.GradeMask = mask
	return 1, nil
}

func (m *memSubjectRepo) Delete(_ context.Context, id int64) (int64, error) {
	for i, s := range m.rows {
		if s.ID == id {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

type memKPRow struct {
	id   int64
	subj int64
	name string
}

type memKPRepo struct {
	rows   []memKPRow
	nextID int64
}

func (m *memKPRepo) Insert(_ context.Context, kp *domain.KnowledgePoint) (int64, error) {
	m.nextID++
	m.rows = append(m.rows, memKPRow{id: m.nextID, subj: kp.SubjectID, name: kp.Name})
	return m.nextID, nil
}

func (m *memKPRepo) UpdateName(_ context.Context, id int64, name string) (int64, error) {
	for i := range m.rows {
		if m.rows[i].id == id {
			m.rows[i].name = name
			return 1, nil
		}
	}
	return 0, nil
}

func (m *memKPRepo) Delete(_ context.Context, id int64) (int64, error) {
	for i, r := range m.rows {
		if r.id == id {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func (m *memKPRepo) GetByID(_ context.Context, id int64) (*domain.KnowledgePoint, error) {
	for _, r := range m.rows {
		if r.id == id {
			return &domain.KnowledgePoint{ID: r.id, SubjectID: r.subj, Name: r.name}, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *memKPRepo) ListBySubject(_ context.Context, subjectID int64) ([]*domain.KnowledgePoint, error) {
	out := []*domain.KnowledgePoint{}
	for _, r := range m.rows {
		if r.subj == subjectID {
			out = append(out, &domain.KnowledgePoint{ID: r.id, SubjectID: r.subj, Name: r.name})
		}
	}
	return out, nil
}

type memUserRepo struct {
	user *domain.User
}

func (m *memUserRepo) GetFirst(context.Context) (*domain.User, error) {
	if m.user == nil {
		return nil, domain.ErrNotFound
	}
	cp := *m.user
	return &cp, nil
}

func (m *memUserRepo) GetByID(_ context.Context, id int64) (*domain.User, error) {
	if m.user == nil || m.user.ID != id {
		return nil, domain.ErrNotFound
	}
	cp := *m.user
	return &cp, nil
}

func (m *memUserRepo) List(_ context.Context) ([]*domain.User, error) {
	if m.user == nil {
		return nil, nil
	}
	return []*domain.User{{ID: m.user.ID, DisplayName: m.user.DisplayName, PinHash: m.user.PinHash, Status: m.user.Status}}, nil
}

func (m *memUserRepo) Insert(_ context.Context, u *domain.User) (int64, error) {
	if m.user != nil && m.user.DisplayName == u.DisplayName {
		return 0, domain.ErrDuplicateDisplayName
	}
	clone := *u
	clone.ID = 1
	m.user = &clone
	return 1, nil
}

func (m *memUserRepo) ExistsByDisplayName(_ context.Context, name string, excludeID int64) (bool, error) {
	if m.user == nil || excludeID == m.user.ID {
		return false, nil
	}
	return m.user.DisplayName == name, nil
}

func (m *memUserRepo) UpdateDisplayName(_ context.Context, _ int64, name string) (int64, error) {
	m.user.DisplayName = name
	return 1, nil
}

func (m *memUserRepo) UpdatePinHash(_ context.Context, _ int64, hash string) (int64, error) {
	m.user.PinHash = hash
	m.user.PinVersion++
	return 1, nil
}

type memTaskRepo struct {
	retryCalls int
	getStatus  int
}

func (m *memTaskRepo) List(context.Context, domain.ListTasksFilter) ([]*domain.Task, error) {
	return []*domain.Task{}, nil
}

func (m *memTaskRepo) CountByStatus(context.Context) (map[int]int, error) {
	return map[int]int{}, nil
}

func (m *memTaskRepo) GetByID(_ context.Context, id int64) (*domain.Task, error) {
	return &domain.Task{ID: id, Status: m.getStatus}, nil
}

func (m *memTaskRepo) Retry(context.Context, int64) (int64, error) {
	m.retryCalls++
	return 1, nil
}

func (m *memTaskRepo) ClearFinished(context.Context) (int64, error) { return 0, nil }

// The next 6 methods are the worker-facing contract; the api layer never calls them,
// so placeholders suffice.
func (m *memTaskRepo) Insert(context.Context, *domain.Task) (int64, error) { return 0, nil }
func (m *memTaskRepo) Claim(context.Context) (*domain.Task, error)         { return nil, domain.ErrNotFound }
func (m *memTaskRepo) Finish(context.Context, int64) error                 { return nil }
func (m *memTaskRepo) Skip(context.Context, int64, string) error           { return nil }
func (m *memTaskRepo) Fail(context.Context, int64, string, bool) error     { return nil }
func (m *memTaskRepo) ClearOlderThan(context.Context, int) (int64, error)  { return 0, nil }

type settingsEnv struct {
	echo *echo.Echo
	subj *memSubjectRepo
	kp   *memKPRepo
	user *memUserRepo
	// tok is the JWT of the preset parent (id=1, pv=1); after consolidation the /api/me* routes
	// all require auth, so the rename/PIN assertions must hit the new paths with a Bearer token.
	tok string
}

func newSettingsEnv(t *testing.T) *settingsEnv {
	t.Helper()
	subj := &memSubjectRepo{rows: []*domain.Subject{
		{ID: 1, Code: "math", Name: "数学", SortOrder: 1, Source: domain.SubjectSourceSystem},
		{ID: 2, Code: "u1", Name: "奥数", SortOrder: 2, Source: domain.SubjectSourceUser},
	}}
	kp := &memKPRepo{}
	user := &memUserRepo{user: &domain.User{ID: 1, DisplayName: "家长管理员", PinVersion: 1, Status: domain.UserStatusActive}}
	taskRepo := &memTaskRepo{}

	jwtStore, err := auth.LoadOrCreate(t.TempDir())
	require.NoError(t, err)
	// The kid repo only satisfies AuthService wiring; this group never uses the kid surface.
	authSvc := service.NewAuthService(user, newMemAuthKidRepo(), jwtStore)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	requireAny := httpmw.RequireAnyAudience(log, jwtStore, authSvc)
	requireParent := httpmw.RequireAudience(log, jwtStore, authSvc, httpmw.AudienceParent)
	tok, err := jwtStore.SignParent(1, "家长管理员", 1, time.Now())
	require.NoError(t, err)

	e := echo.New()
	g := e.Group("/api")
	NewSubjectHandler(service.NewSubjectService(subj)).RegisterRoutes(g)
	NewKnowledgePointHandler(service.NewKnowledgePointService(kp, subj)).RegisterRoutes(g)
	NewUserHandler(service.NewUserService(user)).RegisterRoutes(g)
	NewTaskHandler(service.NewTaskService(taskRepo)).RegisterRoutes(g)
	// Session reads/writes of one's self converge on /api/me*; assertions run through the authed paths.
	NewAuthHandler(authSvc, requireAny, requireParent).RegisterRoutes(g)
	return &settingsEnv{echo: e, subj: subj, kp: kp, user: user, tok: tok}
}

// doSettingsJSON shares a name but not a signature with doJSON in kid_handler_test.go,
// hence the prefix.
func doSettingsJSON(t *testing.T, e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	return doSettingsTokenJSON(t, e, method, path, "", body)
}

// doSettingsTokenJSON is doSettingsJSON with a Bearer token (for the authed /api/me* paths).
func doSettingsTokenJSON(t *testing.T, e *echo.Echo, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func decodeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, CodeOK, out.Code, "body=%s", rec.Body.String())
	return out.Data
}

func decodeErr(t *testing.T, rec *httptest.ResponseRecorder) errorResponse {
	t.Helper()
	var out errorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

// decodeDataList decodes a {code, data:[...]} envelope (list endpoints).
func decodeDataList(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var out struct {
		Code int              `json:"code"`
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, CodeOK, out.Code, "body=%s", rec.Body.String())
	return out.Data
}

func TestSubjectRoutesRegistered(t *testing.T) {
	env := newSettingsEnv(t)
	got := map[string]bool{}
	for _, r := range env.echo.Router().Routes() {
		got[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"GET /api/subjects", "POST /api/subjects",
		"PUT /api/subjects/:id", "DELETE /api/subjects/:id",
	} {
		require.True(t, got[want], "路由未注册: %s", want)
	}
}

func TestSubjectCreateReturnsTrimmedName(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsJSON(t, env.echo, http.MethodPost, "/api/subjects", `{"name":"  科学  "}`)
	require.Equal(t, http.StatusOK, rec.Code)
	data := decodeData(t, rec)
	require.Equal(t, "科学", data["name"], "回显必须是落库后的名字")
	require.NotNil(t, data["id"])
}

func TestSubjectCreateRejectsBlank(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsJSON(t, env.echo, http.MethodPost, "/api/subjects", `{"name":"   "}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeInvalidInput, decodeErr(t, rec).Code)
}

func TestSubjectUpdateAllowsSystemSubjectRename(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsJSON(t, env.echo, http.MethodPut, "/api/subjects/1", `{"name":"数学（深）"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "数学（深）", decodeData(t, rec)["name"])
}

func TestSubjectListFiltersByGrade(t *testing.T) {
	env := newSettingsEnv(t)
	env.subj.rows[0].GradeMask = domain.GradeMaskAll
	env.subj.rows[1].GradeMask = domain.StagePrimary

	// No grade → full dictionary (backward-compatible).
	rec := doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, decodeDataList(t, rec), 2)

	// grade=8 (junior) → only the all-stage subject survives the mask filter.
	rec = doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects?grade=8", "")
	require.Equal(t, http.StatusOK, rec.Code)
	rows := decodeDataList(t, rec)
	require.Len(t, rows, 1)
	require.Equal(t, "数学", rows[0]["name"])

	// grade=3 (primary) → both are visible.
	rec = doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects?grade=3", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, decodeDataList(t, rec), 2)
}

func TestSubjectListFiltersByTrack(t *testing.T) {
	env := newSettingsEnv(t)
	// row0: arts (junior+senior); row1: science (junior+senior). Senior-capable so the
	// combined grade+track case can exercise the track filter at grade >= 10.
	env.subj.rows[0].GradeMask = domain.StageJunior | domain.StageSenior | domain.TrackArts
	env.subj.rows[1].GradeMask = domain.StageJunior | domain.StageSenior | domain.TrackScience

	// track=arts → only the arts row.
	rec := doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects?track=1", "")
	require.Equal(t, http.StatusOK, rec.Code)
	rows := decodeDataList(t, rec)
	require.Len(t, rows, 1)
	require.Equal(t, "数学", rows[0]["name"])

	// track=science → the other row.
	rec = doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects?track=2", "")
	require.Len(t, decodeDataList(t, rec), 1)

	// track=0 → no track filter, full dictionary.
	rec = doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects?track=0", "")
	require.Len(t, decodeDataList(t, rec), 2)

	// grade below senior forces track OFF → both junior+senior rows survive.
	rec = doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects?grade=8&track=2", "")
	require.Len(t, decodeDataList(t, rec), 2)

	// grade >= senior + track → track takes effect → the science row only.
	rec = doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects?grade=10&track=2", "")
	require.Len(t, decodeDataList(t, rec), 1)

	rec = doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects?track=x", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSubjectListRejectsBadGrade(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects?grade=abc", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeValidation, decodeErr(t, rec).Code)
}

func TestSubjectUpdateMask(t *testing.T) {
	env := newSettingsEnv(t)
	env.subj.rows[1].Name = "奥数"

	rec := doSettingsJSON(t, env.echo, http.MethodPut, "/api/subjects/2", `{"mask":1}`)
	require.Equal(t, http.StatusOK, rec.Code)
	data := decodeData(t, rec)
	require.Equal(t, float64(1), data["mask"])
	require.Equal(t, "奥数", data["name"], "仅改 mask 不得清空名字")

	// track bits (high 3) are editable through the same mask field.
	rec = doSettingsJSON(t, env.echo, http.MethodPut, "/api/subjects/2", `{"mask":24}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, float64(24), decodeData(t, rec)["mask"], "文+理 track bits 可写回")

	// mask 8 (arts track bit) is valid now; 64 (bit6, reserved) is rejected.
	rec = doSettingsJSON(t, env.echo, http.MethodPut, "/api/subjects/2", `{"mask":64}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeInvalidInput, decodeErr(t, rec).Code)
}

func TestSubjectDeleteRejectsSystemSubject(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsJSON(t, env.echo, http.MethodDelete, "/api/subjects/1", "")
	require.Equal(t, http.StatusBadRequest, rec.Code, "预置科目禁删要在 API 层就拦")
	require.Len(t, env.subj.rows, 2)
}

func TestSubjectDeleteUserSubject(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsJSON(t, env.echo, http.MethodDelete, "/api/subjects/2", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, env.subj.rows, 1)
}

func TestSubjectRejectsNonNumericID(t *testing.T) {
	env := newSettingsEnv(t)
	for _, path := range []string{"/api/subjects/abc", "/api/subjects/0", "/api/subjects/-1"} {
		rec := doSettingsJSON(t, env.echo, http.MethodDelete, path, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, "路径 %s 应被拒", path)
	}
}

func TestKnowledgePointRoutesRegistered(t *testing.T) {
	env := newSettingsEnv(t)
	got := map[string]bool{}
	for _, r := range env.echo.Router().Routes() {
		got[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"GET /api/subjects/:subjectId/knowledge_points",
		"POST /api/subjects/:subjectId/knowledge_points",
		"PUT /api/subjects/:subjectId/knowledge_points/:id",
		"DELETE /api/subjects/:subjectId/knowledge_points/:id",
	} {
		require.True(t, got[want], "路由未注册: %s", want)
	}
}

func TestKnowledgePointCrudRoundTrip(t *testing.T) {
	env := newSettingsEnv(t)

	rec := doSettingsJSON(t, env.echo, http.MethodPost, "/api/subjects/2/knowledge_points", `{"name":"  鸡兔同笼  "}`)
	require.Equal(t, http.StatusOK, rec.Code)
	id := int64(decodeData(t, rec)["id"].(float64))
	require.Equal(t, "鸡兔同笼", decodeData(t, rec)["name"])

	rec = doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects/2/knowledge_points", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list struct {
		Data []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Data, 1)
	require.Equal(t, "鸡兔同笼", list.Data[0].Name)

	rec = doSettingsJSON(t, env.echo, http.MethodPut,
		"/api/subjects/2/knowledge_points/"+strconv.FormatInt(id, 10), `{"name":"鸡兔同笼（提高）"}`)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = doSettingsJSON(t, env.echo, http.MethodDelete, "/api/subjects/2/knowledge_points/"+strconv.FormatInt(id, 10), "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, env.kp.rows)
}

// TestKnowledgePointCrossSubjectWriteRejected is the key cross-subject authorization test:
// the path targets subject=1 (preset math) but the id belongs to subject=2; it must 404 and
// change nothing.
func TestKnowledgePointCrossSubjectWriteRejected(t *testing.T) {
	env := newSettingsEnv(t)
	_, err := env.kp.Insert(context.Background(), &domain.KnowledgePoint{SubjectID: 2, Name: "时态"})
	require.NoError(t, err)

	rec := doSettingsJSON(t, env.echo, http.MethodPut, "/api/subjects/1/knowledge_points/1", `{"name":"改错科目"}`)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, CodeNotFound, decodeErr(t, rec).Code)
	require.Equal(t, "时态", env.kp.rows[0].name, "越权请求不得改到数据")

	rec = doSettingsJSON(t, env.echo, http.MethodDelete, "/api/subjects/1/knowledge_points/1", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Len(t, env.kp.rows, 1, "越权删除不得生效")
}

func TestKnowledgePointListUnknownSubject(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects/999/knowledge_points", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestKnowledgePointRejectsNonNumericSubjectID(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsJSON(t, env.echo, http.MethodGet, "/api/subjects/abc/knowledge_points", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// GET /api/me returns MeParent (id + display_name) without a pin_set field — one boolean less
// exposed than the old UserSummary. The no-pin_hash / no-hash-prefix leak assertions stay as-is.
func TestUserMeNeverLeaksPinHash(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsTokenJSON(t, env.echo, http.MethodGet, "/api/me", env.tok, "")
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	require.NotContains(t, body, "pin_hash", "凭据字段连名字都不该出现")
	require.NotContains(t, body, "$2a$")

	data := decodeData(t, rec)
	require.Equal(t, "家长管理员", data["display_name"])
}

func TestUserUpdateDisplayName(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsTokenJSON(t, env.echo, http.MethodPost, "/api/me/rename", env.tok, `{"display_name":" 王妈妈 "}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "王妈妈", decodeData(t, rec)["display_name"])
}

func TestUserUpdateDisplayNameBlank(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsTokenJSON(t, env.echo, http.MethodPost, "/api/me/rename", env.tok, `{"display_name":"  "}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeInvalidInput, decodeErr(t, rec).Code)
}

// Setting a PIN goes through the parent branch of POST /api/me/pin. The response returns {token}
// (a new JWT with pin_version+1) instead of UserSummary; the "no echo of PIN/hash" invariant holds,
// and "actually set" is asserted on the repo storing a bcrypt hash.
func TestUserUpdatePinSetsFlagWithoutEchoing(t *testing.T) {
	env := newSettingsEnv(t)
	require.Empty(t, env.user.user.PinHash, "预置家长应未设 PIN")

	rec := doSettingsTokenJSON(t, env.echo, http.MethodPost, "/api/me/pin", env.tok, `{"new_pin":"246810"}`)
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	require.NotContains(t, body, "246810", "PIN 不得回显")
	require.NotContains(t, body, "$2a$")
	require.NotEmpty(t, decodeData(t, rec)["token"], "改 PIN 应换发新 JWT（方案 B 无缝续用）")
	require.True(t, strings.HasPrefix(env.user.user.PinHash, "$2"), "仓储应落 bcrypt 哈希（pin_set 语义）")
	require.Equal(t, 2, env.user.user.PinVersion, "pin_version 应 +1")
}

func TestUserUpdatePinRejectsBadFormat(t *testing.T) {
	env := newSettingsEnv(t)
	for _, pin := range []string{"123", "1234567", "abcd"} {
		rec := doSettingsTokenJSON(t, env.echo, http.MethodPost, "/api/me/pin", env.tok, `{"new_pin":"`+pin+`"}`)
		require.Equal(t, http.StatusBadRequest, rec.Code, "PIN %s 应被拒", pin)
	}
}

// Account-deleted window: the token still exists but the account is gone from the DB. GET /api/me
// sits behind the requireAny middleware, so liveness is rejected there with 401 (the handler's 404
// branch is unreachable) — still "neither 500 nor loggable", and account existence is not leaked.
func TestUserMeNotFoundWhenNoAccount(t *testing.T) {
	userRepo := &memUserRepo{}
	jwtStore, err := auth.LoadOrCreate(t.TempDir())
	require.NoError(t, err)
	authSvc := service.NewAuthService(userRepo, newMemAuthKidRepo(), jwtStore)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := echo.New()
	NewAuthHandler(authSvc,
		httpmw.RequireAnyAudience(log, jwtStore, authSvc),
		httpmw.RequireAudience(log, jwtStore, authSvc, httpmw.AudienceParent)).
		RegisterRoutes(e.Group("/api"))
	staleTok, err := jwtStore.SignParent(1, "幽灵", 1, time.Now())
	require.NoError(t, err)

	rec := doSettingsTokenJSON(t, e, http.MethodGet, "/api/me", staleTok, "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, CodeUnauthorized, decodeErr(t, rec).Code)
}

func TestUserRoutesRegistered(t *testing.T) {
	env := newSettingsEnv(t)
	got := map[string]bool{}
	for _, r := range env.echo.Router().Routes() {
		got[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"GET /api/users",
		"POST /api/users",
		"GET /api/me",
		"POST /api/me/pin",
		"POST /api/me/rename",
	} {
		require.True(t, got[want], "路由未注册: %s", want)
	}
	// Legacy paths must stay absent (guard against regressions re-mounting them).
	for _, gone := range []string{
		"GET /api/users/me",
		"PUT /api/users/me",
		"POST /api/users/me/pin",
	} {
		require.False(t, got[gone], "旧路径不应再注册: %s", gone)
	}
}

func TestUserCreateRoundTrip(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsJSON(t, env.echo, http.MethodPost, "/api/users", `{"display_name":" 爸爸 ","pin":"123456"}`)
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	require.NotContains(t, body, "123456", "PIN 不得回显")
	require.NotContains(t, body, "$2a$")
	data := decodeData(t, rec)
	require.Equal(t, "爸爸", data["display_name"])
	require.Equal(t, true, data["pin_set"])
	require.NotNil(t, data["status"])
}

func TestUserCreateRejectsDuplicate(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsJSON(t, env.echo, http.MethodPost, "/api/users", `{"display_name":"家长管理员"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeDuplicateName, decodeErr(t, rec).Code, "与既有家长重名必须 40013")
}

func TestUserCreateRejectsInvalidInput(t *testing.T) {
	env := newSettingsEnv(t)
	for _, payload := range []string{`{"display_name":"  "}`, `{"display_name":"妈妈","pin":"123"}`} {
		rec := doSettingsJSON(t, env.echo, http.MethodPost, "/api/users", payload)
		require.Equal(t, http.StatusBadRequest, rec.Code, "载荷 %s 应被拒", payload)
		require.Equal(t, CodeInvalidInput, decodeErr(t, rec).Code)
	}
}

func TestUserList(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsJSON(t, env.echo, http.MethodGet, "/api/users", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Data, 1)
	require.Equal(t, "家长管理员", list.Data[0]["display_name"])
	require.NotContains(t, rec.Body.String(), "pin_hash")
}

func TestTaskRoutesRegistered(t *testing.T) {
	env := newSettingsEnv(t)
	got := map[string]bool{}
	for _, r := range env.echo.Router().Routes() {
		got[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"GET /api/tasks", "GET /api/tasks/:id",
		"POST /api/tasks/:id/retry", "POST /api/tasks/clear-finished",
	} {
		require.True(t, got[want], "路由未注册: %s", want)
	}
}

func TestTaskListRejectsUnknownStatus(t *testing.T) {
	env := newSettingsEnv(t)
	rec := doSettingsJSON(t, env.echo, http.MethodGet, "/api/tasks?status=9", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTaskRetryNonFailedRejected(t *testing.T) {
	e := echo.New()
	NewTaskHandler(service.NewTaskService(&memTaskRepo{getStatus: domain.TaskStatusRunning})).
		RegisterRoutes(e.Group("/api"))
	// Retrying a running task would replay a backup that has already finished; it must be 400.
	rec := doSettingsJSON(t, e, http.MethodPost, "/api/tasks/1/retry", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, CodeInvalidInput, decodeErr(t, rec).Code)
}

func TestTaskClearFinished(t *testing.T) {
	e := echo.New()
	NewTaskHandler(service.NewTaskService(&memTaskRepo{})).RegisterRoutes(e.Group("/api"))

	rec := doSettingsJSON(t, e, http.MethodPost, "/api/tasks/clear-finished", "")
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestSettingsErrorsMapToCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", domain.ErrNotFound, CodeNotFound},
		{"invalid input", domain.ErrInvalidInput, CodeInvalidInput},
		{"unknown", errors.New("boom"), CodeInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			// fail* only writes the response body and returns nil (the error is already in the
			// response), so only the status code is asserted.
			require.NoError(t, failSubject(c, tc.err))
			require.Equal(t, tc.want, decodeErr(t, rec).Code)
		})
	}
}

func (m *memSubjectRepo) WithTx(tx domain.Tx) domain.SubjectRepository { return m }

func (m *memKPRepo) WithTx(tx domain.Tx) domain.KnowledgePointRepository { return m }

func (m *memUserRepo) WithTx(tx domain.Tx) domain.UserRepository { return m }

func (m *memTaskRepo) WithTx(tx domain.Tx) domain.TaskRepository { return m }
