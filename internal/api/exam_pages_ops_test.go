package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/service"
)

// newPagesOpsServer assembles a test server wired to the fake repos.
func newPagesOpsServer(t *testing.T) (*echo.Echo, *fakePaperRepo, *fakePageRepo, string) {
	t.Helper()
	dir := t.TempDir()
	examRepo, pageRepo := newFakePaperRepo(), newFakePageRepo()
	e := echo.New()
	h := NewExamHandler(service.NewExamService(examRepo, pageRepo, dir, nil).WithTxManager(fakeTxManager{}), nil, nil)
	h.RegisterRoutes(e.Group("/api"))
	return e, examRepo, pageRepo, dir
}

// seedPagesOpsExam builds an exam with n pages (page files are not written to disk—a pure
// reorder never reads old files).
func seedPagesOpsExam(t *testing.T, examRepo *fakePaperRepo, pageRepo *fakePageRepo, dir string, n int) (*domain.Exam, []int64) {
	t.Helper()
	ctx := context.Background()
	examID, err := examRepo.Insert(ctx, &domain.Exam{Status: domain.ExamStatusActive})
	require.NoError(t, err)
	exam, err := examRepo.GetByID(ctx, examID)
	require.NoError(t, err)

	ids := make([]int64, 0, n)
	for idx := 1; idx <= n; idx++ {
		pid, err := pageRepo.Insert(ctx, &domain.Page{
			ExamID: examID, PageIndex: idx,
			FilePath: filepath.Join(dir, "assets", "exams", strconv.FormatInt(examID, 10), fmt.Sprintf("page_%03d.webp", idx)),
			SHA256:   fmt.Sprintf("sha-%d", idx),
		})
		require.NoError(t, err)
		ids = append(ids, pid)
	}
	return exam, ids
}

// opsJSON builds the multipart `operations` field content.
func opsJSON(t *testing.T, deletes []int64, finalOrder any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"delete_page_ids": deletes,
		"final_order":     finalOrder,
	})
	require.NoError(t, err)
	return string(raw)
}

// TestAPIPagesOpsReorderSuccess covers the happy path: 202, a task_id, the DB order
// applied in place, and the payload enqueued.
func TestAPIPagesOpsReorderSuccess(t *testing.T) {
	e, examRepo, pageRepo, dir := newPagesOpsServer(t)
	exam, ids := seedPagesOpsExam(t, examRepo, pageRepo, dir, 3)

	// final_order in numeric-id form (JSON number → normalized to a string on the server).
	ops := opsJSON(t, nil, []any{ids[2], ids[0], ids[1]})
	status, resp := uploadMultipartMulti(t, e,
		fmt.Sprintf("/api/exams/%d/pages/ops", exam.ID), nil, map[string]string{"operations": ops})
	require.Equal(t, http.StatusAccepted, status)
	require.Equal(t, float64(CodeOK), resp["code"])
	data := resp["data"].(map[string]any)
	require.Positive(t, data["task_id"].(float64))
	require.EqualValues(t, exam.ID, data["exam_id"].(float64))

	// Detail: the page order is immediately visible (DB changed, disk is async).
	code, body := getJSON(t, e, fmt.Sprintf("/api/exams/%d", exam.ID))
	require.Equal(t, http.StatusOK, code)
	pages := body["data"].(map[string]any)["pages"].([]any)
	require.Len(t, pages, 3)
	gotIDs := []int64{
		int64(pages[0].(map[string]any)["id"].(float64)),
		int64(pages[1].(map[string]any)["id"].(float64)),
		int64(pages[2].(map[string]any)["id"].(float64)),
	}
	require.Equal(t, []int64{ids[2], ids[0], ids[1]}, gotIDs)
	require.EqualValues(t, 1, pages[0].(map[string]any)["page_index"])
	require.EqualValues(t, 3, pages[2].(map[string]any)["page_index"])

	require.Len(t, pageRepo.enqueued, 1)
	task := pageRepo.enqueued[0]
	require.Equal(t, domain.TaskKindReorderPages, task.Kind)
	require.Equal(t, 60, task.Priority)
	require.Equal(t, domain.TaskStatusPending, task.Status)
	var detail map[string]any
	require.NoError(t, json.Unmarshal([]byte(task.Detail), &detail))
	require.EqualValues(t, exam.ID, detail["exam_id"].(float64))
	require.Len(t, detail["final_order"], 3)
	require.Len(t, detail["page_files"], 3)
	require.NotEmpty(t, detail["task_dir"])
}

