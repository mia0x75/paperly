package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/service"
	"paperly/internal/storage"
)

// fakePaperRepo satisfies domain.ExamRepository, for API black-box tests only (the real
// sqlite repo is covered by the repository layer tests).
type fakePaperRepo struct {
	byID   map[int64]*domain.Exam
	nextID int64
}

func newFakePaperRepo() *fakePaperRepo {
	return &fakePaperRepo{byID: map[int64]*domain.Exam{}}
}

func (r *fakePaperRepo) Insert(_ context.Context, p *domain.Exam) (int64, error) {
	r.nextID++
	clone := *p
	clone.ID = r.nextID
	r.byID[r.nextID] = &clone
	return r.nextID, nil
}

func (r *fakePaperRepo) GetByID(_ context.Context, id int64) (*domain.Exam, error) {
	p, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return p, nil
}

func (r *fakePaperRepo) List(_ context.Context, f domain.ListExamFilter) ([]*domain.Exam, int64, error) {
	out := make([]*domain.Exam, 0)
	for _, p := range r.byID {
		if f.Status == 0 && p.Status == domain.ExamStatusDeleted {
			continue
		}
		if f.KidID != nil {
			if p.KidID == nil || *p.KidID != *f.KidID {
				continue
			}
		}
		if f.Cursor > 0 && p.ID >= f.Cursor {
			continue
		}
		out = append(out, p)
	}
	// Replicates the SQL: id DESC + keyset(id < cursor) + fetch one extra to probe hasMore.
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var next int64
	if len(out) > limit {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

func (r *fakePaperRepo) Update(_ context.Context, p *domain.Exam) error {
	cur, ok := r.byID[p.ID]
	if !ok {
		return domain.ErrNotFound
	}
	cur.SubjectID, cur.Grade, cur.ExamType, cur.ExamDate = p.SubjectID, p.Grade, p.ExamType, p.ExamDate
	cur.KidID, cur.Summary, cur.SummarySource = p.KidID, p.Summary, p.SummarySource
	return nil
}

func (r *fakePaperRepo) UpdateSummary(_ context.Context, id int64, expectedOld, next string) (bool, error) {
	cur, ok := r.byID[id]
	if !ok {
		return false, domain.ErrNotFound
	}
	stored := ""
	if cur.Summary != nil {
		stored = *cur.Summary
	}
	if stored != expectedOld {
		return false, nil
	}
	cur.Summary = &next
	return true, nil
}

func (r *fakePaperRepo) SoftDelete(_ context.Context, id int64) error {
	p, ok := r.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	p.Status = domain.ExamStatusDeleted
	return nil
}

func (r *fakePaperRepo) Restore(_ context.Context, id int64) error {
	p, ok := r.byID[id]
	if !ok || p.Status != domain.ExamStatusDeleted {
		return domain.ErrNotFound
	}
	p.Status = domain.ExamStatusActive
	return nil
}

// fakePageRepo satisfies domain.PageRepository.
type fakePageRepo struct {
	byExam   map[int64][]*domain.Page
	nextID   int64
	nextTask int64
	// enqueued records the tasks RebuildPages enqueues (including detail), for payload assertions.
	enqueued   []*domain.Task
	rebuildErr error
}

func newFakePageRepo() *fakePageRepo {
	return &fakePageRepo{byExam: map[int64][]*domain.Page{}}
}

func (r *fakePageRepo) Insert(_ context.Context, p *domain.Page) (int64, error) {
	r.nextID++
	clone := *p
	clone.ID = r.nextID
	r.byExam[p.ExamID] = append(r.byExam[p.ExamID], &clone)
	return r.nextID, nil
}

func (r *fakePageRepo) GetByID(_ context.Context, id int64) (*domain.Page, error) {
	for _, pages := range r.byExam {
		for _, p := range pages {
			if p.ID == id {
				return p, nil
			}
		}
	}
	return nil, domain.ErrNotFound
}

func (r *fakePageRepo) ListByExam(_ context.Context, examID int64) ([]*domain.Page, error) {
	out := r.byExam[examID]
	if out == nil {
		out = make([]*domain.Page, 0)
	}
	// Replicates the real repo's page_index ASC ordering contract.
	sorted := make([]*domain.Page, len(out))
	copy(sorted, out)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PageIndex < sorted[j].PageIndex })
	return sorted, nil
}

