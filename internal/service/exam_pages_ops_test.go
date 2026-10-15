package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	"paperly/internal/storage"
)

// seedOpsExam builds an exam with real page files on disk (n pages, page_NNN.webp), returning the exam and pages.
func seedOpsExam(t *testing.T, paperRepo *mockPaperRepo, pageRepo *mockPageRepo, dataDir string, n int) (*domain.Exam, []*domain.Page) {
	t.Helper()
	ctx := context.Background()
	examID, err := paperRepo.Insert(ctx, &domain.Exam{Status: domain.ExamStatusActive})
	require.NoError(t, err)
	exam, err := paperRepo.GetByID(ctx, examID)
	require.NoError(t, err)

	pages := make([]*domain.Page, 0, n)
	for idx := 1; idx <= n; idx++ {
		path := storage.PagePath(dataDir, examID, idx, "webp")
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf("page-%d", idx)), 0o640))
		id, err := pageRepo.Insert(ctx, &domain.Page{
			ExamID: examID, PageIndex: idx, FilePath: path,
			SHA256: fmt.Sprintf("sha-%d", idx),
		})
		require.NoError(t, err)
		pages = append(pages, &domain.Page{ID: id, ExamID: examID, PageIndex: idx, FilePath: path})
	}
	return exam, pages
}

// idStrs converts page ids into the string form used in final_order.
func idStrs(ids ...int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, fmt.Sprintf("%d", id))
	}
	return out
}

// lastTask fetches the most recently enqueued task from the fake (for payload assertions).
func lastTask(t *testing.T, pageRepo *mockPageRepo) *domain.Task {
	t.Helper()
	require.NotEmpty(t, pageRepo.enqueued, "应有任务入队")
	return pageRepo.enqueued[len(pageRepo.enqueued)-1]
}

// parseDetail parses an enqueued task's detail (structurally identical to the worker side).
func parseDetail(t *testing.T, task *domain.Task) reorderPagesDetail {
	t.Helper()
	require.Equal(t, domain.TaskKindReorderPages, task.Kind)
	require.Equal(t, pagesOpsPriority, task.Priority)
	require.Equal(t, domain.TaskStatusPending, task.Status)
	var d reorderPagesDetail
	require.NoError(t, json.Unmarshal([]byte(task.Detail), &d))
	return d
}

// TestPagesOpsReorderOnly pure reorder: rows settled + task enqueued + staging dir always created (a stage landing point even without new pages).
func TestPagesOpsReorderOnly(t *testing.T) {
	ctx := context.Background()
	paperRepo, pageRepo := newMockPaperRepo(), newMockPageRepo()
	dir := t.TempDir()
	svc := NewExamService(paperRepo, pageRepo, dir, nil).WithTxManager(fakeTxManager{})
	exam, pages := seedOpsExam(t, paperRepo, pageRepo, dir, 3)

	// The order is changed to [p3, p1, p2].
	taskID, err := svc.PagesOps(ctx, PagesOpsReq{
		ExamID:     exam.ID,
		FinalOrder: idStrs(pages[2].ID, pages[0].ID, pages[1].ID),
	})
	require.NoError(t, err)
	require.Positive(t, taskID)

	// DB: order and paths settle in one go.
	got, err := pageRepo.ListByExam(ctx, exam.ID)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, []int64{pages[2].ID, pages[0].ID, pages[1].ID},
		[]int64{got[0].ID, got[1].ID, got[2].ID})
	for i, p := range got {
		require.Equal(t, i+1, p.PageIndex)
		require.Equal(t, storage.PagePath(dir, exam.ID, i+1, "webp"), p.FilePath,
			"file_path 应同步到终名")
	}

	// Payload: self-contained + staging dir (relative paths).
	d := parseDetail(t, lastTask(t, pageRepo))
	require.Equal(t, exam.ID, d.ExamID)
	require.Equal(t, idStrs(pages[2].ID, pages[0].ID, pages[1].ID), d.FinalOrder)
	require.Empty(t, d.DeletePageIDs)
	require.Empty(t, d.TempFiles)
	require.Len(t, d.PageFiles, 3)
	require.True(t, strings.HasPrefix(d.TaskDir, "assets/tmp/"))
	for _, p := range pages {
		require.Equal(t, fmt.Sprintf("assets/exams/%d/page_%03d.webp", exam.ID, p.PageIndex), d.PageFiles[p.ID],
			"page_files 记录改名前的相对路径")
	}
	require.DirExists(t, filepath.Join(dir, filepath.FromSlash(d.TaskDir)),
		"纯重排也要建暂存目录（换位页的 stage 落点）")
}

