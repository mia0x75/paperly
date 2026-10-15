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

	"paperly/internal/domain"
	"paperly/internal/service"
)

// apiTestRepo is the minimal in-memory repo double for API-layer black-box tests.
type apiTestRepo struct {
	byID   map[int64]*domain.Kid
	nextID int64
}

func newAPITestRepo() *apiTestRepo {
	return &apiTestRepo{byID: map[int64]*domain.Kid{}}
}

func (r *apiTestRepo) Insert(_ context.Context, s *domain.Kid) (int64, error) {
	r.nextID++
	clone := *s
	clone.ID = r.nextID
	r.byID[r.nextID] = &clone
	return r.nextID, nil
}

func (r *apiTestRepo) GetByID(_ context.Context, id int64) (*domain.Kid, error) {
	s, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return s, nil
}

func (r *apiTestRepo) List(_ context.Context, f domain.ListKidsFilter) ([]*domain.Kid, error) {
	out := make([]*domain.Kid, 0)
	for _, s := range r.byID {
		if f.Status != 0 && s.Status != f.Status {
			continue
		}
		if f.Status == 0 && !f.IncludeDeleted && s.Status == domain.KidStatusDeleted {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (r *apiTestRepo) Update(_ context.Context, s *domain.Kid) error {
	cur, ok := r.byID[s.ID]
	if !ok {
		return domain.ErrNotFound
	}
	// Mirror the real repo's full-row mutable update (name/grade/gender/avatar/track).
	cur.Name = s.Name
	cur.Grade = s.Grade
	cur.Gender = s.Gender
	cur.Avatar = s.Avatar
	cur.Track = s.Track
	return nil
}

func (r *apiTestRepo) SoftDelete(_ context.Context, id int64) error {
	cur, ok := r.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	cur.Status = domain.KidStatusDeleted
	return nil
}

func (r *apiTestRepo) ExistsByAccessCode(_ context.Context, code string) (bool, error) {
	for _, s := range r.byID {
		if s.AccessCode == code {
			return true, nil
		}
	}
	return false, nil
}

func (r *apiTestRepo) Restore(_ context.Context, id int64) error {
	s, ok := r.byID[id]
	if !ok || s.Status != domain.KidStatusDeleted {
		return domain.ErrNotFound
	}
	s.Status = domain.KidStatusActive
	return nil
}

func (r *apiTestRepo) Graduate(_ context.Context, id int64) error {
	s, ok := r.byID[id]
	if !ok || s.Status != domain.KidStatusActive {
		return domain.ErrNotFound
	}
	s.Status = domain.KidStatusGraduated
	return nil
}

func (r *apiTestRepo) ExistsByName(_ context.Context, name string, excludeID int64) (bool, error) {
	for _, s := range r.byID {
		if s.ID != excludeID && s.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (r *apiTestRepo) GetByAccessCode(_ context.Context, code string) (*domain.Kid, error) {
	for _, s := range r.byID {
		if s.AccessCode == code && s.Status == domain.KidStatusActive {
			return s, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *apiTestRepo) UpdateAccessCode(_ context.Context, id int64, code string) error {
	s, ok := r.byID[id]
	if !ok || s.Status == domain.KidStatusDeleted {
		return domain.ErrNotFound
	}
	s.AccessCode = code
	return nil
}

func (r *apiTestRepo) UpdatePinHash(_ context.Context, id int64, hash string) error {
	s, ok := r.byID[id]
	if !ok || s.Status == domain.KidStatusDeleted {
		return domain.ErrNotFound
	}
	s.PinHash = hash
	s.PinVersion++
	return nil
}

func newTestServer() *echo.Echo {
	e := echo.New()
	h := NewKidHandler(service.NewKidService(newAPITestRepo(), nil, nil, nil))
	h.RegisterRoutes(e.Group("/api"))
	return e
}

func doJSON(t *testing.T, e *echo.Echo, method, target, body string) (int, map[string]any) {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var parsed map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	}
	return rec.Code, parsed
}

func TestAPICreateKid(t *testing.T) {
	e := newTestServer()
	status, resp := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"小明","grade":9,"pin":"1234"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])

	data, ok := resp["data"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "小明", data["name"])
	code, _ := data["access_code"].(string)
	require.Len(t, code, domain.AccessCodeLength)
	require.Equal(t, "/invite/"+code, data["kid_url"])

	// Security contract (ADR 0002): no response may expose pin_hash.
	raw := postRaw(t, e, "/api/kids", `{"name":"小亮","grade":9,"pin":"4321"}`)
	require.NotContains(t, raw, "pin_hash", "响应不得包含 pin_hash")
	require.NotContains(t, raw, "PinHash", "响应不得包含 PinHash")
}

// postRaw returns the raw response body so leak assertions can search the text.
func postRaw(t *testing.T, e *echo.Echo, target, body string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Body.String()
}

func TestAPICreateKidEmptyName(t *testing.T) {
	e := newTestServer()
	status, resp := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":""}`)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, float64(CodeInvalidInput), resp["code"])
}

func TestAPICreateKidDuplicateName(t *testing.T) {
	e := newTestServer()
	status, _ := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"小明","grade":9}`)
	require.Equal(t, http.StatusOK, status)

	status, resp := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"小明","grade":8}`)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, float64(CodeDuplicateName), resp["code"])
}

func TestAPICreateKidInvalidBody(t *testing.T) {
	e := newTestServer()
	status, resp := doJSON(t, e, http.MethodPost, "/api/kids", `{not-json`)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, float64(CodeInvalidBody), resp["code"])
}