// RebuildPages replicates the real semantics: delete rows + rewrite index/file_path per
// reorders + bookkeep and enqueue.
func (r *fakePageRepo) RebuildPages(_ context.Context, examID int64, deletes []int64, reorders []domain.PageReorder, enqueue *domain.Task) (int64, error) {
	if r.rebuildErr != nil {
		return 0, r.rebuildErr
	}
	deleted := make(map[int64]bool, len(deletes))
	for _, id := range deletes {
		deleted[id] = true
	}
	kept := make([]*domain.Page, 0, len(r.byExam[examID]))
	for _, p := range r.byExam[examID] {
		if !deleted[p.ID] {
			kept = append(kept, p)
		}
	}
	byID := make(map[int64]*domain.Page, len(kept))
	for _, p := range kept {
		byID[p.ID] = p
	}
	for _, ro := range reorders {
		p, ok := byID[ro.ID]
		if !ok || p.ExamID != examID {
			return 0, domain.ErrNotFound
		}
		p.PageIndex = ro.NewIndex
		p.FilePath = ro.NewFilePath
	}
	r.byExam[examID] = kept
	if enqueue == nil {
		return 0, nil
	}
	r.nextTask++
	clone := *enqueue
	r.enqueued = append(r.enqueued, &clone)
	return r.nextTask, nil
}

// InsertIfAbsent replicates the ON CONFLICT (exam_id, page_index) DO NOTHING semantics.
func (r *fakePageRepo) InsertIfAbsent(_ context.Context, pages []*domain.Page) (int, error) {
	inserted := 0
	for _, p := range pages {
		dup := false
		for _, cur := range r.byExam[p.ExamID] {
			if cur.PageIndex == p.PageIndex {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		if _, err := r.Insert(context.Background(), p); err != nil {
			return inserted, err
		}
		inserted++
	}
	return inserted, nil
}

func newPaperTestServer(t *testing.T) *echo.Echo {
	t.Helper()
	dir := t.TempDir()
	e := echo.New()
	h := NewExamHandler(service.NewExamService(newFakePaperRepo(), newFakePageRepo(), dir, nil).WithTxManager(fakeTxManager{}), nil, nil)
	h.RegisterRoutes(e.Group("/api"))
	return e
}

func uploadMultipart(t *testing.T, e *echo.Echo, target string, fileName string, content []byte, fields map[string]string) (int, map[string]any) {
	t.Helper()
	return uploadMultipartMulti(t, e, target, []filePart{{name: fileName, content: content}}, fields)
}

type filePart struct {
	name    string
	content []byte
}

func uploadMultipartMulti(t *testing.T, e *echo.Echo, target string, files []filePart, fields map[string]string) (int, map[string]any) {
	t.Helper()

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	for _, f := range files {
		fw, err := mw.CreateFormFile("file", f.name)
		require.NoError(t, err)
		_, err = fw.Write(f.content)
		require.NoError(t, err)
	}
	for k, v := range fields {
		require.NoError(t, mw.WriteField(k, v))
	}
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, target, body)
	req.Header.Set(echo.HeaderContentType, mw.FormDataContentType())
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var parsed map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	}
	return rec.Code, parsed
}

