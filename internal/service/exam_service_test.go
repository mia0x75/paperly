package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	"paperly/internal/repository"
	"paperly/internal/storage"
)

// mockPaperRepo is an in-memory fake implementation of domain.ExamRepository.
type mockPaperRepo struct {
	byID   map[int64]*domain.Exam
	nextID int64
	listFn func(f domain.ListExamFilter) ([]*domain.Exam, int64, error)
}

// fakeTxManager runs fn inline (the in-memory mocks ignore the transaction
// handle); it only needs to satisfy domain.TxManager so Upload's atomic path can open.
type fakeTxManager struct{}

func (fakeTxManager) WithinTx(_ context.Context, fn func(tx domain.Tx) error) error {
	return fn(nil)
}

// commitFailTxManager runs fn successfully but then reports the commit failed —
// simulating rows-written-then-rolled-back while the page files hit the disk.
type commitFailTxManager struct{}

func (commitFailTxManager) WithinTx(_ context.Context, fn func(tx domain.Tx) error) error {
	if err := fn(nil); err != nil {
		return err
	}
	return errors.New("commit failed")
}

func newMockPaperRepo() *mockPaperRepo {
	return &mockPaperRepo{byID: map[int64]*domain.Exam{}}
}

func (m *mockPaperRepo) Insert(_ context.Context, p *domain.Exam) (int64, error) {
	m.nextID++
	clone := *p
	clone.ID = m.nextID
	m.byID[m.nextID] = &clone
	return m.nextID, nil
}

func (m *mockPaperRepo) GetByID(_ context.Context, id int64) (*domain.Exam, error) {
	p, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return p, nil
}

func (m *mockPaperRepo) List(_ context.Context, f domain.ListExamFilter) ([]*domain.Exam, int64, error) {
	if m.listFn != nil {
		return m.listFn(f)
	}
	out := make([]*domain.Exam, 0)
	for _, p := range m.byID {
		// Same basis as ExamRepoSQLite.List: Status != 0 matches exactly, 0 = all (including deleted).
		if f.Status != 0 && p.Status != f.Status {
			continue
		}
		out = append(out, p)
	}
	return out, 0, nil
}

func (m *mockPaperRepo) Update(_ context.Context, p *domain.Exam) error {
	cur, ok := m.byID[p.ID]
	if !ok {
		return domain.ErrNotFound
	}
	cur.SubjectID, cur.Grade, cur.ExamType, cur.ExamDate = p.SubjectID, p.Grade, p.ExamType, p.ExamDate
	cur.KidID, cur.Summary, cur.SummarySource = p.KidID, p.Summary, p.SummarySource
	return nil
}

// UpdateSummary replicates the conditional-write semantics (NULL counts as empty);
// false means the stored value no longer matches (concurrent modification).
func (m *mockPaperRepo) UpdateSummary(_ context.Context, id int64, expectedOld, next string) (bool, error) {
	cur, ok := m.byID[id]
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

func (m *mockPaperRepo) SoftDelete(_ context.Context, id int64) error {
	p, ok := m.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	p.Status = domain.ExamStatusDeleted
	return nil
}

func (m *mockPaperRepo) Restore(_ context.Context, id int64) error {
	p, ok := m.byID[id]
	if !ok || p.Status != domain.ExamStatusDeleted {
		return domain.ErrNotFound
	}
	p.Status = domain.ExamStatusActive
	return nil
}

// mockPageRepo is an in-memory fake implementation of domain.PageRepository.
type mockPageRepo struct {
	byExam   map[int64][]*domain.Page
	nextID   int64
	nextTask int64
	// enqueued records the tasks queued by RebuildPages (with detail), for payload assertions.
	enqueued []*domain.Task
	// rebuildErr, when non-nil, makes RebuildPages fail
	// (tests temp-dir cleanup after a failed transaction).
	rebuildErr error
}

func newMockPageRepo() *mockPageRepo {
	return &mockPageRepo{byExam: map[int64][]*domain.Page{}}
}

func (m *mockPageRepo) Insert(_ context.Context, p *domain.Page) (int64, error) {
	m.nextID++
	clone := *p
	clone.ID = m.nextID
	m.byExam[p.ExamID] = append(m.byExam[p.ExamID], &clone)
	return m.nextID, nil
}

func (m *mockPageRepo) GetByID(_ context.Context, id int64) (*domain.Page, error) {
	for _, pages := range m.byExam {
		for _, p := range pages {
			if p.ID == id {
				return p, nil
			}
		}
	}
	return nil, domain.ErrNotFound
}

func (m *mockPageRepo) ListByExam(_ context.Context, examID int64) ([]*domain.Page, error) {
	out := m.byExam[examID]
	if out == nil {
		out = make([]*domain.Page, 0)
	}
	sorted := make([]*domain.Page, len(out))
	copy(sorted, out)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PageIndex < sorted[j].PageIndex })
	return sorted, nil
}

