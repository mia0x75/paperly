package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"paperly/internal/domain"
	"paperly/internal/storage"
)

// handleReorderPages consumes kind=5 page reorder.
//
// Responsibility boundary: the request transaction has already made the DB changes (existing page
// index/file_path in place, deleted rows removed, the task row enqueued in the same transaction);
// this handler only moves disk files and inserts new page rows. detail is self-contained with zero
// DB reads (except InsertIfAbsent)—service and worker do not import each other, so the payload
// struct lives separately on each side with fields that must stay in sync (see
// service.reorderPagesDetail).
//
// Idempotency model (the key to rerun safety: once done, the "old path" may hold *someone else's*
// file and must no longer be moved as ours):
//   - staging directory absent → the previous round ran through phase 6 (all in place), so the
//     whole file phase is skipped;
//   - directory present, no marker → phase 2 incomplete (phases 3/4 follow in order), so the old
//     path must belong to this page;
//   - directory present, marker present → phase 2 complete, only stage/final names are trusted and
//     the old path is never touched.
//
// Phases (the marker is written after the phase 2 loop ends):
//  1. Delete deleted-page files (absent counts as success)—must precede all renames, since a
//     deleted page's old name may become the final name of a surviving/new page
//  2. Surviving existing page old → task_dir/stage_{id}.{ext} (frees the final name space)
//     2.5 Write marker (phase 2 complete; only after this may final names be touched)
//  3. New page task_dir/new_N.webp → final name
//  4. stage_{id} → final name
//  5. InsertIfAbsent to add new page rows (final index = final_order position+1, so row and file
//     share a position)
//  6. Clean up task_dir
//
// Error classification: payload shape/path out of bounds → nonRetryable (a retry only repeats the
// error); file IO failure → retryable. A phase 6 cleanup failure is non-retryable: phases 1-5 have
// completed, the directory may be half-cleaned (marker gone), and a rerun would move "someone
// else's files" as old ones; better to leave a junk directory than risk a wrong order. A retryable
// failure keeps task_dir (the stage files are the resume scene).
func handleReorderPages(ctx context.Context, deps Deps, task *domain.Task) error {
	p, err := parseReorderPagesDetail(deps, task.Detail)
	if err != nil {
		return err // already wrapped as nonRetryable
	}
	d := p.detail
	deps.Log.Info("start reordering exam pages",
		"task_id", task.ID, "exam_id", d.ExamID,
		"delete", len(d.DeletePageIDs), "new", len(d.TempFiles), "total", len(d.FinalOrder))

	taskAbs := filepath.Join(deps.DataDir, filepath.FromSlash(d.TaskDir))
	if _, err := os.Stat(taskAbs); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("check staging directory %s: %w", d.TaskDir, err)
		}
		// Directory absent = the previous round's phase 6 cleared the scene, so all files are
		// in place and only row inserts remain.
	} else {
		markerPresent := fileExists(filepath.Join(taskAbs, phase2Marker))

		// Phase 1: delete deleted-page files. Doing this later would make phase 3/4's
		// "final name already exists" check mistake someone else's content for our own.
		for _, id := range d.DeletePageIDs {
			rel := d.PageFiles[id]
			if err := os.Remove(filepath.Join(deps.DataDir, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete page file %s: %w", rel, err)
			}
		}

		// Phase 2: surviving pages → staging names.
		for _, item := range p.survivors {
			if item.absOld == item.absFinal {
				// In-place pages are never moved, so a missing file is genuinely lost (no crash
				// path touches it).
				if !fileExists(item.absFinal) {
					return fmt.Errorf("page file missing (in-place page): %s", item.relOld)
				}
				continue
			}
			if fileExists(item.absStage) {
				continue // already staged last round, waiting for phase 4
			}
			if markerPresent {
				// Phase 2 is already complete: the old path may now hold someone else's file,
				// so only stage/final names are trusted.
				if fileExists(item.absFinal) {
					continue // already placed in a previous phase 4
				}
				return fmt.Errorf("page file missing (neither stage nor final exists): %s", item.relOld)
			}
			if fileExists(item.absOld) {
				// Phase 2 incomplete: nothing has claimed the final name yet (phases 3/4 run
				// only after the marker), so the old path must be this page's file.
				if err := os.Rename(item.absOld, item.absStage); err != nil {
					return fmt.Errorf("stage page file %s: %w", item.absOld, err)
				}
				continue
			}
			// marker absent + old path empty + never staged: impossible outside the crash
			// window (had the last round run the phase 2 loop, this page would already be
			// staged) → the file is genuinely lost.
			return fmt.Errorf("page file missing (unstaged and empty old path): %s", item.relOld)
		}

		// Phase 2.5: marker. Only after it is written may phases 3/4 touch final names—reruns use
		// it to decide the old path is no longer trustworthy. A write failure is retryable (the
		// marker is still absent on rerun, so the logic matches this round).
		if err := os.WriteFile(filepath.Join(taskAbs, phase2Marker), []byte("1"), 0o640); err != nil {
			return fmt.Errorf("write phase marker: %w", err)
		}

		// Phase 3: new page staging file → final name (surviving pages' old files are all freed
		// by now, so "final name already exists" can only mean this page was placed last round).
		for _, nf := range p.newFiles {
			if fileExists(nf.absFinal) {
				continue // already in place last round
			}
			if err := os.Rename(nf.absTemp, nf.absFinal); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("new page staging file missing: %s", nf.relTemp)
				}
				return fmt.Errorf("land new page %s: %w", nf.relTemp, err)
			}
		}

		// Phase 4: staging → final name.
		for _, item := range p.survivors {
			if item.absOld == item.absFinal {
				continue
			}
			if fileExists(item.absFinal) {
				continue // already placed this round or a previous one
			}
			if err := os.Rename(item.absStage, item.absFinal); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("page staging file missing (neither stage nor final exists): %s", item.relOld)
				}
				return fmt.Errorf("land page file %s: %w", item.absStage, err)
			}
		}
	}

	// Phase 5: insert new page rows. Rows appear only after the files are in place, so the detail
	// page image can never 404.
	if len(d.TempFiles) > 0 {
		if deps.Pages == nil {
			return nonRetryable(fmt.Errorf("pages dependency not injected, cannot insert new page rows"))
		}
		newPages := make([]*domain.Page, 0, len(p.newFiles))
		for i, nf := range p.newFiles {
			newPages = append(newPages, &domain.Page{
				ExamID:    d.ExamID,
				PageIndex: nf.newIndex,
				FilePath:  nf.absFinal,
				SHA256:    d.TempFiles[i].SHA256,
			})
		}
		if _, err := deps.Pages.InsertIfAbsent(ctx, newPages); err != nil {
			return fmt.Errorf("insert new page rows: %w", err)
		}
	}

	// Phase 6: clean up the staging directory (stage/new/marker all live inside). A failure is
	// non-retryable—see the header: phases 1-5 are done, so a rerun's risk outweighs leaving junk.
	if err := os.RemoveAll(taskAbs); err != nil {
		return nonRetryable(fmt.Errorf("clean staging directory (files already in place; may be removed manually at %s): %w", d.TaskDir, err))
	}
	return nil
}