func TestAPIListKids(t *testing.T) {
	e := newTestServer()
	_, _ = doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"A","grade":9}`)
	_, _ = doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"B","grade":9}`)

	status, resp := doJSON(t, e, http.MethodGet, "/api/kids", "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])
	list, ok := resp["data"].([]any)
	require.True(t, ok)
	require.Len(t, list, 2)
}

func TestAPIGetKidAndNotFound(t *testing.T) {
	e := newTestServer()
	raw := postRaw(t, e, "/api/kids", `{"name":"小明","grade":9,"pin":"1234"}`)

	// Extract the id from the raw JSON (black-box, not relying on internal structure).
	var createResp map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &createResp))
	data := createResp["data"].(map[string]any)
	id := int(data["id"].(float64))

	status, resp := doJSON(t, e, http.MethodGet, "/api/kids/"+itoa(id), "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])
	require.Equal(t, float64(9), resp["data"].(map[string]any)["grade"])

	// The detail view exposes neither access_code (ADR 0002: prevents resharing) nor pin_hash.
	detailJSON := getRaw(t, e, "/api/kids/"+itoa(id))
	require.NotContains(t, detailJSON, "access_code", "详情不得返回 access_code")
	require.NotContains(t, detailJSON, "pin_hash", "详情不得返回 pin_hash")
	_ = resp

	status, resp = doJSON(t, e, http.MethodGet, "/api/kids/99999", "")
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, float64(CodeNotFound), resp["code"])

	status, resp = doJSON(t, e, http.MethodGet, "/api/kids/abc", "")
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, float64(CodeValidation), resp["code"])
}

func getRaw(t *testing.T, e *echo.Echo, target string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Body.String()
}

func TestAPIUpdateKid(t *testing.T) {
	e := newTestServer()
	_, createResp := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"旧","grade":9}`)
	id := int(createResp["data"].(map[string]any)["id"].(float64))

	status, resp := doJSON(t, e, http.MethodPut, "/api/kids/"+itoa(id), `{"name":"新","grade":9}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])
	require.Equal(t, "新", resp["data"].(map[string]any)["name"])
}

func TestAPIUpdateKidTrack(t *testing.T) {
	e := newTestServer()
	_, createResp := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"小明","grade":10}`)
	id := int(createResp["data"].(map[string]any)["id"].(float64))

	status, resp := doJSON(t, e, http.MethodPut, "/api/kids/"+itoa(id), `{"name":"小明","grade":10,"track":2}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(2), resp["data"].(map[string]any)["track"])

	_, getResp := doJSON(t, e, http.MethodGet, "/api/kids/"+itoa(id), "")
	require.Equal(t, float64(2), getResp["data"].(map[string]any)["track"])

	status, resp = doJSON(t, e, http.MethodPut, "/api/kids/"+itoa(id), `{"name":"小明","grade":10,"track":5}`)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, float64(CodeInvalidInput), resp["code"])
}

func TestAPIUpdateKidDuplicateName(t *testing.T) {
	e := newTestServer()
	_, _ = doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"小明","grade":9}`)
	_, bResp := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"小红","grade":8}`)
	bID := int(bResp["data"].(map[string]any)["id"].(float64))

	status, resp := doJSON(t, e, http.MethodPut, "/api/kids/"+itoa(bID), `{"name":"小明","grade":8}`)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, float64(CodeDuplicateName), resp["code"])
}

func TestAPIDeleteKidSoftDelete(t *testing.T) {
	e := newTestServer()
	_, createResp := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"A","grade":9}`)
	id := int(createResp["data"].(map[string]any)["id"].(float64))

	status, _ := doJSON(t, e, http.MethodDelete, "/api/kids/"+itoa(id), "")
	require.Equal(t, http.StatusNoContent, status)

	_, listResp := doJSON(t, e, http.MethodGet, "/api/kids", "")
	require.Empty(t, listResp["data"].([]any))
}

func TestAPIRestoreKid(t *testing.T) {
	e := newTestServer()
	_, createResp := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"A","grade":9}`)
	id := int(createResp["data"].(map[string]any)["id"].(float64))

	status, _ := doJSON(t, e, http.MethodDelete, "/api/kids/"+itoa(id), "")
	require.Equal(t, http.StatusNoContent, status)

	status, resp := doJSON(t, e, http.MethodPost, "/api/kids/"+itoa(id)+"/restore", "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])
	require.Equal(t, float64(domain.KidStatusActive), resp["data"].(map[string]any)["status"])
}

