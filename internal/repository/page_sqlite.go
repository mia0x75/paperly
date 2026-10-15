package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"paperly/internal/domain"
)

// pageColumns lists the query fields explicitly; SELECT * is forbidden (AGENTS.md
// SQLite rules).
const pageColumns = `id, exam_id, page_index, file_path, sha256, created_at`

// PageRepoSQLite implements domain.PageRepository on SQLite.
type PageRepoSQLite struct {
	baseRepo
	// notifier is optional. When set, RebuildPages' same-transaction task enqueue wakes
	// the Worker after commit, since this path bypasses TaskRepoSQLite.Insert.
	notifier Notifier
}

var _ domain.PageRepository = (*PageRepoSQLite)(nil)

func NewPageRepoSQLite(db *DB) *PageRepoSQLite {
	return &PageRepoSQLite{baseRepo: baseRepo{db: db}}
}

// WithNotifier attaches the wake notifier and returns the receiver for chaining. Kept
// off the constructor so existing call sites keep compiling.
func (r *PageRepoSQLite) WithNotifier(n Notifier) *PageRepoSQLite {
	r.notifier = n
	return r
}

// WithTx returns a shallow copy bound to tx; its statements then run inside that
// transaction.
func (r *PageRepoSQLite) WithTx(tx any) domain.PageRepository {
	return &PageRepoSQLite{baseRepo: r.mustBindTx(tx), notifier: r.notifier}
}

// Insert writes one page via the write pool.
func (r *PageRepoSQLite) Insert(ctx context.Context, p *domain.Page) (int64, error) {
	const q = `INSERT INTO pages (exam_id, page_index, file_path, sha256)
		VALUES (:exam_id, :page_index, :file_path, :sha256)`
	res, err := r.writeExt().NamedExecContext(ctx, q, p)
	if err != nil {
		return 0, fmt.Errorf("insert page: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("page last insert id: %w", err)
	}
	return id, nil
}

// GetByID returns domain.ErrNotFound if the page is absent; uses the read pool.
func (r *PageRepoSQLite) GetByID(ctx context.Context, id int64) (*domain.Page, error) {
	var p domain.Page
	if err := r.readExt().GetContext(ctx, &p,
		`SELECT `+pageColumns+` FROM pages WHERE id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get page by id: %w", err)
	}
	return &p, nil
}

// ListByExam returns an exam's pages in page_index order; uses the read pool.
func (r *PageRepoSQLite) ListByExam(ctx context.Context, examID int64) ([]*domain.Page, error) {
	var out []*domain.Page
	if err := r.readExt().SelectContext(ctx, &out,
		`SELECT `+pageColumns+` FROM pages WHERE exam_id = ? ORDER BY page_index ASC`, examID); err != nil {
		return nil, fmt.Errorf("list pages by exam: %w", err)
	}
	if out == nil {
		out = make([]*domain.Page, 0)
	}
	return out, nil
}

// RebuildPages does "delete pages + renumber + enqueue reorder task" in one transaction;
// domain.PageRepository documents the contract.
//
// Steps: delete rows → shift page_index by +1000 (makes room and avoids the transient
// conflict of a UNIQUE(exam_id,page_index) swap) → write back the final page_index/
// file_path per reorder. Finally enqueue tasks(kind=5).
func (r *PageRepoSQLite) RebuildPages(ctx context.Context, examID int64, deletes []int64, reorders []domain.PageReorder, enqueue *domain.Task) (int64, error) {
	tx, err := r.db.Write().BeginTxx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin rebuild pages: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Delete. The IN placeholder count comes from len(deletes); the count isn't user
	// input, and every value is bound.
	if len(deletes) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(deletes)), ",")
		args := make([]any, 0, len(deletes)+1)
		args = append(args, examID)
		for _, id := range deletes {
			args = append(args, id)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM pages WHERE exam_id = ? AND id IN (`+placeholders+`)`, args...); err != nil {
			return 0, fmt.Errorf("delete pages: %w", err)
		}
	}

	// 2. Shift everything so all original indexes are vacated first.
	if _, err := tx.ExecContext(ctx,
		`UPDATE pages SET page_index = page_index + 1000 WHERE exam_id = ?`, examID); err != nil {
		return 0, fmt.Errorf("offset page index: %w", err)
	}

	// 3. Write back final values one at a time. Every update is scoped to this exam, so an
	// externally supplied id can't touch another exam's rows.
	for _, ro := range reorders {
		res, err := tx.ExecContext(ctx,
			`UPDATE pages SET page_index = ?, file_path = ? WHERE id = ? AND exam_id = ?`,
			ro.NewIndex, ro.NewFilePath, ro.ID, examID)
		if err != nil {
			return 0, fmt.Errorf("reorder page %d: %w", ro.ID, err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return 0, fmt.Errorf("reorder page %d: %w", ro.ID, domain.ErrNotFound)
		}
	}

	// 4. Enqueue the reorder task in the same transaction, so it commits or rolls back with
	// the page order.
	var taskID int64
	if enqueue != nil {
		res, err := tx.NamedExecContext(ctx, taskInsertSQL, enqueue)
		if err != nil {
			return 0, fmt.Errorf("enqueue reorder task: %w", err)
		}
		if taskID, err = res.LastInsertId(); err != nil {
			return 0, fmt.Errorf("reorder task last insert id: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit rebuild pages: %w", err)
	}
	// The task is only visible to the Worker after commit; wake it now instead of waiting
	// for the fallback poll.
	if enqueue != nil && r.notifier != nil {
		r.notifier.Notify()
	}
	return taskID, nil
}

// InsertIfAbsent inserts new pages and skips rows that conflict on (exam_id, page_index),
// making retries idempotent. Uses the write pool: INSERT ... ON CONFLICT DO NOTHING, with
// the inserted count taken from RowsAffected.
func (r *PageRepoSQLite) InsertIfAbsent(ctx context.Context, pages []*domain.Page) (int, error) {
	const q = `INSERT INTO pages (exam_id, page_index, file_path, sha256)
		VALUES (:exam_id, :page_index, :file_path, :sha256)
		ON CONFLICT (exam_id, page_index) DO NOTHING`
	inserted := 0
	for _, p := range pages {
		res, err := r.writeExt().NamedExecContext(ctx, q, p)
		if err != nil {
			return inserted, fmt.Errorf("insert page if absent: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	return inserted, nil
}
