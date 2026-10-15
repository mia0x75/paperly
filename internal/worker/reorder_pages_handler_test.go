package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	"paperly/internal/storage"
)

// In-memory PageStore: deduplicates by (exam_id, page_index) mimicking
// ON CONFLICT DO NOTHING, and counts calls.
type fakePageStore struct {
	inserted []*domain.Page
	calls    int
	err      error
}

func (f *fakePageStore) InsertIfAbsent(_ context.Context, pages []*domain.Page) (int, error) {
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	n := 0
	for _, p := range pages {
		dup := false
		for _, cur := range f.inserted {
			if cur.ExamID == p.ExamID && cur.PageIndex == p.PageIndex {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		clone := *p
		f.inserted = append(f.inserted, &clone)
		n++
	}
	return n, nil
}

// Creates assets/exams/{examID}/page_00N.webp under dataDir (content as marker).
func seedReorderFS(t *testing.T, dataDir string, examID int64, contents ...string) []string {
	t.Helper()
	paths := make([]string, 0, len(contents))
	for i, body := range contents {
		p := storage.PagePath(dataDir, examID, i+1, "webp")
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o640))
		paths = append(paths, p)
	}
	return paths
}

// Mimics the staging directory created by service before enqueue (the worker
// judges whether the last round finished by its presence/absence; it must be
// present on the first run).
func makeTaskDir(t *testing.T, dataDir, rel string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, filepath.FromSlash(rel)), 0o750))
}

// Builds a kind=5 task (detail serialized directly from the struct, ensuring
// isomorphism with service).
func writeReorderTask(t *testing.T, d *reorderPagesDetail) *domain.Task {
	t.Helper()
	raw, err := json.Marshal(d)
	require.NoError(t, err)
	return &domain.Task{ID: 7, Kind: domain.TaskKindReorderPages, Priority: 60, Status: domain.TaskStatusPending, Detail: string(raw)}
}

// Deps with a real temp DataDir and a fakePageStore.
func reorderDeps(t *testing.T) (Deps, string, *fakePageStore) {
	t.Helper()
	deps := testDeps()
	dir := t.TempDir()
	deps.DataDir = dir
	store := &fakePageStore{}
	deps.Pages = store
	return deps, dir, store
}

// Covers phases 1/2/4/6: page deletion + swap (including freeing someone else's
// final name for a survivor) relies on disk moves, with zero DB reads.
func TestHandleReorderPagesDeleteAndSwap(t *testing.T) {
	deps, dir, store := reorderDeps(t)
	paths := seedReorderFS(t, dir, 14, "A", "B", "C") // page_001=A page_002=B page_003=C
	makeTaskDir(t, dir, "assets/tmp/tok1")

	// Delete page_002; final order [page3, page1]—page1 must move into the final
	// name just vacated by the deleted page_002.
	task := writeReorderTask(t, &reorderPagesDetail{
		ExamID:        14,
		TaskDir:       "assets/tmp/tok1",
		FinalOrder:    []string{"103", "101"},
		PageFiles:     map[int64]string{101: "assets/exams/14/page_001.webp", 102: "assets/exams/14/page_002.webp", 103: "assets/exams/14/page_003.webp"},
		DeletePageIDs: []int64{102},
	})
	require.NoError(t, handleReorderPages(context.Background(), deps, task))

	read := func(p string) string {
		b, err := os.ReadFile(p)
		require.NoError(t, err)
		return string(b)
	}
	require.Equal(t, "C", read(paths[0]), "page3 换到 1 号位")
	require.Equal(t, "A", read(paths[1]), "page1 换到 2 号位（原 page2 被删后腾出的终名）")
	require.NoFileExists(t, paths[2], "page3 原文件已搬走")
	require.NoDirExists(t, filepath.Join(dir, "assets", "tmp", "tok1"), "阶段 6 清理暂存目录")
	require.Zero(t, store.calls, "无新页不调 InsertIfAbsent")
}