// TestPagesOpsDeleteAndReorder deletes a page, filling the gap with the remaining pages.
func TestPagesOpsDeleteAndReorder(t *testing.T) {
	ctx := context.Background()
	paperRepo, pageRepo := newMockPaperRepo(), newMockPageRepo()
	dir := t.TempDir()
	svc := NewExamService(paperRepo, pageRepo, dir, nil).WithTxManager(fakeTxManager{})
	exam, pages := seedOpsExam(t, paperRepo, pageRepo, dir, 3)

	_, err := svc.PagesOps(ctx, PagesOpsReq{
		ExamID:        exam.ID,
		DeletePageIDs: []int64{pages[1].ID},
		FinalOrder:    idStrs(pages[0].ID, pages[2].ID),
	})
	require.NoError(t, err)

	got, err := pageRepo.ListByExam(ctx, exam.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, []int64{pages[0].ID, pages[2].ID}, []int64{got[0].ID, got[1].ID})
	require.Equal(t, 2, got[1].PageIndex, "末页补位")

	d := parseDetail(t, lastTask(t, pageRepo))
	require.Equal(t, []int64{pages[1].ID}, d.DeletePageIDs)
	require.Len(t, d.PageFiles, 3, "被删页也在 page_files（Worker 要按它删文件）")
}

// TestPagesOpsWithNewFile new page: WebP staged + final_order placeholder + payload carries sha256.
func TestPagesOpsWithNewFile(t *testing.T) {
	ctx := context.Background()
	paperRepo, pageRepo := newMockPaperRepo(), newMockPageRepo()
	dir := t.TempDir()
	svc := NewExamService(paperRepo, pageRepo, dir, nil).WithTxManager(fakeTxManager{})
	exam, pages := seedOpsExam(t, paperRepo, pageRepo, dir, 2)

	taskID, err := svc.PagesOps(ctx, PagesOpsReq{
		ExamID:     exam.ID,
		FinalOrder: append(idStrs(pages[0].ID), "new_0", fmt.Sprintf("%d", pages[1].ID)),
		Files:      []PageFile{{Reader: bytes.NewReader(pngWithTag("new-page")), FileName: "n.png"}},
	})
	require.NoError(t, err)
	require.Positive(t, taskID)

	d := parseDetail(t, lastTask(t, pageRepo))
	require.Len(t, d.TempFiles, 1)
	require.Equal(t, d.TaskDir+"/new_0.webp", d.TempFiles[0].Path)
	require.NotEmpty(t, d.TempFiles[0].SHA256)

	// The staged file really lands on disk, and its sha256 matches the disk content (the Worker relies on it to write the new-page row).
	staged := filepath.Join(dir, filepath.FromSlash(d.TempFiles[0].Path))
	blob, err := os.ReadFile(staged)
	require.NoError(t, err)
	require.Equal(t, sha256Hex(blob), d.TempFiles[0].SHA256)

	// Existing pages reorder: p1→1 (in place), p2→3 (making room for new_0).
	got, err := pageRepo.ListByExam(ctx, exam.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, 1, got[0].PageIndex)
	require.Equal(t, 3, got[1].PageIndex, "新页占走 2 号位")
	require.Equal(t, storage.PagePath(dir, exam.ID, 3, "webp"), got[1].FilePath)

	// The new-page row is not inserted yet (the Worker inserts it after moving the files, to avoid a 404).
	for _, p := range got {
		require.NotEqual(t, 2, p.PageIndex)
	}
}

