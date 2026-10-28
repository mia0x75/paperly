package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/service"
)

type apiMessageRepo struct {
	rows []*domain.Message
}

func (r *apiMessageRepo) Insert(_ context.Context, m *domain.Message) (int64, error) {
	m.ID = int64(len(r.rows) + 1)
	r.rows = append(r.rows, m)
	return m.ID, nil
}

func (r *apiMessageRepo) List(_ context.Context, f domain.ListMessagesFilter) ([]*domain.Message, error) {
	out := make([]*domain.Message, 0, len(r.rows))
	for _, m := range r.rows {
		if m.Audience != f.Audience {
			continue
		}
		if f.Audience == domain.MessageAudienceKid && (m.KidID == nil || *m.KidID != f.KidID) {
			continue
		}
		if f.Cursor > 0 && m.ID >= f.Cursor {
			continue
		}
		if f.UnreadOnly && m.IsRead {
			continue
		}
		out = append(out, m)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (r *apiMessageRepo) CountUnread(_ context.Context, audience int, kidID int64) (int, error) {
	n := 0
	for _, m := range r.rows {
		if m.Audience != audience || m.IsRead {
			continue
		}
		if audience == domain.MessageAudienceKid && (m.KidID == nil || *m.KidID != kidID) {
			continue
		}
		n++
	}
	return n, nil
}

func (r *apiMessageRepo) MarkRead(_ context.Context, id int64, audience int, kidID int64) error {
	for _, m := range r.rows {
		if m.ID != id || m.Audience != audience || m.IsRead {
			continue
		}
		if audience == domain.MessageAudienceKid && (m.KidID == nil || *m.KidID != kidID) {
			continue
		}
		m.IsRead = true
	}
	return nil
}

func (r *apiMessageRepo) MarkAllRead(_ context.Context, audience int, kidID int64) (int64, error) {
	var n int64
	for _, m := range r.rows {
		if m.Audience != audience || m.IsRead {
			continue
		}
		if audience == domain.MessageAudienceKid && (m.KidID == nil || *m.KidID != kidID) {
			continue
		}
		m.IsRead = true
		n++
	}
	return n, nil
}

// Delete mirrors the real repository: physical delete inside the recipient boundary,
// no match -> domain.ErrNotFound.
func (r *apiMessageRepo) Delete(_ context.Context, id int64, audience int, kidID int64) error {
	for i, m := range r.rows {
		if m.ID != id || m.Audience != audience {
			continue
		}
		if audience == domain.MessageAudienceKid && (m.KidID == nil || *m.KidID != kidID) {
			continue
		}
		r.rows = append(r.rows[:i], r.rows[i+1:]...)
		return nil
	}
	return domain.ErrNotFound
}

func kidPtr(v int64) *int64 { return &v }

// seedAPI builds a repo double containing both parent and kid notifications.
func seedAPI() *apiMessageRepo {
	return &apiMessageRepo{rows: []*domain.Message{
		{ID: 1, Audience: domain.MessageAudienceParent, Kind: domain.MessageKindGradeAdvanced, Title: "已自动升级"},
		{ID: 2, Audience: domain.MessageAudienceParent, Kind: domain.MessageKindBackupDone, Title: "备份完成"},
		{ID: 3, Audience: domain.MessageAudienceParent, Kind: domain.MessageKindBackupDone, Title: "备份完成（已读）", IsRead: true},
		{ID: 4, Audience: domain.MessageAudienceKid, KidID: kidPtr(7), Kind: domain.MessageKindReviewDue, Title: "有 3 道错题待复习"},
		{ID: 5, Audience: domain.MessageAudienceKid, KidID: kidPtr(8), Kind: domain.MessageKindReviewDue, Title: "别人的通知"},
	}}
}

func newMessageServer(repo *apiMessageRepo) *echo.Echo {
	e := echo.New()
	g := e.Group("/api")
	svc := service.NewMessageService(repo)
	NewMessageHandler(svc).RegisterRoutes(g)
	return e
}

// newKidMessageServer simulates a kid session: the audience / kid_id come from the
// token claims (here: a test header), never from the request body.
func newKidMessageServer(repo *apiMessageRepo) *echo.Echo {
	e := echo.New()
	g := e.Group("/api")
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			(*c).Set(string(httpmw.CtxAudience), httpmw.AudienceKid)
			kidID, _ := strconv.ParseInt(c.Request().Header.Get("X-Test-Kid"), 10, 64)
			(*c).Set(string(httpmw.CtxKidID), kidID)
			return next(c)
		}
	})
	svc := service.NewMessageService(repo)
	NewMessageHandler(svc).RegisterRoutes(g)
	return e
}