// Covers phases 3/5: new pages are placed, then rows are inserted (row and file
// share the same position).
func TestHandleReorderPagesWithNewFile(t *testing.T) {
	deps, dir, store := reorderDeps(t)
	paths := seedReorderFS(t, dir, 14, "A")
	tempDir := filepath.Join(dir, "assets", "tmp", "tok2")
	require.NoError(t, os.MkdirAll(tempDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "new_0.webp"), []byte("NEW"), 0o640))

	task := writeReorderTask(t, &reorderPagesDetail{
		ExamID:     14,
		TaskDir:    "assets/tmp/tok2",
		TempFiles:  []reorderTempFile{{Path: "assets/tmp/tok2/new_0.webp", SHA256: "sha-new"}},
		FinalOrder: []string{"101", "new_0"},
		PageFiles:  map[int64]string{101: "assets/exams/14/page_001.webp"},
	})
	require.NoError(t, handleReorderPages(context.Background(), deps, task))

	b, err := os.ReadFile(paths[0])
	require.NoError(t, err)
	require.Equal(t, "A", string(b), "原地页不动")
	newPath := storage.PagePath(dir, 14, 2, "webp")
	b, err = os.ReadFile(newPath)
	require.NoError(t, err)
	require.Equal(t, "NEW", string(b), "新页落到 2 号终名")

	require.Len(t, store.inserted, 1)
	p := store.inserted[0]
	require.EqualValues(t, 14, p.ExamID)
	require.Equal(t, 2, p.PageIndex)
	require.Equal(t, newPath, p.FilePath, "行的 file_path 必须是磁盘真实终名")
	require.Equal(t, "sha-new", p.SHA256)
	require.NoDirExists(t, tempDir)
}

// On a failed rerun every phase skips if it exists, so repeated execution does not
// break the final state or duplicate row inserts.
func TestHandleReorderPagesRerunIdempotent(t *testing.T) {
	deps, dir, store := reorderDeps(t)
	paths := seedReorderFS(t, dir, 14, "A")
	tempDir := filepath.Join(dir, "assets", "tmp", "tok3")
	require.NoError(t, os.MkdirAll(tempDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "new_0.webp"), []byte("NEW"), 0o640))

	task := writeReorderTask(t, &reorderPagesDetail{
		ExamID:     14,
		TaskDir:    "assets/tmp/tok3",
		TempFiles:  []reorderTempFile{{Path: "assets/tmp/tok3/new_0.webp", SHA256: "sha-new"}},
		FinalOrder: []string{"new_0", "101"},
		PageFiles:  map[int64]string{101: "assets/exams/14/page_001.webp"},
	})
	require.NoError(t, handleReorderPages(context.Background(), deps, task))
	require.NoError(t, handleReorderPages(context.Background(), deps, task), "重跑应幂等成功")

	// Final order [new_0, 101]: position 1 is the new page, position 2 is the
	// original page101.
	read := func(p string) string {
		b, err := os.ReadFile(p)
		require.NoError(t, err)
		return string(b)
	}
	require.Equal(t, "NEW", read(paths[0]), "新页在 1 号位")
	require.Equal(t, "A", read(storage.PagePath(dir, 14, 2, "webp")), "存量页换到 2 号位")
	require.Len(t, store.inserted, 1, "ON CONFLICT 语义：第二次不重复插行")
	require.Equal(t, 2, store.calls)
	require.Equal(t, 1, store.inserted[0].PageIndex)
	require.Equal(t, paths[0], store.inserted[0].FilePath)
}