// RebuildPages replicates the real semantics: delete rows + rewrite index/file_path +
// record the enqueued task.
func (m *mockPageRepo) RebuildPages(_ context.Context, examID int64, deletes []int64, reorders []domain.PageReorder, enqueue *domain.Task) (int64, error) {
	if m.rebuildErr != nil {
		return 0, m.rebuildErr
	}
	deleted := make(map[int64]bool, len(deletes))
	for _, id := range deletes {
		deleted[id] = true
	}
	kept := make([]*domain.Page, 0, len(m.byExam[examID]))
	for _, p := range m.byExam[examID] {
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
	m.byExam[examID] = kept
	if enqueue == nil {
		return 0, nil
	}
	m.nextTask++
	clone := *enqueue
	m.enqueued = append(m.enqueued, &clone)
	return m.nextTask, nil
}

// InsertIfAbsent replicates the ON CONFLICT (exam_id, page_index) DO NOTHING semantics.
func (m *mockPageRepo) InsertIfAbsent(_ context.Context, pages []*domain.Page) (int, error) {
	inserted := 0
	for _, p := range pages {
		dup := false
		for _, cur := range m.byExam[p.ExamID] {
			if cur.PageIndex == p.PageIndex {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		if _, err := m.Insert(context.Background(), p); err != nil {
			return inserted, err
		}
		inserted++
	}
	return inserted, nil
}

func newTestExamService(repo domain.ExamRepository, dir string) *ExamService {
	return NewExamService(repo, newMockPageRepo(), dir, nil).WithTxManager(fakeTxManager{})
}

func TestExamUploadSuccess(t *testing.T) {
	ctx := context.Background()
	repo := newMockPaperRepo()
	pageRepo := newMockPageRepo()
	dir := t.TempDir()
	svc := NewExamService(repo, pageRepo, dir, nil).WithTxManager(fakeTxManager{})

	img := samplePNG()
	p, id, err := svc.Upload(ctx, UploadExamReq{
		Pages:      []PageFile{{Reader: bytes.NewReader(img), FileName: "期中.png", FileSize: int64(len(img))}},
		UploadedBy: domain.UploadedByParent,
	})
	require.NoError(t, err)
	require.Positive(t, id)
	require.Equal(t, domain.ExamStatusActive, p.Status)
	require.Equal(t, domain.UploadedByParent, p.UploadedBy)

	pagePath := storage.PagePath(dir, id, 1, "webp")
	require.FileExists(t, pagePath)
	pages, err := pageRepo.ListByExam(ctx, id)
	require.NoError(t, err)
	require.Len(t, pages, 1)
	require.Equal(t, 1, pages[0].PageIndex)
	require.Len(t, pages[0].SHA256, 64)
}

func TestExamUploadMultiPage(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	pageRepo := newMockPageRepo()
	svc := NewExamService(newMockPaperRepo(), pageRepo, dir, nil).WithTxManager(fakeTxManager{})

	pg := func(tag string) PageFile {
		b := pngWithTag(tag)
		return PageFile{Reader: bytes.NewReader(b), FileName: tag + ".png", FileSize: int64(len(b))}
	}
	p, id, err := svc.Upload(ctx, UploadExamReq{Pages: []PageFile{pg("a"), pg("b"), pg("c")}})
	require.NoError(t, err)
	require.Positive(t, p.ID)

	pages, err := pageRepo.ListByExam(ctx, id)
	require.NoError(t, err)
	require.Len(t, pages, 3)
	require.Equal(t, 1, pages[0].PageIndex)
	require.Equal(t, 3, pages[2].PageIndex)
	require.FileExists(t, storage.PagePath(dir, id, 3, "webp"))
}

// failingPageInsertRepo wraps a real PageRepository but fails on Insert, to prove
// the upload path rolls the exam row back when a page write fails.
type failingPageInsertRepo struct {
	domain.PageRepository
}

func (f failingPageInsertRepo) WithTx(domain.Tx) domain.PageRepository { return f }

func (failingPageInsertRepo) Insert(context.Context, *domain.Page) (int64, error) {
	return 0, errors.New("page insert failed")
}

func TestExamUploadRollsBackExamOnPageFailure(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := repository.Open(ctx, filepath.Join(dir, "paperly.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	examRepo := repository.NewExamRepoSQLite(db)
	svc := NewExamService(examRepo,
		failingPageInsertRepo{repository.NewPageRepoSQLite(db)},
		dir, nil).WithTxManager(db)

	pg := func(tag string) PageFile {
		b := pngWithTag(tag)
		return PageFile{Reader: bytes.NewReader(b), FileName: tag + ".png", FileSize: int64(len(b))}
	}
	_, id, err := svc.Upload(ctx, UploadExamReq{Pages: []PageFile{pg("a"), pg("b")}})
	require.Error(t, err)
	require.Zero(t, id, "failed upload must not return a husk exam id")

	items, _, err := examRepo.List(ctx, domain.ListExamFilter{})
	require.NoError(t, err)
	require.Empty(t, items, "exam row must roll back when a page write fails")

	entries, rerr := os.ReadDir(filepath.Join(dir, "assets", "exams"))
	if !os.IsNotExist(rerr) {
		require.NoError(t, rerr)
		require.Empty(t, entries, "written files must be cleaned up on failure")
	}
}

func TestExamUploadRejectsNoPages(t *testing.T) {
	svc := newTestExamService(newMockPaperRepo(), t.TempDir())
	_, _, err := svc.Upload(context.Background(), UploadExamReq{})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestExamUploadRejectsBadGrade(t *testing.T) {
	svc := newTestExamService(newMockPaperRepo(), t.TempDir())
	bad := 13
	_, _, err := svc.Upload(context.Background(), UploadExamReq{
		Pages: []PageFile{{Reader: bytes.NewReader(samplePNG()), FileSize: int64(len(samplePNG()))}},
		Grade: &bad,
	})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

// PDF is rejected by the business layer (uploads are image-only).
func TestExamUploadPDFRejected(t *testing.T) {
	dir := t.TempDir()
	svc := newTestExamService(newMockPaperRepo(), dir)
	pdf := []byte("%PDF-1.4 fake")

	_, _, err := svc.Upload(context.Background(), UploadExamReq{
		Pages: []PageFile{{Reader: bytes.NewReader(pdf), FileName: "期中.pdf", FileSize: int64(len(pdf))}},
	})
	require.ErrorIs(t, err, domain.ErrPDFNotSupported)
	// Nothing persisted: the exams dir must be empty.
	require.Equal(t, 0, countDirs(filepath.Join(dir, "assets", "exams")))
}

func TestExamUploadTooLarge(t *testing.T) {
	svc := newTestExamService(newMockPaperRepo(), t.TempDir())

	big := bytes.NewReader(append([]byte(samplePNG()), make([]byte, storage.MaxUploadBytes)...))
	_, _, err := svc.Upload(context.Background(), UploadExamReq{
		Pages: []PageFile{{Reader: big, FileSize: int64(storage.MaxUploadBytes + 4)}},
	})
	require.ErrorIs(t, err, domain.ErrFileTooLarge)

	// A lying FileSize must still be rejected if the content exceeds the limit
	// (LimitReader semantics).
	sneaky := io.MultiReader(
		bytes.NewReader(samplePNG()),
		bytes.NewReader(bytes.Repeat([]byte("a"), storage.MaxUploadBytes)),
	)
	_, _, err = svc.Upload(context.Background(), UploadExamReq{
		Pages: []PageFile{{Reader: sneaky, FileSize: 10}},
	})
	require.ErrorIs(t, err, domain.ErrFileTooLarge)
}

func TestExamUploadUnsupportedType(t *testing.T) {
	svc := newTestExamService(newMockPaperRepo(), t.TempDir())
	_, _, err := svc.Upload(context.Background(), UploadExamReq{
		Pages: []PageFile{{Reader: bytes.NewReader([]byte("MZ fake exe")), FileSize: 11}},
	})
	require.ErrorIs(t, err, domain.ErrUnsupportedFileType)
}

func TestExamDeleteSoft(t *testing.T) {
	ctx := context.Background()
	repo := newMockPaperRepo()
	svc := newTestExamService(repo, t.TempDir())

	_, id, err := svc.Upload(ctx, UploadExamReq{
		Pages: []PageFile{{Reader: bytes.NewReader(samplePNG()), FileSize: int64(len(samplePNG()))}},
	})
	require.NoError(t, err)

	require.NoError(t, svc.Delete(ctx, id))
	got, err := svc.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.ExamStatusDeleted, got.Status)

	// Status=0 = all statuses, including deleted; an explicit status filters.
	items, next, err := svc.List(ctx, domain.ListExamFilter{})
	require.NoError(t, err)
	require.Zero(t, next)
	require.Len(t, items, 1, "默认 List 含已删除试卷")

	items, _, err = svc.List(ctx, domain.ListExamFilter{Status: domain.ExamStatusActive})
	require.NoError(t, err)
	require.Empty(t, items, "只查正常不应命中软删卷")

	items, _, err = svc.List(ctx, domain.ListExamFilter{Status: domain.ExamStatusDeleted})
	require.NoError(t, err)
	require.Len(t, items, 1, "只查已删除应命中软删卷")

	require.ErrorIs(t, svc.Delete(ctx, 999), domain.ErrNotFound)
}

func TestExamRestore(t *testing.T) {
	ctx := context.Background()
	repo := newMockPaperRepo()
	svc := newTestExamService(repo, t.TempDir())

	_, id, err := svc.Upload(ctx, UploadExamReq{
		Pages: []PageFile{{Reader: bytes.NewReader(samplePNG()), FileSize: int64(len(samplePNG()))}},
	})
	require.NoError(t, err)

	// An active exam cannot be restored: the status gate reports ErrInvalidState
	// (distinct from "not found").
	_, err = svc.Restore(ctx, id)
	require.ErrorIs(t, err, domain.ErrInvalidState)

	require.NoError(t, svc.Delete(ctx, id))

	got, err := svc.Restore(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.ExamStatusActive, got.Status)

	items, _, err := svc.List(ctx, domain.ListExamFilter{Status: domain.ExamStatusActive})
	require.NoError(t, err)
	require.Len(t, items, 1)

	// Restoring again → ErrInvalidState; a nonexistent exam → ErrNotFound.
	_, err = svc.Restore(ctx, id)
	require.ErrorIs(t, err, domain.ErrInvalidState)
	_, err = svc.Restore(ctx, 999)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestExamGetDetailIncludesPages(t *testing.T) {
	ctx := context.Background()
	pageRepo := newMockPageRepo()
	svc := NewExamService(newMockPaperRepo(), pageRepo, t.TempDir(), nil).WithTxManager(fakeTxManager{})

	pg := func(tag string) PageFile {
		b := pngWithTag(tag)
		return PageFile{Reader: bytes.NewReader(b), FileName: tag + ".png", FileSize: int64(len(b))}
	}
	_, id, err := svc.Upload(ctx, UploadExamReq{Pages: []PageFile{pg("x"), pg("y")}})
	require.NoError(t, err)

	paper, pages, err := svc.GetDetail(ctx, id)
	require.NoError(t, err)
	require.Equal(t, id, paper.ID)
	require.Len(t, pages, 2)
}

func TestExamUpdateKidID(t *testing.T) {
	ctx := context.Background()
	repo := newMockPaperRepo()
	svc := newTestExamService(repo, t.TempDir())

	_, id, err := svc.Upload(ctx, UploadExamReq{
		Pages: []PageFile{{Reader: bytes.NewReader(samplePNG()), FileSize: int64(len(samplePNG()))}},
	})
	require.NoError(t, err)

	kid := int64(3)
	got, err := svc.Update(ctx, id, UpdateExamReq{KidID: &kid})
	require.NoError(t, err)
	require.NotNil(t, got.KidID)
	require.EqualValues(t, 3, *got.KidID)

	// Whole-record PUT: nil = unattribute.
	got, err = svc.Update(ctx, id, UpdateExamReq{KidID: nil})
	require.NoError(t, err)
	require.Nil(t, got.KidID, "整表 PUT 语义下 null = 取消归属")
}

// A tampered summary_source is rejected on update.
func TestExamUpdateRejectsInvalidSummarySource(t *testing.T) {
	ctx := context.Background()
	repo := newMockPaperRepo()
	svc := newTestExamService(repo, t.TempDir())

	_, id, err := svc.Upload(ctx, UploadExamReq{
		Pages: []PageFile{{Reader: bytes.NewReader(samplePNG()), FileSize: int64(len(samplePNG()))}},
	})
	require.NoError(t, err)

	bad := 999
	_, err = svc.Update(ctx, id, UpdateExamReq{SummarySource: &bad})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

// A tampered summary_source is rejected before any row or file is written.
func TestExamUploadRejectsInvalidSummarySource(t *testing.T) {
	svc := newTestExamService(newMockPaperRepo(), t.TempDir())
	bad := 999
	_, _, err := svc.Upload(context.Background(), UploadExamReq{
		Pages:         []PageFile{{}}, // read short-circuits at the validator, before touching the reader
		SummarySource: &bad,
	})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

// The manual source round-trips.
func TestExamUploadAcceptsValidSummarySource(t *testing.T) {
	ctx := context.Background()
	repo := newMockPaperRepo()
	svc := newTestExamService(repo, t.TempDir())

	src := int(domain.SummarySourceManual)
	p, _, err := svc.Upload(ctx, UploadExamReq{
		Pages:         []PageFile{{Reader: bytes.NewReader(samplePNG()), FileSize: int64(len(samplePNG()))}},
		SummarySource: &src,
	})
	require.NoError(t, err)
	require.NotNil(t, p.SummarySource)
	require.Equal(t, domain.SummarySourceManual, domain.SummarySource(*p.SummarySource))
}

// When the row writes succeed but the commit fails (rows roll back, files already on disk),
// the uploaded directory must be swept.
func TestExamUploadRemovesDirOnCommitFailure(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	svc := NewExamService(newMockPaperRepo(), newMockPageRepo(), dir, nil).WithTxManager(commitFailTxManager{})

	_, _, err := svc.Upload(ctx, UploadExamReq{
		Pages: []PageFile{{Reader: bytes.NewReader(samplePNG()), FileSize: int64(len(samplePNG()))}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "commit failed")

	// The mock repo hands out id=1 for the first insert; its page dir must be gone.
	_, statErr := os.Stat(storage.PageDir(dir, 1))
	require.True(t, os.IsNotExist(statErr), "commit 失败后不得残留已写文件的目录")
}

func countDirs(path string) int {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}

// samplePNG returns a real decodable PNG (the WebP transcode path needs real pixels).
func samplePNG() []byte {
	return encodeTestPNG("sample")
}

// pngWithTag returns a real PNG with different content (for multi-page/different-sha cases).
func pngWithTag(tag string) []byte {
	return encodeTestPNG(tag)
}

// encodeTestPNG builds a solid-color PNG; tag determines the color so different calls
// yield different bytes.
func encodeTestPNG(tag string) []byte {
	var h uint8
	for i := 0; i < len(tag); i++ {
		h = h*31 + tag[i]
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	c := color.RGBA{R: h, G: 0x40, B: 0x80, A: 0xFF}
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

func TestExamGetPageFile(t *testing.T) {
	ctx := context.Background()
	repo := newMockPaperRepo()
	pageRepo := newMockPageRepo()
	dir := t.TempDir()
	svc := NewExamService(repo, pageRepo, dir, nil).WithTxManager(fakeTxManager{})

	img := samplePNG()
	_, id, err := svc.Upload(ctx, UploadExamReq{
		Pages:      []PageFile{{Reader: bytes.NewReader(img), FileName: "p.png", FileSize: int64(len(img))}},
		UploadedBy: domain.UploadedByParent,
	})
	require.NoError(t, err)
	pages, err := pageRepo.ListByExam(ctx, id)
	require.NoError(t, err)
	require.Len(t, pages, 1)

	res, err := svc.GetPageFile(ctx, pages[0].ID)
	require.NoError(t, err)
	require.Equal(t, "image/webp", res.ContentType)
	// The persisted bytes must be WebP: verify the RIFF/WEBP container header.
	require.GreaterOrEqual(t, len(res.Data), 12)
	require.Equal(t, "RIFF", string(res.Data[0:4]))
	require.Equal(t, "WEBP", string(res.Data[8:12]))
	require.NotEqual(t, img, res.Data, "上传的 PNG 必须已被转码为 WebP")

	_, err = svc.GetPageFile(ctx, 99999)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

// Path containment check: if the file_path in the DB is polluted to point outside dataDir
// (e.g. a bad migration), the file must not be read.
func TestExamGetPageFileRejectsTraversal(t *testing.T) {
	ctx := context.Background()
	repo := newMockPaperRepo()
	pageRepo := newMockPageRepo()
	dir := t.TempDir()
	svc := NewExamService(repo, pageRepo, dir, nil).WithTxManager(fakeTxManager{})

	outside := filepath.Join(t.TempDir(), "secret.png")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	pid, err := pageRepo.Insert(ctx, &domain.Page{
		ExamID: 1, PageIndex: 1, FilePath: outside, SHA256: "x",
	})
	require.NoError(t, err)

	_, err = svc.GetPageFile(ctx, pid)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func (m *mockPaperRepo) WithTx(tx domain.Tx) domain.ExamRepository { return m }

func (m *mockPageRepo) WithTx(tx domain.Tx) domain.PageRepository { return m }