func TestAPIUploadExamSuccess(t *testing.T) {
	e := newPaperTestServer(t)
	status, resp := uploadMultipart(t, e, "/api/exams/upload", "期中.png",
		pngDoc("upload-test"), map[string]string{"subject_id": "1", "grade": "9"})
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])

	data := resp["data"].(map[string]any)
	require.EqualValues(t, 1, data["subject_id"])
	require.EqualValues(t, 9, data["grade"])
	require.EqualValues(t, domain.UploadedByParent, data["uploaded_by"])
	require.EqualValues(t, domain.ExamStatusActive, data["status"])

	// Security contract: the upload response does not include internal path fields.
	raw := readAllBody(resp)
	require.NotContains(t, raw, "file_path")
}

func readAllBody(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}

func TestAPIUploadExamMultiPage(t *testing.T) {
	e := newPaperTestServer(t)
	files := []filePart{
		{name: "p1.png", content: pngDoc("page-1")},
		{name: "p2.png", content: pngDoc("page-2")},
		{name: "p3.png", content: pngDoc("page-3")},
	}
	status, resp := uploadMultipartMulti(t, e, "/api/exams/upload", files, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])

	id := itoa(int(resp["data"].(map[string]any)["id"].(float64)))
	code, body := getJSON(t, e, "/api/exams/"+id)
	require.Equal(t, http.StatusOK, code)
	pages := body["data"].(map[string]any)["pages"].([]any)
	require.Len(t, pages, 3)
}

func TestAPIUploadExamUnsupportedType(t *testing.T) {
	e := newPaperTestServer(t)
	status, resp := uploadMultipart(t, e, "/api/exams/upload", "evil.bin",
		[]byte("MZ fake exe content"), nil)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, float64(CodeUnsupportedType), resp["code"])
}

func TestAPIUploadExamTooLarge(t *testing.T) {
	e := newPaperTestServer(t)
	big := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, storage.MaxUploadBytes+1)...)
	status, resp := uploadMultipart(t, e, "/api/exams/upload", "big.png", big, nil)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, float64(CodeFileTooLarge), resp["code"])
}

func TestAPIExamListPagination(t *testing.T) {
	e := newPaperTestServer(t)
	for i := 0; i < 3; i++ {
		s, _ := uploadMultipart(t, e, "/api/exams/upload", "p.png",
			pngDoc("page-"+string(rune('a'+i))), nil)
		require.Equal(t, http.StatusOK, s)
	}

	status, resp := doJSON(t, e, http.MethodGet, "/api/exams?limit=2", "")
	require.Equal(t, http.StatusOK, status)
	list := resp["data"].(map[string]any)
	items := list["items"].([]any)
	require.Len(t, items, 2)
	require.Equal(t, true, list["has_more"])
	next := int64(list["next_cursor"].(float64))
	require.NotZero(t, next)

	status, resp = doJSON(t, e, http.MethodGet, "/api/exams?limit=2&cursor="+itoa(int(next)), "")
	require.Equal(t, http.StatusOK, status)
	list = resp["data"].(map[string]any)
	items = list["items"].([]any)
	require.Len(t, items, 1)
	require.Equal(t, false, list["has_more"])
	require.Equal(t, float64(0), list["next_cursor"])

	// List items do not contain file_path / sha256.
	itemJSON := readAllBody(list)
	require.NotContains(t, itemJSON, "file_path")
	require.NotContains(t, itemJSON, "sha256")
}

