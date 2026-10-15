package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/service"
)

// newSummaryTestServer builds an exam handler whose audience is driven by the
// X-Test-Audience header (parent / kid), mirroring the real middleware context.
func newSummaryTestServer(t *testing.T) (*echo.Echo, *fakePaperRepo) {
	t.Helper()
	dir := t.TempDir()
	examRepo := newFakePaperRepo()
	pageRepo := newFakePageRepo()
	e := echo.New()
	h := NewExamHandler(service.NewExamService(examRepo, pageRepo, dir, nil).WithTxManager(fakeTxManager{}), nil, nil)
	g := e.Group("/api")
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			switch c.Request().Header.Get("X-Test-Audience") {
			case "kid":
				(*c).Set(string(httpmw.CtxAudience), httpmw.AudienceKid)
				(*c).Set(string(httpmw.CtxKidID), int64(7))
			case "parent":
				(*c).Set(string(httpmw.CtxAudience), httpmw.AudienceParent)
				(*c).Set(string(httpmw.CtxUserID), int64(1))
			}
			return next(c)
		}
	})
	h.RegisterRoutes(g)
	return e, examRepo
}

// summaryJSON issues a request carrying JSON and returns the parsed envelope.
func summaryJSON(t *testing.T, e *echo.Echo, method, target, audience, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	req.Header.Set("X-Test-Audience", audience)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	var parsed map[string]any
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &parsed))
	}
	return rec.Code, parsed
}

func summaryData(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	data, ok := resp["data"].(map[string]any)
	require.True(t, ok, "missing data envelope: %v", resp)
	return data
}

func TestAPIAppendSummaryParent(t *testing.T) {
	e, examRepo := newSummaryTestServer(t)
	ctx := t.Context()
	id, err := examRepo.Insert(ctx, &domain.Exam{Status: domain.ExamStatusActive})
	require.NoError(t, err)

	status, resp := summaryJSON(t, e, http.MethodPost, "/api/exams/1/summary", "parent",
		`{"content":"<p>这次计算粗心</p>"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])

	data := summaryData(t, resp)
	sig, _ := data["sig"].(string)
	require.Len(t, sig, 8)
	summary, _ := data["summary"].(string)
	require.Contains(t, summary, "家长 ·")
	require.Contains(t, summary, `data-author-type="1"`)
	require.Contains(t, summary, "<p>这次计算粗心</p>")

	stored, err := examRepo.GetByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, stored.Summary)
	require.Contains(t, *stored.Summary, sig)
}

func TestAPIAppendSummaryKidOwnExam(t *testing.T) {
	e, examRepo := newSummaryTestServer(t)
	ctx := t.Context()
	_, err := examRepo.Insert(ctx, &domain.Exam{KidID: kidPtr(7), Status: domain.ExamStatusActive})
	require.NoError(t, err)

	status, resp := summaryJSON(t, e, http.MethodPost, "/api/exams/1/summary", "kid",
		`{"content":"<p>我会了</p>"}`)
	require.Equal(t, http.StatusOK, status)
	summary := summaryData(t, resp)["summary"].(string)
	require.Contains(t, summary, "孩子 ·")
	require.Contains(t, summary, `data-author-type="2"`)
	require.Contains(t, summary, `data-author="7"`)
}

func TestAPIAppendSummaryKidForeignExam(t *testing.T) {
	e, examRepo := newSummaryTestServer(t)
	ctx := t.Context()
	_, err := examRepo.Insert(ctx, &domain.Exam{KidID: kidPtr(8), Status: domain.ExamStatusActive})
	require.NoError(t, err)

	status, resp := summaryJSON(t, e, http.MethodPost, "/api/exams/1/summary", "kid",
		`{"content":"<p>x</p>"}`)
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, float64(CodeNotFound), resp["code"])
}

func TestAPIAppendSummaryEmptyContent(t *testing.T) {
	e, examRepo := newSummaryTestServer(t)
	_, err := examRepo.Insert(t.Context(), &domain.Exam{Status: domain.ExamStatusActive})
	require.NoError(t, err)

	status, resp := summaryJSON(t, e, http.MethodPost, "/api/exams/1/summary", "parent",
		`{"content":"<p></p>"}`)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, float64(CodeInvalidInput), resp["code"])
}

func TestAPIAppendSummaryExamNotFound(t *testing.T) {
	e, _ := newSummaryTestServer(t)
	status, resp := summaryJSON(t, e, http.MethodPost, "/api/exams/404/summary", "parent",
		`{"content":"<p>x</p>"}`)
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, float64(CodeNotFound), resp["code"])
}

func TestAPIDeleteSummary(t *testing.T) {
	e, examRepo := newSummaryTestServer(t)
	ctx := t.Context()
	_, err := examRepo.Insert(ctx, &domain.Exam{Status: domain.ExamStatusActive})
	require.NoError(t, err)

	_, appendResp := summaryJSON(t, e, http.MethodPost, "/api/exams/1/summary", "parent",
		`{"content":"<p>待删除</p>"}`)
	sig := summaryData(t, appendResp)["sig"].(string)

	status, resp := summaryJSON(t, e, http.MethodDelete, "/api/exams/1/summary/"+sig, "parent", "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])
	summary := summaryData(t, resp)["summary"].(string)
	require.NotContains(t, summary, sig)
	require.NotContains(t, summary, "待删除")
}

func TestAPIDeleteSummaryKidForbidden(t *testing.T) {
	e, examRepo := newSummaryTestServer(t)
	ctx := t.Context()
	_, err := examRepo.Insert(ctx, &domain.Exam{KidID: kidPtr(7), Status: domain.ExamStatusActive})
	require.NoError(t, err)

	// Parent appends; the kid owns the exam but did not author the segment.
	_, appendResp := summaryJSON(t, e, http.MethodPost, "/api/exams/1/summary", "parent",
		`{"content":"<p>家长署名</p>"}`)
	sig := summaryData(t, appendResp)["sig"].(string)

	status, resp := summaryJSON(t, e, http.MethodDelete, "/api/exams/1/summary/"+sig, "kid", "")
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, float64(CodeNotFound), resp["code"])
}

func TestAPIDeleteSummaryUnknownSig(t *testing.T) {
	e, examRepo := newSummaryTestServer(t)
	_, err := examRepo.Insert(t.Context(), &domain.Exam{Status: domain.ExamStatusActive})
	require.NoError(t, err)

	status, resp := summaryJSON(t, e, http.MethodDelete, "/api/exams/1/summary/aaaaaaaa", "parent", "")
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, float64(CodeNotFound), resp["code"])
}
