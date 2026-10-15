package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"paperly/internal/domain"
	"paperly/internal/storage"
)

// pagesOpsPriority is the queue priority of the page-reorder task (tasks.kind=5), on
// the 0-100 scale where higher runs first and the queue default is 50. The parent
// clicked "save" and is waiting on the result, so it sits one tier above routine
// maintenance but still below grade advancement (80), which is time-sensitive because
// it takes effect across days.
const pagesOpsPriority = 60

// maxPagesAfterOps caps the total page count after reordering (same rule as the
// single-upload cap MaxUploadPages).
const maxPagesAfterOps = MaxUploadPages

// PagesOpsReq is the input for one "reorder pages" submission (delete, add and
// reorder may be mixed).
type PagesOpsReq struct {
	ExamID int64
	// DeletePageIDs are the ids of pages to delete; may be empty.
	DeletePageIDs []int64
	// FinalOrder is the final page order: each entry is the decimal string of an
	// existing page id, or "new_N" (N indexes into Files). The order maps to pages
	// 1..K (page_index = index + 1, including slots taken by new_N). It must cover
	// exactly all surviving existing pages plus all new files.
	FinalOrder []string
	// Files are the new page images, one-to-one with new_N in order; may be empty.
	Files []PageFile
}

// pagesOpsPlan is the validated execution plan: rows to delete and rows to reorder
// (final index + final path).
type pagesOpsPlan struct {
	deletes  []int64
	reorders []domain.PageReorder
}

// PagesOps performs a page reorder:
//
//  1. Validate final_order is a complete permutation of surviving pages + new files
//     (pure memory, before any side effects).
//  2. Convert new page files to WebP and stage them at assets/tmp/{token}/new_N.webp,
//     cleaning up on failure.
//  3. In a single transaction, delete rows, renumber page_index/file_path and enqueue
//     tasks(kind=5).
//  4. Return the task id (the handler replies 202).
//
// New page rows are not inserted here because the files have not been moved to their
// final names yet (the Worker renames on disk), so inserting rows first would make the
// detail page image 404; the Worker adds the rows with InsertIfAbsent after renaming.
// Existing pages' file_path is updated to the final name within this transaction, so
// order and paths settle in one go and the Worker only has to move files per the detail
// (idempotent and retryable).
func (s *ExamService) PagesOps(ctx context.Context, req PagesOpsReq) (int64, error) {
	if _, err := s.repo.GetByID(ctx, req.ExamID); err != nil {
		return 0, err // ErrNotFound → handler 404
	}
	pages, err := s.pageRepo.ListByExam(ctx, req.ExamID)
	if err != nil {
		return 0, fmt.Errorf("list exam pages: %w", err)
	}
	plan, err := planPagesOps(req, pages, s.dataDir)
	if err != nil {
		return 0, err
	}

	// Stage new pages first. Files land before the transaction opens, so on rollback
	// only the temp dir needs cleaning and no half-written DB state remains; the reverse
	// order (transaction before files) would leave rows pointing at nonexistent files.
	taskRel, tempAbs, tempFiles, err := s.stageOpsWorkspace(req.Files)
	if err != nil {
		return 0, err
	}

	detail, err := buildReorderPagesDetail(s.dataDir, req.ExamID, pages, plan.deletes, req.FinalOrder, taskRel, tempFiles)
	if err != nil {
		if tempAbs != "" {
			_ = os.RemoveAll(tempAbs)
		}
		return 0, err
	}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		if tempAbs != "" {
			_ = os.RemoveAll(tempAbs)
		}
		return 0, fmt.Errorf("marshal reorder detail: %w", err)
	}

	taskID, err := s.pageRepo.RebuildPages(ctx, req.ExamID, plan.deletes, plan.reorders, &domain.Task{
		Kind:     domain.TaskKindReorderPages,
		Priority: pagesOpsPriority,
		Status:   domain.TaskStatusPending,
		Detail:   string(detailJSON),
	})
	if err != nil {
		if tempAbs != "" {
			_ = os.RemoveAll(tempAbs)
		}
		return 0, err
	}
	return taskID, nil
}