// TestAPIExamListFilterValidation pins down the invalid-value contract of list filter params:
// status/limit/cursor/grade/exam_type that are invalid all return 400 (40002), only omitted
// values fall back to the default.
func TestAPIExamListFilterValidation(t *testing.T) {
	e := newPaperTestServer(t)

	bad := []string{
		"?status=abc", "?status=0", "?status=3",
		"?limit=0", "?limit=101", "?limit=abc",
		"?cursor=-1", "?cursor=abc",
		"?grade=abc", "?grade=0", "?grade=13",
		"?exam_type=abc", "?exam_type=7", "?exam_type=0",
	}
	for _, q := range bad {
		status, resp := doJSON(t, e, http.MethodGet, "/api/exams"+q, "")
		require.Equal(t, http.StatusBadRequest, status, "非法参数应 400：%s", q)
		require.Equal(t, float64(CodeValidation), resp["code"], "非法参数应 40002：%s", q)
	}

	good := []string{
		"", "?status=1", "?status=2",
		"?limit=1", "?limit=20", "?limit=100",
		"?cursor=0", "?cursor=5",
		"?grade=1", "?grade=9", "?grade=12",
		"?exam_type=5", "?exam_type=99",
	}
	for _, q := range good {
		status, resp := doJSON(t, e, http.MethodGet, "/api/exams"+q, "")
		require.Equal(t, http.StatusOK, status, "合法参数应 200：%s", q)
		require.Equal(t, float64(CodeOK), resp["code"], "合法参数应 0：%s", q)
	}
}

func TestAPIExamUpdateAndSoftDelete(t *testing.T) {
	e := newPaperTestServer(t)
	_, r1 := uploadMultipart(t, e, "/api/exams/upload", "a.png", pngDoc("upd"), nil)
	id1 := itoa(int(r1["data"].(map[string]any)["id"].(float64)))

	status, resp := doJSON(t, e, http.MethodPut, "/api/exams/"+id1,
		`{"grade":10,"summary":"有折痕"}`)
	require.Equal(t, http.StatusOK, status)
	detail := resp["data"].(map[string]any)
	require.EqualValues(t, 10, detail["grade"])
	require.Equal(t, "有折痕", detail["summary"])

	status, _ = doJSON(t, e, http.MethodDelete, "/api/exams/"+id1, "")
	require.Equal(t, http.StatusNoContent, status)

	// Default List excludes it.
	_, listResp := doJSON(t, e, http.MethodGet, "/api/exams", "")
	require.Empty(t, listResp["data"].(map[string]any)["items"])

	// Get still returns it (soft delete only blocks List).
	status, resp = doJSON(t, e, http.MethodGet, "/api/exams/"+id1, "")
	require.Equal(t, http.StatusOK, status)
	require.EqualValues(t, domain.ExamStatusDeleted, resp["data"].(map[string]any)["status"])
}

// TestAPIExamRestore covers POST /api/exams/:id/restore:
// a soft-deleted exam is restored to active and returns to the normal list; restoring an
// already-active exam returns 40012 (ErrInvalidState).
func TestAPIExamRestore(t *testing.T) {
	e := newPaperTestServer(t)
	_, r1 := uploadMultipart(t, e, "/api/exams/upload", "a.png", pngDoc("rst"), nil)
	id1 := itoa(int(r1["data"].(map[string]any)["id"].(float64)))

	// An active exam cannot be restored: the state gate returns 40012.
	status, resp := doJSON(t, e, http.MethodPost, "/api/exams/"+id1+"/restore", "")
	require.Equal(t, http.StatusBadRequest, status)
	require.EqualValues(t, CodeInvalidState, resp["code"])

	status, _ = doJSON(t, e, http.MethodDelete, "/api/exams/"+id1, "")
	require.Equal(t, http.StatusNoContent, status)

	status, resp = doJSON(t, e, http.MethodPost, "/api/exams/"+id1+"/restore", "")
	require.Equal(t, http.StatusOK, status)
	require.EqualValues(t, domain.ExamStatusActive, resp["data"].(map[string]any)["status"])

	_, listResp := doJSON(t, e, http.MethodGet, "/api/exams", "")
	require.Len(t, listResp["data"].(map[string]any)["items"], 1)

	status, resp = doJSON(t, e, http.MethodPost, "/api/exams/"+id1+"/restore", "")
	require.Equal(t, http.StatusBadRequest, status)
	require.EqualValues(t, CodeInvalidState, resp["code"])

	status, resp = doJSON(t, e, http.MethodPost, "/api/exams/99999/restore", "")
	require.Equal(t, http.StatusNotFound, status)
	require.EqualValues(t, CodeNotFound, resp["code"])
}

