package domain

import (
	"context"
	"time"
)

// Page is an exam page (pages table).
type Page struct {
	ID        int64     `db:"id"`
	ExamID    int64     `db:"exam_id"`
	PageIndex int       `db:"page_index"`
	FilePath  string    `db:"file_path"`
	SHA256    string    `db:"sha256"`
	CreatedAt time.Time `db:"created_at"`
}

// PageReorder moves a page to NewIndex and changes file_path to the matching final name (the row is
// updated first; the worker does the file rename).
type PageReorder struct {
	ID          int64
	NewIndex    int
	NewFilePath string
}

// PageRepository is the exam-page data-access contract.
type PageRepository interface {
	Insert(ctx context.Context, p *Page) (int64, error)
	// GetByID fetches one page by primary key (page image preview/download).
	GetByID(ctx context.Context, id int64) (*Page, error)
	ListByExam(ctx context.Context, examID int64) ([]*Page, error)
	// RebuildPages tidies one exam's pages in a **single transaction**: delete the ids in deletes,
	// reorder the rest per reorders, and enqueue enqueue (tasks.kind=5). All three must succeed or
	// roll back together — splitting them could leave "DB order changed but no task enqueued, so the
	// disk files are never touched". The transient UNIQUE(exam_id, page_index) conflict is avoided by
	// offsetting everything first, then writing back. enqueue == nil means reorder only. Returns the
	// enqueued task id (0 when nil).
	RebuildPages(ctx context.Context, examID int64, deletes []int64, reorders []PageReorder, enqueue *Task) (int64, error)
	// InsertIfAbsent batch-inserts pages, skipping a row whose (exam_id, page_index) already exists —
	// idempotency for reorder-task retries (a row inserted but whose file rename failed is not
	// inserted twice). Returns the number actually inserted.
	InsertIfAbsent(ctx context.Context, pages []*Page) (int, error)
	// WithTx returns a new instance bound to tx.
	WithTx(tx Tx) PageRepository
}