// stageOpsWorkspace creates the task's staging directory assets/tmp/{token}/ and routes
// new pages through the same pipeline as upload to new_N.webp. It returns the relative
// dir, the absolute dir and the file list.
//
// The directory is created even with no new pages: page swaps must first vacate the
// final-name space, and stage_{id} lands here (Worker phases 2/4). If any file fails,
// the created directory is cleaned up immediately so no orphans remain.
func (s *ExamService) stageOpsWorkspace(files []PageFile) (string, string, []reorderTempFile, error) {
	token, err := storage.NewTempToken()
	if err != nil {
		return "", "", nil, err
	}
	taskRel := storage.TempTaskRel(token)
	tempAbs := storage.TempTaskDir(s.dataDir, token)
	if err := os.MkdirAll(tempAbs, 0o750); err != nil {
		return "", "", nil, fmt.Errorf("mkdir task dir: %w", err)
	}
	fail := func(err error) (string, string, []reorderTempFile, error) {
		_ = os.RemoveAll(tempAbs)
		return "", "", nil, err
	}

	tempFiles := make([]reorderTempFile, 0, len(files))
	for i, f := range files {
		blob, err := s.stageOpsPage(f)
		if err != nil {
			return fail(fmt.Errorf("new page %d: %w", i+1, err))
		}
		rel := taskRel + fmt.Sprintf("/new_%d.webp", i)
		if err := os.WriteFile(filepath.Join(tempAbs, fmt.Sprintf("new_%d.webp", i)), blob, 0o640); err != nil {
			return fail(fmt.Errorf("write temp page %d: %w", i+1, err))
		}
		tempFiles = append(tempFiles, reorderTempFile{Path: rel, SHA256: sha256Hex(blob)})
	}
	return taskRel, tempAbs, tempFiles, nil
}