// TestAPIPagesOpsDeleteAndNewFile covers deleting a page + adding a new one: 202, and the
// new page file is genuinely staged.
func TestAPIPagesOpsDeleteAndNewFile(t *testing.T) {
	e, examRepo, pageRepo, dir := newPagesOpsServer(t)
	exam, ids := seedPagesOpsExam(t, examRepo, pageRepo, dir, 2)

	ops := opsJSON(t, []int64{ids[1]}, []any{ids[0], "new_0"})
	status, resp := uploadMultipartMulti(t, e,
		fmt.Sprintf("/api/exams/%d/pages/ops", exam.ID),
		[]filePart{{name: "added.png", content: pngDoc("added")}},
		map[string]string{"operations": ops})
	require.Equal(t, http.StatusAccepted, status)
	require.Equal(t, float64(CodeOK), resp["code"])

	require.Len(t, pageRepo.enqueued, 1)
	var detail struct {
		TaskDir   string `json:"task_dir"`
		TempFiles []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"temp_files"`
		DeletePageIDs []int64 `json:"delete_page_ids"`
	}
	require.NoError(t, json.Unmarshal([]byte(pageRepo.enqueued[0].Detail), &detail))
	require.Len(t, detail.TempFiles, 1)
	require.Equal(t, []int64{ids[1]}, detail.DeletePageIDs)
	staged := filepath.Join(dir, filepath.FromSlash(detail.TempFiles[0].Path))
	blob, err := os.ReadFile(staged)
	require.NoError(t, err)
	require.NotEmpty(t, blob, "新页应已转 WebP 暂存")
	require.Equal(t, "assets/tmp/", detail.TempFiles[0].Path[:len("assets/tmp/")])

	// Detail after deletion: only 1 page remains (the new page row is inserted only when the
	// Worker lands).
	code, body := getJSON(t, e, fmt.Sprintf("/api/exams/%d", exam.ID))
	require.Equal(t, http.StatusOK, code)
	require.Len(t, body["data"].(map[string]any)["pages"].([]any), 1)
}

// TestAPIPagesOpsValidationErrors: shape errors 40002, business-validation errors 40010,
// missing exam 404.
func TestAPIPagesOpsValidationErrors(t *testing.T) {
	e, examRepo, pageRepo, dir := newPagesOpsServer(t)
	exam, ids := seedPagesOpsExam(t, examRepo, pageRepo, dir, 2)
	target := fmt.Sprintf("/api/exams/%d/pages/ops", exam.ID)

	t.Run("缺 operations 字段", func(t *testing.T) {
		status, resp := uploadMultipartMulti(t, e, target, nil, nil)
		require.Equal(t, http.StatusBadRequest, status)
		require.EqualValues(t, CodeValidation, resp["code"])
	})

	t.Run("operations 不是合法 JSON", func(t *testing.T) {
		status, resp := uploadMultipartMulti(t, e, target, nil,
			map[string]string{"operations": "{oops"})
		require.Equal(t, http.StatusBadRequest, status)
		require.EqualValues(t, CodeValidation, resp["code"])
	})

	t.Run("final_order 引用不存在的页", func(t *testing.T) {
		status, resp := uploadMultipartMulti(t, e, target, nil,
			map[string]string{"operations": opsJSON(t, nil, []any{99999, ids[1]})})
		require.Equal(t, http.StatusBadRequest, status)
		require.EqualValues(t, CodeInvalidInput, resp["code"])
	})

	t.Run("既删又排", func(t *testing.T) {
		status, resp := uploadMultipartMulti(t, e, target, nil,
			map[string]string{"operations": opsJSON(t, []int64{ids[0]}, []any{ids[0], ids[1]})})
		require.Equal(t, http.StatusBadRequest, status)
		require.EqualValues(t, CodeInvalidInput, resp["code"])
	})

	t.Run("试卷不存在", func(t *testing.T) {
		status, resp := uploadMultipartMulti(t, e, "/api/exams/99999/pages/ops", nil,
			map[string]string{"operations": opsJSON(t, nil, []any{ids[0], ids[1]})})
		require.Equal(t, http.StatusNotFound, status)
		require.EqualValues(t, CodeNotFound, resp["code"])
	})

	require.Empty(t, pageRepo.enqueued, "校验失败一律不得入队")
}

// TestAPIPagesOpsKidOwnership kid-side ownership: own exam 202, someone else's 404 (same
// rule as Delete).
func TestAPIPagesOpsKidOwnership(t *testing.T) {
	dir := t.TempDir()
	examRepo, pageRepo := newFakePaperRepo(), newFakePageRepo()
	ctx := context.Background()

	mine, err := examRepo.Insert(ctx, &domain.Exam{KidID: kidPtr(7), Status: domain.ExamStatusActive})
	require.NoError(t, err)
	theirs, err := examRepo.Insert(ctx, &domain.Exam{KidID: kidPtr(8), Status: domain.ExamStatusActive})
	require.NoError(t, err)
	seed := func(examID int64) []int64 {
		var out []int64
		for idx := 1; idx <= 2; idx++ {
			pid, err := pageRepo.Insert(ctx, &domain.Page{
				ExamID: examID, PageIndex: idx,
				FilePath: filepath.Join(dir, "assets", "exams", strconv.FormatInt(examID, 10), fmt.Sprintf("page_%03d.webp", idx)),
			})
			require.NoError(t, err)
			out = append(out, pid)
		}
		return out
	}
	mineIDs, theirIDs := seed(mine), seed(theirs)

	e := echo.New()
	h := NewExamHandler(service.NewExamService(examRepo, pageRepo, dir, nil).WithTxManager(fakeTxManager{}), nil, nil)
	g := e.Group("/api")
	// Test-specific audience injection (same rule as TestAPIPageFileKidOwnership).
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			(*c).Set(string(httpmw.CtxAudience), httpmw.AudienceKid)
			kidID, _ := strconv.ParseInt(c.Request().Header.Get("X-Test-Kid"), 10, 64)
			(*c).Set(string(httpmw.CtxKidID), kidID)
			return next(c)
		}
	})
	h.RegisterRoutes(g)

	status, resp := postOpsAsKid(t, e, mine, opsJSON(t, nil, []any{mineIDs[1], mineIDs[0]}), "7")
	require.Equal(t, http.StatusAccepted, status)
	require.Equal(t, float64(CodeOK), resp["code"])

	status, resp = postOpsAsKid(t, e, theirs, opsJSON(t, nil, []any{theirIDs[1], theirIDs[0]}), "7")
	require.Equal(t, http.StatusNotFound, status)
	require.EqualValues(t, CodeNotFound, resp["code"])
}

// postOpsAsKid sends a pages/ops request as the given kid (the X-Test-Kid header is
// consumed by the test middleware).
func postOpsAsKid(t *testing.T, e *echo.Echo, examID int64, ops, kid string) (int, map[string]any) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	require.NoError(t, mw.WriteField("operations", ops))
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/exams/%d/pages/ops", examID), body)
	req.Header.Set(echo.HeaderContentType, mw.FormDataContentType())
	req.Header.Set("X-Test-Kid", kid)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var parsed map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	}
	return rec.Code, parsed
}