func TestAPIListKidsIncludeDeleted(t *testing.T) {
	e := newTestServer()
	_, createResp := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"A","grade":9}`)
	id := int(createResp["data"].(map[string]any)["id"].(float64))
	_, _ = doJSON(t, e, http.MethodDelete, "/api/kids/"+itoa(id), "")

	_, def := doJSON(t, e, http.MethodGet, "/api/kids", "")
	require.Empty(t, def["data"].([]any), "默认应排除已删除")

	_, all := doJSON(t, e, http.MethodGet, "/api/kids?include_deleted=true", "")
	require.Len(t, all["data"].([]any), 1, "include_deleted=true 应含已删除")

	_, all2 := doJSON(t, e, http.MethodGet, "/api/kids?status=all", "")
	require.Len(t, all2["data"].([]any), 1, "status=all 等价")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func createKidForReset(t *testing.T, e *echo.Echo) (int64, string) {
	t.Helper()
	_, resp := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"小明","grade":9}`)
	data := resp["data"].(map[string]any)
	id := int64(data["id"].(float64))
	return id, data["access_code"].(string)
}

func TestAPIResetAccessCode(t *testing.T) {
	e := newTestServer()
	id, oldCode := createKidForReset(t, e)

	status, resp := doJSON(t, e, http.MethodPost,
		"/api/kids/"+itoa(int(id))+"/access_code/reset", "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])

	data := resp["data"].(map[string]any)
	newCode := data["access_code"].(string)
	require.NotEqual(t, oldCode, newCode, "应返回新码")
	require.Len(t, newCode, domain.AccessCodeLength)
	require.EqualValues(t, id, data["kid_id"])
	require.Equal(t, "/invite/"+newCode, data["kid_url"])

	// The end-to-end assertion that the old code is invalidated lives in the
	// service layer (repo.GetByAccessCode(old)=ErrNotFound); here we only confirm
	// the route returns the new code.
}

func TestAPIResetAccessCodeNotFound(t *testing.T) {
	e := newTestServer()
	status, resp := doJSON(t, e, http.MethodPost, "/api/kids/999/access_code/reset", "")
	require.Equal(t, http.StatusNotFound, status)
	require.EqualValues(t, CodeNotFound, int64(resp["code"].(float64)))
}

func TestAPIResetPinClearsAndSets(t *testing.T) {
	e := newTestServer()
	_, resp := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"小明","grade":9,"pin":"1234"}`)
	id := itoa(int(resp["data"].(map[string]any)["id"].(float64)))

	status, cleared := doJSON(t, e, http.MethodPost, "/api/kids/"+id+"/pin/reset", "")
	require.Equal(t, http.StatusOK, status)
	data := cleared["data"].(map[string]any)
	require.EqualValues(t, false, data["pin_set"], "清除后 pin_set=false")

	status, set := doJSON(t, e, http.MethodPost, "/api/kids/"+id+"/pin/reset", `{"new_pin":"5678"}`)
	require.Equal(t, http.StatusOK, status)
	require.EqualValues(t, true, set["data"].(map[string]any)["pin_set"])
}

func TestAPIResetPinRejectsInvalid(t *testing.T) {
	e := newTestServer()
	id, _ := createKidForReset(t, e)
	for _, bad := range []string{"123", "1234567", "abcd"} {
		status, resp := doJSON(t, e, http.MethodPost,
			"/api/kids/"+itoa(int(id))+"/pin/reset", `{"new_pin":"`+bad+`"}`)
		require.Equal(t, http.StatusBadRequest, status, "非法 PIN %q", bad)
		require.EqualValues(t, CodeInvalidInput, int64(resp["code"].(float64)))
	}
}

func TestAPIResetPinNotFound(t *testing.T) {
	e := newTestServer()
	status, resp := doJSON(t, e, http.MethodPost, "/api/kids/999/pin/reset", `{"new_pin":"1234"}`)
	require.Equal(t, http.StatusNotFound, status)
	require.EqualValues(t, CodeNotFound, int64(resp["code"].(float64)))
}

func TestAPIResetResponsesNeverLeakPinHash(t *testing.T) {
	e := newTestServer()
	id, _ := createKidForReset(t, e)
	raw := postRaw(t, e, "/api/kids/"+itoa(int(id))+"/pin/reset", `{"new_pin":"1234"}`)
	require.NotContains(t, raw, "pin_hash", "重置 PIN 响应不得含 pin_hash")
	raw = postRaw(t, e, "/api/kids/"+itoa(int(id))+"/access_code/reset", "")
	require.NotContains(t, raw, "pin_hash", "重置码响应不得含 pin_hash")
}

func (m *apiTestRepo) WithTx(tx domain.Tx) domain.KidRepository { return m }