// pngDoc returns a real, decodable PNG (the tag is used to differentiate content).
func pngDoc(tag string) []byte {
	var h uint8
	for i := 0; i < len(tag); i++ {
		h = h*31 + tag[i]
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	c := color.RGBA{R: h, G: 0x30, B: 0x90, A: 0xFF}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// TestAPIPageFileServesBytes covers GET /api/pages/:id/file:
// a hit returns the image bytes + Content-Type; a missing id returns 404; an invalid id returns 400.
func TestAPIPageFileServesBytes(t *testing.T) {
	dir := t.TempDir()
	pageRepo := newFakePageRepo()
	pagePath := filepath.Join(dir, "assets", "exams", "1", "page_001.png")
	require.NoError(t, os.MkdirAll(filepath.Dir(pagePath), 0o750))
	require.NoError(t, os.WriteFile(pagePath, pngDoc("page-file"), 0o600))
	pid, err := pageRepo.Insert(context.Background(), &domain.Page{
		ExamID: 1, PageIndex: 1, FilePath: pagePath, SHA256: "x",
	})
	require.NoError(t, err)

	e := echo.New()
	h := NewExamHandler(service.NewExamService(newFakePaperRepo(), pageRepo, dir, nil).WithTxManager(fakeTxManager{}), nil, nil)
	h.RegisterRoutes(e.Group("/api"))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+itoa(int(pid))+"/file", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, pngDoc("page-file"), rec.Body.Bytes())

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/99999/file", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/0/file", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestAPIPageFileKidOwnership covers the page-image ownership check for kid sessions:
// a kid's own exam's page image returns 200, someone else's returns 404—same rule as Get,
// not distinguishing "missing" from "unauthorized".
func TestAPIPageFileKidOwnership(t *testing.T) {
	dir := t.TempDir()
	examRepo := newFakePaperRepo()
	pageRepo := newFakePageRepo()
	ctx := context.Background()

	mine, err := examRepo.Insert(ctx, &domain.Exam{
		KidID: kidPtr(7), Status: domain.ExamStatusActive,
	})
	require.NoError(t, err)
	theirs, err := examRepo.Insert(ctx, &domain.Exam{
		KidID: kidPtr(8), Status: domain.ExamStatusActive,
	})
	require.NoError(t, err)

	writePage := func(examID int64) int64 {
		path := filepath.Join(dir, "assets", "exams", itoa(int(examID)), "page_001.png")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, pngDoc("ownership"), 0o600))
		pid, err := pageRepo.Insert(ctx, &domain.Page{
			ExamID: examID, PageIndex: 1, FilePath: path, SHA256: "x",
		})
		require.NoError(t, err)
		return pid
	}
	minePage := writePage(mine)
	theirPage := writePage(theirs)

	e := echo.New()
	h := NewExamHandler(service.NewExamService(examRepo, pageRepo, dir, nil).WithTxManager(fakeTxManager{}), nil, nil)
	g := e.Group("/api")
	// Test-specific audience injection: simulates a kid session that has passed the gate
	// (kid_id is taken from the request header).
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			(*c).Set(string(httpmw.CtxAudience), httpmw.AudienceKid)
			kidID, _ := strconv.ParseInt(c.Request().Header.Get("X-Test-Kid"), 10, 64)
			(*c).Set(string(httpmw.CtxKidID), kidID)
			return next(c)
		}
	})
	h.RegisterRoutes(g)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/pages/"+itoa(int(minePage))+"/file", nil)
	req.Header.Set("X-Test-Kid", "7")
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/pages/"+itoa(int(theirPage))+"/file", nil)
	req.Header.Set("X-Test-Kid", "7")
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func (m *fakePaperRepo) WithTx(tx domain.Tx) domain.ExamRepository { return m }

func (m *fakePageRepo) WithTx(tx domain.Tx) domain.PageRepository { return m }