// TestPagesOpsValidationErrors validation matrix: all rejected before any side effect, always ErrInvalidInput.
func TestPagesOpsValidationErrors(t *testing.T) {
	newSvc := func(t *testing.T) (*ExamService, *mockPaperRepo, *mockPageRepo, *domain.Exam, []*domain.Page, string) {
		t.Helper()
		paperRepo, pageRepo := newMockPaperRepo(), newMockPageRepo()
		dir := t.TempDir()
		svc := NewExamService(paperRepo, pageRepo, dir, nil).WithTxManager(fakeTxManager{})
		exam, pages := seedOpsExam(t, paperRepo, pageRepo, dir, 3)
		return svc, paperRepo, pageRepo, exam, pages, dir
	}
	ids := func(pages []*domain.Page) []string {
		return idStrs(pages[0].ID, pages[1].ID, pages[2].ID)
	}

	cases := []struct {
		name string
		req  func(pages []*domain.Page) PagesOpsReq
	}{
		{"final_order 为空", func(p []*domain.Page) PagesOpsReq {
			return PagesOpsReq{FinalOrder: nil}
		}},
		{"引用不存在的页", func(p []*domain.Page) PagesOpsReq {
			return PagesOpsReq{FinalOrder: []string{"99999", fmt.Sprintf("%d", p[1].ID), fmt.Sprintf("%d", p[2].ID)}}
		}},
		{"id 重复", func(p []*domain.Page) PagesOpsReq {
			return PagesOpsReq{FinalOrder: []string{fmt.Sprintf("%d", p[0].ID), fmt.Sprintf("%d", p[0].ID), fmt.Sprintf("%d", p[2].ID)}}
		}},
		{"同页既删又排", func(p []*domain.Page) PagesOpsReq {
			return PagesOpsReq{
				DeletePageIDs: []int64{p[0].ID},
				FinalOrder:    ids(p),
			}
		}},
		{"存活页漏排（既没删也没进 final_order）", func(p []*domain.Page) PagesOpsReq {
			return PagesOpsReq{FinalOrder: idStrs(p[0].ID, p[1].ID)}
		}},
		{"删不存在的页", func(p []*domain.Page) PagesOpsReq {
			return PagesOpsReq{DeletePageIDs: []int64{99999}, FinalOrder: ids(p)}
		}},
		{"删除项重复", func(p []*domain.Page) PagesOpsReq {
			return PagesOpsReq{DeletePageIDs: []int64{p[0].ID, p[0].ID}, FinalOrder: idStrs(p[1].ID, p[2].ID)}
		}},
		{"new 占位无对应文件", func(p []*domain.Page) PagesOpsReq {
			return PagesOpsReq{FinalOrder: []string{"new_0", fmt.Sprintf("%d", p[1].ID), fmt.Sprintf("%d", p[2].ID)}}
		}},
		{"文件数与占位数不一致", func(p []*domain.Page) PagesOpsReq {
			return PagesOpsReq{
				FinalOrder: append(idStrs(p[0].ID, p[1].ID), "new_0"),
				Files: []PageFile{
					{Reader: bytes.NewReader(pngWithTag("a")), FileName: "a.png"},
					{Reader: bytes.NewReader(pngWithTag("b")), FileName: "b.png"},
				},
			}
		}},
		{"final_order 含非法项", func(p []*domain.Page) PagesOpsReq {
			return PagesOpsReq{FinalOrder: []string{"11abc", fmt.Sprintf("%d", p[1].ID), fmt.Sprintf("%d", p[2].ID)}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, pageRepo, exam, pages, _ := newSvc(t)
			req := tc.req(pages)
			req.ExamID = exam.ID
			_, err := svc.PagesOps(context.Background(), req)
			require.ErrorIs(t, err, domain.ErrInvalidInput)
			require.Empty(t, pageRepo.enqueued, "校验失败不得入队")
		})
	}

	t.Run("试卷不存在", func(t *testing.T) {
		svc, _, pageRepo, _, pages, _ := newSvc(t)
		_, err := svc.PagesOps(context.Background(), PagesOpsReq{ExamID: 99999, FinalOrder: ids(pages)})
		require.ErrorIs(t, err, domain.ErrNotFound)
		require.Empty(t, pageRepo.enqueued)
	})

	t.Run("整理后超过 50 页上限", func(t *testing.T) {
		_, _, _, exam, pages, dir := newSvc(t)
		big := make([]*domain.Page, 51)
		for i := range big {
			big[i] = &domain.Page{ID: pages[0].ID, ExamID: exam.ID, PageIndex: i + 1,
				FilePath: filepath.Join(dir, "p.webp")}
		}
		req := PagesOpsReq{ExamID: exam.ID, FinalOrder: make([]string, 51)}
		_, err := planPagesOps(req, big, dir)
		require.ErrorIs(t, err, domain.ErrInvalidInput)
	})
}

// TestPagesOpsFailureCleansTempDir transaction enqueue fails: the staging dir must be removed, leaving no orphans.
func TestPagesOpsFailureCleansTempDir(t *testing.T) {
	ctx := context.Background()
	paperRepo, pageRepo := newMockPaperRepo(), newMockPageRepo()
	dir := t.TempDir()
	svc := NewExamService(paperRepo, pageRepo, dir, nil).WithTxManager(fakeTxManager{})
	exam, pages := seedOpsExam(t, paperRepo, pageRepo, dir, 1)
	pageRepo.rebuildErr = errors.New("db down")

	_, err := svc.PagesOps(ctx, PagesOpsReq{
		ExamID:     exam.ID,
		FinalOrder: append(idStrs(pages[0].ID), "new_0"),
		Files:      []PageFile{{Reader: bytes.NewReader(pngWithTag("x")), FileName: "x.png"}},
	})
	require.Error(t, err)

	tmpRoot := filepath.Join(dir, "assets", "tmp")
	entries, err := os.ReadDir(tmpRoot)
	if err != nil && !os.IsNotExist(err) {
		require.NoError(t, err)
	}
	require.Empty(t, entries, "失败后暂存目录不得残留")
}