type messageListBody struct {
	Code int `json:"code"`
	Data struct {
		Items []struct {
			ID     int64  `json:"id"`
			Kind   int    `json:"kind"`
			Title  string `json:"title"`
			IsRead bool   `json:"is_read"`
		} `json:"items"`
		NextCursor int64 `json:"next_cursor"`
		HasMore    bool  `json:"has_more"`
	} `json:"data"`
}

func getJSONAs[T any](t *testing.T, e *echo.Echo, target string) (int, T) {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	var out T
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	}
	return rec.Code, out
}

func postJSONAs[T any](t *testing.T, e *echo.Echo, target string) (int, T) {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, target, nil))
	var out T
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	}
	return rec.Code, out
}

// The parent side only sees parent notifications; kid notifications must not appear
// in the parent list.
func TestMessageListParentSeesOnlyParentAudience(t *testing.T) {
	e := newMessageServer(seedAPI())

	status, body := getJSONAs[messageListBody](t, e, "/api/messages")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, CodeOK, body.Code)
	require.Len(t, body.Data.Items, 3)
	for _, it := range body.Data.Items {
		require.NotContains(t, it.Title, "待复习", "孩子通知不得出现在家长端")
	}
	// id DESC: newest first.
	require.EqualValues(t, 3, body.Data.Items[0].ID)
}

// Badge = unread count (already-read ones do not count).
func TestMessageUnreadCountEndpoint(t *testing.T) {
	e := newMessageServer(seedAPI())

	status, body := getJSONAs[struct {
		Data struct {
			UnreadCount int `json:"unread_count"`
		} `json:"data"`
	}](t, e, "/api/messages/unread-count")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 2, body.Data.UnreadCount, "3 条家长通知里 1 条已读")
}

// The static segment /messages/unread-count must not be swallowed by /messages/:id/read
// (and vice versa).
func TestMessageStaticRoutesNotShadowedByParam(t *testing.T) {
	e := newMessageServer(seedAPI())

	status, _ := getJSONAs[messageListBody](t, e, "/api/messages/unread-count")
	require.Equal(t, http.StatusOK, status, "静态段 unread-count 必须命中自己的 handler")

	code, _ := postJSONAs[map[string]any](t, e, "/api/messages/1/read")
	require.Equal(t, http.StatusOK, code)
}

func TestMessageMarkReadAndReadAll(t *testing.T) {
	repo := seedAPI()
	e := newMessageServer(repo)

	status, _ := postJSONAs[map[string]any](t, e, "/api/messages/1/read")
	require.Equal(t, http.StatusOK, status)
	require.True(t, repo.rows[0].IsRead, "单条应被标记已读")

	// Repeating the mark is idempotent and returns no error.
	status, _ = postJSONAs[map[string]any](t, e, "/api/messages/1/read")
	require.Equal(t, http.StatusOK, status)

	// Mark-all-read: kid notifications are unaffected (the parent side only
	// manages audience=1).
	status, body := postJSONAs[struct {
		Data struct {
			Updated int64 `json:"updated"`
		} `json:"data"`
	}](t, e, "/api/messages/read-all")
	require.Equal(t, http.StatusOK, status)
	require.EqualValues(t, 1, body.Data.Updated, "只剩 1 条未读")
	require.False(t, repo.rows[3].IsRead, "孩子通知不得被家长的「全部已读」波及")

	_, body = postJSONAs[struct {
		Data struct {
			Updated int64 `json:"updated"`
		} `json:"data"`
	}](t, e, "/api/messages/read-all")
	require.Zero(t, body.Data.Updated)
}

func TestMessageMarkReadRejectsBadID(t *testing.T) {
	e := newMessageServer(seedAPI())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/messages/abc/read", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid message id")
}