// phase2Marker marks "phase 2 complete" in the staging directory: while it is present, a rerun
// never moves old paths again (later phases may have taken the old path for another page's file).
const phase2Marker = "phase2.done"

// reorderPagesDetail is the kind=5 detail payload (the reader side). Its writer is
// service.reorderPagesDetail—the packages do not import each other, so changes must be synced on
// both sides.
type reorderPagesDetail struct {
	ExamID        int64             `json:"exam_id"`
	TaskDir       string            `json:"task_dir"`
	TempFiles     []reorderTempFile `json:"temp_files"`
	FinalOrder    []string          `json:"final_order"`
	PageFiles     map[int64]string  `json:"page_files"`
	DeletePageIDs []int64           `json:"delete_page_ids"`
}

type reorderTempFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Move plan for one surviving existing page (phases 2/4).
type reorderPlanItem struct {
	relOld   string // relative path before rename (for error messages)
	absOld   string
	absStage string
	absFinal string
}

// Placement plan for one new page (phases 3/5).
type reorderNewFile struct {
	relTemp  string
	absTemp  string
	absFinal string
	newIndex int
}

// Validated execution plan.
type reorderPlan struct {
	detail    *reorderPagesDetail
	survivors []reorderPlanItem
	newFiles  []reorderNewFile
}