// An untrusted payload is always nonRetryable, and out-of-bounds paths must not
// touch the filesystem.
func TestHandleReorderPagesInvalidPayloadNonRetryable(t *testing.T) {
	base := func() *reorderPagesDetail {
		return &reorderPagesDetail{
			ExamID:     14,
			TaskDir:    "assets/tmp/tok",
			FinalOrder: []string{"101"},
			PageFiles:  map[int64]string{101: "assets/exams/14/page_001.webp"},
		}
	}
	cases := []struct {
		name   string
		mutate func(d *reorderPagesDetail)
	}{
		{"page_files 路径穿越", func(d *reorderPagesDetail) {
			d.PageFiles = map[int64]string{101: "assets/exams/14/../../secret.webp"}
		}},
		{"page_files 路径为绝对路径", func(d *reorderPagesDetail) {
			d.PageFiles = map[int64]string{101: "/etc/passwd"}
		}},
		{"page_files 路径属于别的试卷", func(d *reorderPagesDetail) {
			d.PageFiles = map[int64]string{101: "assets/exams/15/page_001.webp"}
		}},
		{"task_dir 越界", func(d *reorderPagesDetail) {
			d.TaskDir = "assets/exams/14"
		}},
		{"task_dir 为空", func(d *reorderPagesDetail) {
			d.TaskDir = ""
		}},
		{"final_order 引用 page_files 外的页", func(d *reorderPagesDetail) {
			d.FinalOrder = []string{"999"}
		}},
		{"存活页漏排", func(d *reorderPagesDetail) {
			d.FinalOrder = nil
		}},
		{"new 占位越界", func(d *reorderPagesDetail) {
			d.FinalOrder = []string{"101", "new_0"} // TempFiles is empty
		}},
		{"exam_id 非法", func(d *reorderPagesDetail) {
			d.ExamID = 0
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps, dir, _ := reorderDeps(t)
			seedReorderFS(t, dir, 14, "A") // scene: however bad the payload, files must be untouched
			d := base()
			tc.mutate(d)
			requireNonRetryable(t, handleReorderPages(context.Background(), deps, writeReorderTask(t, d)))

			b, err := os.ReadFile(storage.PagePath(dir, 14, 1, "webp"))
			require.NoError(t, err)
			require.Equal(t, "A", string(b), "非法载荷不得改动任何文件")
		})
	}

	t.Run("detail 不是合法 JSON", func(t *testing.T) {
		deps, _, _ := reorderDeps(t)
		requireNonRetryable(t, handleReorderPages(context.Background(), deps,
			&domain.Task{ID: 1, Kind: domain.TaskKindReorderPages, Detail: "{oops"}))
	})
}

// A genuinely lost file is a runtime problem → retryable (not nonRetryable); whereas
// "the to-be-deleted file never existed" is not an error (it may have been deleted
// last round).
func TestHandleReorderPagesMissingFileRetryable(t *testing.T) {
	t.Run("存活页文件三处全无", func(t *testing.T) {
		deps, dir, _ := reorderDeps(t)
		seedReorderFS(t, dir, 14, "A") // only page_001 is created, page_files claims there is also 002
		makeTaskDir(t, dir, "assets/tmp/tok4")
		task := writeReorderTask(t, &reorderPagesDetail{
			ExamID:     14,
			TaskDir:    "assets/tmp/tok4",
			FinalOrder: []string{"101", "102"},
			PageFiles: map[int64]string{
				101: "assets/exams/14/page_001.webp",
				102: "assets/exams/14/page_002.webp",
			},
		})
		err := handleReorderPages(context.Background(), deps, task)
		require.Error(t, err)
		var nre *errNonRetryable
		require.False(t, errors.As(err, &nre), "文件丢失应可重试，而非直接判死")
	})

	t.Run("待删文件本就不存在视为成功", func(t *testing.T) {
		deps, dir, _ := reorderDeps(t)
		paths := seedReorderFS(t, dir, 14, "A")
		makeTaskDir(t, dir, "assets/tmp/tok5")
		task := writeReorderTask(t, &reorderPagesDetail{
			ExamID:        14,
			TaskDir:       "assets/tmp/tok5",
			FinalOrder:    []string{"101"},
			PageFiles:     map[int64]string{101: "assets/exams/14/page_001.webp", 102: "assets/exams/14/page_002.webp"},
			DeletePageIDs: []int64{102},
		})
		require.NoError(t, handleReorderPages(context.Background(), deps, task))
		b, err := os.ReadFile(paths[0])
		require.NoError(t, err)
		require.Equal(t, "A", string(b))
	})
}