func TestMessageListRejectsBadUnreadOnly(t *testing.T) {
	e := newMessageServer(seedAPI())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/messages?unread_only=maybe", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMessageListKeysetPaginationOverHTTP(t *testing.T) {
	repo := &apiMessageRepo{}
	for i := 0; i < 5; i++ {
		_, err := repo.Insert(context.Background(), &domain.Message{
			Audience: domain.MessageAudienceParent, Kind: domain.MessageKindOther,
			Title: fmt.Sprintf("通知 %d", i),
		})
		require.NoError(t, err)
	}
	e := newMessageServer(repo)

	_, page1 := getJSONAs[messageListBody](t, e, "/api/messages?limit=2")
	require.Len(t, page1.Data.Items, 2)
	require.True(t, page1.Data.HasMore)

	_, page2 := getJSONAs[messageListBody](t, e,
		fmt.Sprintf("/api/messages?limit=2&cursor=%d", page1.Data.NextCursor))
	require.Len(t, page2.Data.Items, 2)

	_, page3 := getJSONAs[messageListBody](t, e,
		fmt.Sprintf("/api/messages?limit=2&cursor=%d", page2.Data.NextCursor))
	require.Len(t, page3.Data.Items, 1)
	require.False(t, page3.Data.HasMore)
	require.Zero(t, page3.Data.NextCursor)
}

// TestAPIMessageListPaginationValidation pins down the limit/cursor contract of /api/messages,
// consistent with /api/exams: invalid values return 400, only omitted values fall
// back to the default.
func TestAPIMessageListPaginationValidation(t *testing.T) {
	e := newMessageServer(&apiMessageRepo{})

	bad := []string{"?limit=0", "?limit=101", "?limit=abc", "?cursor=-1", "?cursor=abc"}
	for _, q := range bad {
		status, resp := doJSON(t, e, http.MethodGet, "/api/messages"+q, "")
		require.Equal(t, http.StatusBadRequest, status, "非法参数应 400：%s", q)
		require.Equal(t, float64(CodeValidation), resp["code"], "非法参数应 40002：%s", q)
	}

	good := []string{"", "?limit=1", "?limit=20", "?limit=100", "?cursor=0", "?cursor=5"}
	for _, q := range good {
		status, resp := doJSON(t, e, http.MethodGet, "/api/messages"+q, "")
		require.Equal(t, http.StatusOK, status, "合法参数应 200：%s", q)
		require.Equal(t, float64(CodeOK), resp["code"], "合法参数应 0：%s", q)
	}
}

// TestAPIMessageDelete covers the delete contract: a notification inside the recipient
// boundary is physically removed (200), one outside it is 404, and a malformed id is 400.
func TestAPIMessageDelete(t *testing.T) {
	repo := seedAPI()
	e := newMessageServer(repo)

	status, body := doJSON(t, e, http.MethodDelete, "/api/messages/1", "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), body["code"])
	require.Len(t, repo.rows, 4, "被删的通知应真的消失")

	// The kid-side notification is outside the parent's boundary: 404, row untouched.
	status, body = doJSON(t, e, http.MethodDelete, "/api/messages/4", "")
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, float64(CodeNotFound), body["code"])
	require.Len(t, repo.rows, 4, "越权删除不得生效")

	// Deleting the same id twice: the second attempt is 404 (already gone).
	status, _ = doJSON(t, e, http.MethodDelete, "/api/messages/1", "")
	require.Equal(t, http.StatusNotFound, status)

	// A malformed id is a validation error, not a 404.
	status, _ = doJSON(t, e, http.MethodDelete, "/api/messages/abc", "")
	require.Equal(t, http.StatusBadRequest, status)
}

// TestAPIMessageDeleteKidBoundary: the kid side deletes only its own notifications.
func TestAPIMessageDeleteKidBoundary(t *testing.T) {
	repo := seedAPI()
	e := newKidMessageServer(repo)

	deleteAs := func(kidID, id string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/messages/"+id, nil)
		req.Header.Set("X-Test-Kid", kidID)
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, deleteAs("7", "4"), "孩子应能删自己的通知")
	require.Equal(t, http.StatusNotFound, deleteAs("7", "5"), "孩子不得删别人的通知")
	require.Equal(t, http.StatusNotFound, deleteAs("7", "2"), "孩子不得删家长通知")

	require.Len(t, repo.rows, 4, "只有孩子 7 的那一条被删掉")
}

func (m *apiMessageRepo) WithTx(tx domain.Tx) domain.MessageRepository { return m }