// stageOpsPage runs the same image pipeline as Upload: sniff type, limited read,
// EXIF orientation + scale long edge to 5120, encode to WebP at the configured tier.
func (s *ExamService) stageOpsPage(f PageFile) ([]byte, error) {
	ext, sniffed, err := storage.SniffExtension(f.Reader)
	if err != nil {
		return nil, err
	}
	if ext == "" {
		return nil, domain.ErrUnsupportedFileType
	}
	if ext == "pdf" {
		return nil, domain.ErrPDFNotSupported
	}
	blob, err := io.ReadAll(io.LimitReader(sniffed, storage.MaxUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if len(blob) > storage.MaxUploadBytes {
		return nil, fmt.Errorf("%w: page exceeds size limit", domain.ErrFileTooLarge)
	}
	webp, err := storage.ImageToWebP(bytes.NewReader(blob), storage.WebPMaxEdge, s.image.Preset())
	if err != nil {
		return nil, fmt.Errorf("%w: image could not be decoded or transcoded", domain.ErrInvalidInput)
	}
	return webp, nil
}

// planPagesOps validates the reorder plan and computes the execution details (pure
// memory, no side effects). It passes when final_order is a non-duplicating
// permutation of exactly "non-deleted existing pages ∪ new_0..new_{n-1}", with at
// least 1 and at most maxPagesAfterOps pages.
func planPagesOps(req PagesOpsReq, pages []*domain.Page, dataDir string) (*pagesOpsPlan, error) {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", domain.ErrInvalidInput, fmt.Sprintf(format, args...))
	}
	if len(req.FinalOrder) == 0 {
		return nil, invalid("empty final_order (at least 1 page must remain)")
	}
	if len(req.FinalOrder) > maxPagesAfterOps {
		return nil, invalid("reordered %d pages exceed the %d-page limit", len(req.FinalOrder), maxPagesAfterOps)
	}

	byID := make(map[int64]*domain.Page, len(pages))
	for _, p := range pages {
		byID[p.ID] = p
	}

	deleted := make(map[int64]bool, len(req.DeletePageIDs))
	deletes := make([]int64, 0, len(req.DeletePageIDs))
	for _, id := range req.DeletePageIDs {
		if deleted[id] {
			return nil, invalid("duplicate delete id: %d", id)
		}
		if _, ok := byID[id]; !ok {
			return nil, invalid("deleted non-existent page: %d", id)
		}
		deleted[id] = true
		deletes = append(deletes, id)
	}

	seen := make(map[int64]bool, len(req.FinalOrder))
	seenNew := make(map[int]bool, len(req.Files))
	reorders := make([]domain.PageReorder, 0, len(req.FinalOrder))
	for pos, entry := range req.FinalOrder {
		idx := pos + 1
		if strings.HasPrefix(entry, "new_") {
			n, err := strconv.Atoi(strings.TrimPrefix(entry, "new_"))
			if err != nil || n < 0 || n >= len(req.Files) {
				return nil, invalid("unknown new-page placeholder %q (uploaded %d files)", entry, len(req.Files))
			}
			if seenNew[n] {
				return nil, invalid("duplicate new-page placeholder %q", entry)
			}
			seenNew[n] = true
			continue
		}
		id, err := strconv.ParseInt(entry, 10, 64)
		if err != nil {
			return nil, invalid("invalid final_order item %q", entry)
		}
		p, ok := byID[id]
		if !ok {
			return nil, invalid("final_order references non-existent page: %d", id)
		}
		if deleted[id] {
			return nil, invalid("page %d both deleted and reordered", id)
		}
		if seen[id] {
			return nil, invalid("page %d duplicated in final_order", id)
		}
		seen[id] = true
		reorders = append(reorders, domain.PageReorder{
			ID:          id,
			NewIndex:    idx,
			NewFilePath: storage.PagePath(dataDir, req.ExamID, idx, pageExt(p.FilePath)),
		})
	}

	// All existing pages must be present: neither deleted nor ordered means the client
	// omitted them, and silently dropping a page is worse than an error.
	for _, p := range pages {
		if !deleted[p.ID] && !seen[p.ID] {
			return nil, invalid("page %d missing from final_order (neither deleted nor reordered)", p.ID)
		}
	}
	// Files and new_N must be one-to-one: an extra file with no placeholder is wasted,
	// and a placeholder with no file means a later 404.
	if len(seenNew) != len(req.Files) {
		return nil, invalid("%d files mismatch %d new_* placeholders in final_order", len(req.Files), len(seenNew))
	}
	return &pagesOpsPlan{deletes: deletes, reorders: reorders}, nil
}

// reorderTempFile is a staged new-page file in the detail: a path relative to dataDir
// plus the persisted hash.
type reorderTempFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// reorderPagesDetail is the detail payload of tasks(kind=5) (the written side). The
// reader is internal/worker, which does not import this package, so each side keeps its
// own copy of the struct: fields and JSON keys must stay in sync (update
// worker/reorder_pages_handler.go and the tasks example in DATABASE_DESIGN).
type reorderPagesDetail struct {
	ExamID int64 `json:"exam_id"`
	// TaskDir is the task's staging directory (relative to dataDir, always non-empty):
	// new pages new_N.webp and the stage_{id} swap targets for existing pages live here.
	TaskDir string `json:"task_dir"`
	// TempFiles' order is the N of new_N.
	TempFiles []reorderTempFile `json:"temp_files"`
	// FinalOrder is the final page order: "11" (existing page id) or "new_0".
	FinalOrder []string `json:"final_order"`
	// PageFiles records the pre-rename page file relative paths (including
	// deleted pages) — the Worker reads no DB, locating source files solely by
	// this map.
	PageFiles map[int64]string `json:"page_files"`
	// DeletePageIDs are the ids of pages to delete (intersection with PageFiles;
	// their files are removed).
	DeletePageIDs []int64 `json:"delete_page_ids"`
}

// buildReorderPagesDetail assembles the self-contained payload for the Worker: page
// file relative paths all come from the current DB (pre-rename), and out-of-bounds
// paths are rejected here before enqueue, avoiding guaranteed task failures.
func buildReorderPagesDetail(
	dataDir string, examID int64, pages []*domain.Page,
	deletes []int64, finalOrder []string,
	taskRel string, tempFiles []reorderTempFile,
) (*reorderPagesDetail, error) {
	examPrefix := fmt.Sprintf("assets/exams/%d/", examID)
	pageFiles := make(map[int64]string, len(pages))
	for _, p := range pages {
		rel, err := filepath.Rel(dataDir, p.FilePath)
		if err != nil {
			return nil, fmt.Errorf("resolve page %d path: %w", p.ID, err)
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, examPrefix) {
			return nil, fmt.Errorf("%w: page %d file path not under %s: %s", domain.ErrInvalidInput, p.ID, examPrefix, rel)
		}
		pageFiles[p.ID] = rel
	}
	if tempFiles == nil {
		tempFiles = []reorderTempFile{}
	}
	return &reorderPagesDetail{
		ExamID:        examID,
		TaskDir:       taskRel,
		TempFiles:     tempFiles,
		FinalOrder:    finalOrder,
		PageFiles:     pageFiles,
		DeletePageIDs: deletes,
	}, nil
}

// pageExt returns the page image extension (dot stripped, lowercased). Paths written by
// the upload pipeline always carry an extension.
func pageExt(filePath string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
}