// parseReorderPagesDetail parses and validates detail, returning an executable plan.
//
// The rules mirror the service side (completeness: final_order is the full permutation of
// surviving ∪ new pages), but the purpose differs: here they defend against payload corruption and
// path escapes—an untrusted payload never touches the filesystem and is always nonRetryable.
func parseReorderPagesDetail(deps Deps, raw string) (*reorderPlan, error) {
	bad := func(format string, args ...any) (*reorderPlan, error) {
		return nil, nonRetryable(fmt.Errorf("invalid reorder detail: %s", fmt.Sprintf(format, args...)))
	}
	var d reorderPagesDetail
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, nonRetryable(fmt.Errorf("parse reorder detail: %w", err))
	}
	if d.ExamID <= 0 {
		return bad("exam_id=%d", d.ExamID)
	}
	if len(d.FinalOrder) == 0 {
		return bad("empty final_order")
	}
	if len(d.PageFiles) == 0 && len(d.TempFiles) == 0 {
		return bad("neither existing pages nor new pages")
	}
	if d.TaskDir == "" {
		// Swapping pages needs a stage dir, which service always creates before enqueue.
		return bad("empty task_dir")
	}

	// Path sanitization: every relative path is confined under a whitelisted prefix and re-checked
	// after Clean to prevent .. escapes.
	examPrefix := fmt.Sprintf("assets/exams/%d/", d.ExamID)
	pageFiles := make(map[int64]string, len(d.PageFiles))
	for id, rel := range d.PageFiles {
		clean, err := cleanRelUnder(rel, examPrefix)
		if err != nil {
			return nil, err
		}
		pageFiles[id] = clean
	}
	d.PageFiles = pageFiles

	taskDirClean, err := cleanRelUnder(d.TaskDir, storage.TempTaskPrefix)
	if err != nil {
		return nil, err
	}
	d.TaskDir = strings.TrimSuffix(taskDirClean, "/")

	// Structural validation + position index.
	deleted := make(map[int64]bool, len(d.DeletePageIDs))
	for _, id := range d.DeletePageIDs {
		if deleted[id] {
			return bad("duplicate delete id: %d", id)
		}
		if _, ok := d.PageFiles[id]; !ok {
			return bad("deleted page %d not in page_files", id)
		}
		deleted[id] = true
	}

	posByID := make(map[int64]int, len(d.FinalOrder)) // existing page id → position (1-based)
	posByNew := make(map[int]int, len(d.TempFiles))   // new page N → position (1-based)
	for pos, entry := range d.FinalOrder {
		idx := pos + 1
		if n, ok := parseNewEntry(entry); ok {
			if n < 0 || n >= len(d.TempFiles) {
				return bad("new placeholder %q out of range (new pages: %d)", entry, len(d.TempFiles))
			}
			if _, dup := posByNew[n]; dup {
				return bad("duplicate new placeholder %q", entry)
			}
			posByNew[n] = idx
			continue
		}
		id, err := strconv.ParseInt(entry, 10, 64)
		if err != nil || id <= 0 {
			return bad("unparseable final_order item %q", entry)
		}
		if _, ok := d.PageFiles[id]; !ok {
			return bad("page %d not in page_files", id)
		}
		if deleted[id] {
			return bad("page %d both deleted and reordered", id)
		}
		if _, dup := posByID[id]; dup {
			return bad("page %d duplicated in final_order", id)
		}
		posByID[id] = idx
	}
	// Completeness: every surviving page is covered and new-page placeholders match the file count
	// (a missing entry means a file nobody claims and a row nobody manages; silent misalignment is
	// worse than failure).
	for id := range d.PageFiles {
		if !deleted[id] {
			if _, ok := posByID[id]; !ok {
				return bad("surviving page %d missing from final_order", id)
			}
		}
	}
	if len(posByNew) != len(d.TempFiles) {
		return bad("new-page placeholders %d mismatch files %d", len(posByNew), len(d.TempFiles))
	}

	plan := &reorderPlan{detail: &d}
	taskAbs := filepath.Join(deps.DataDir, filepath.FromSlash(d.TaskDir))
	for id, pos := range posByID {
		relOld := d.PageFiles[id]
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(relOld), "."))
		plan.survivors = append(plan.survivors, reorderPlanItem{
			relOld:   relOld,
			absOld:   filepath.Join(deps.DataDir, filepath.FromSlash(relOld)),
			absStage: filepath.Join(taskAbs, fmt.Sprintf("stage_%d.%s", id, ext)),
			// The final-name formula must match service-side storage.PagePath(pageExt) exactly:
			// the DB file_path is computed by it, and a mismatch leaves the row pointing at a
			// file that cannot be moved.
			absFinal: storage.PagePath(deps.DataDir, d.ExamID, pos, ext),
		})
	}
	for n, tf := range d.TempFiles {
		idx, ok := posByNew[n]
		if !ok {
			return bad("new page %d has no final_order placeholder", n)
		}
		relTemp, err := cleanRelUnder(tf.Path, d.TaskDir+"/")
		if err != nil {
			return nil, err
		}
		plan.newFiles = append(plan.newFiles, reorderNewFile{
			relTemp:  relTemp,
			absTemp:  filepath.Join(deps.DataDir, filepath.FromSlash(relTemp)),
			absFinal: storage.PagePath(deps.DataDir, d.ExamID, idx, "webp"),
			newIndex: idx,
		})
	}
	return plan, nil
}

// cleanRelUnder sanitizes a relative path and confines it under prefix (which must end with /).
// Out-of-bounds or absolute paths and prefix mismatches are all nonRetryable—the path comes from
// the DB, and any anomaly means an untrusted payload.
func cleanRelUnder(rel, prefix string) (string, error) {
	reject := func(reason string) (string, error) {
		return "", nonRetryable(fmt.Errorf("reorder detail path escape (%s): prefix=%s rel=%s", reason, prefix, rel))
	}
	if rel == "" || strings.Contains(rel, `\`) {
		return reject("empty or contains backslash")
	}
	if strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {
		return reject("absolute path")
	}
	clean := path.Clean(rel)
	if !strings.HasPrefix(clean, prefix) {
		return reject("not under whitelisted prefix after cleaning")
	}
	return clean, nil
}

// parseNewEntry returns N for the "new_N" form; anything else (including "new_x") returns false.
func parseNewEntry(entry string) (int, bool) {
	if !strings.HasPrefix(entry, "new_") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(entry, "new_"))
	if err != nil {
		return 0, false
	}
	return n, true
}

// fileExists reports whether a regular file exists (a directory of the same name does not count).
func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
