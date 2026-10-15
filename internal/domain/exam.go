package domain

import (
	"context"
	"errors"
	"time"
)

// Exam status (stored as INT).
const (
	ExamStatusActive  = 1
	ExamStatusDeleted = 2
)

// uploaded_by values (stored as INT).
const (
	UploadedByParent = 1 // parent upload from the admin side
	UploadedByKid    = 2 // kid self-upload with an access_code
)

// exam_type values (stored as INT).
const (
	ExamTypeWeekly  = 1
	ExamTypeMonthly = 2
	ExamTypeMidterm = 3
	ExamTypeFinal   = 4
	ExamTypeMock    = 5
	ExamTypeSpecial = 6
	ExamTypeOther   = 99
)

// SummarySource records where an exam's summary text came from (summary_source is nullable:
// a nil summary has no source).
type SummarySource int

// SummarySourceManual is written by the parent editor; SummarySourceAI is reserved for a
// future automated producer (none in this release).
const (
	SummarySourceManual SummarySource = 1
	SummarySourceAI     SummarySource = 2
)

// Valid reports whether s is a known source. The write path rejects out-of-range values so a
// tampered request cannot persist an unknown provenance.
func (s SummarySource) Valid() bool {
	switch s {
	case SummarySourceManual, SummarySourceAI:
		return true
	}
	return false
}

var (
	ErrFileTooLarge        = errors.New("file too large")
	ErrUnsupportedFileType = errors.New("unsupported file type")
	// PDF upload is not supported; use images/screenshots.
	ErrPDFNotSupported = errors.New("pdf upload not supported")
)

// Exam is the domain model for the exams table.
type Exam struct {
	ID int64 `db:"id"`
	// Title (e.g. "2026-Grade9-Fall-Math-Sep-Monthly"). Parents may fill/edit it; the frontend
	// pre-fills per the genTitle rule. NULL = unnamed.
	Title         *string    `db:"title"`
	SubjectID     *int64     `db:"subject_id"`
	Grade         *int       `db:"grade"`
	ExamType      *int       `db:"exam_type"`
	ExamDate      *time.Time `db:"exam_date"`
	KidID         *int64     `db:"kid_id"`
	UploadedBy    int        `db:"uploaded_by"`
	Summary       *string    `db:"summary"`
	SummarySource *int       `db:"summary_source"`
	// PageCount is filled by a scalar subquery in the repository, not stored: it derives from the
	// pages table, and a second copy would drift (soft-deletes and re-uploads).
	PageCount int       `db:"page_count"`
	Status    int       `db:"status"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// ListExamFilter is the exam-list filter.
type ListExamFilter struct {
	SubjectID *int64
	Grade     *int
	ExamType  *int
	Status    int // 0 = all statuses (including deleted)
	KidID     *int64
	// DateFrom / DateTo are the closed interval over exam_date. It uses exam_date (not created_at)
	// because the home "history exams" filters by exam period, matching the user's mental model.
	// Both nil = no date filter; both set = a single day. The repository normalizes (fills the time
	// part, swaps reversed endpoints), so callers need not preprocess.
	DateFrom *time.Time
	DateTo   *time.Time

	// Cursor is a keyset cursor: return rows with id < Cursor, id DESC. id over offset because
	// LIMIT/OFFSET is positional — adding/deleting earlier rows shifts the window and skips or
	// duplicates entries, while a monotonic id stays stable across deletions.
	// 0 = from the newest. nextCursor == 0 means no more.
	Cursor int64
	// Limit is the page size; <=0 takes the default, >100 is capped at 100.
	Limit int
}

// ExamRepository is the exam data-access contract.
type ExamRepository interface {
	Insert(ctx context.Context, p *Exam) (int64, error)
	GetByID(ctx context.Context, id int64) (*Exam, error)
	// List returns one page (<= f.Limit rows, id DESC) and the next cursor (0 = no more).
	List(ctx context.Context, f ListExamFilter) (items []*Exam, nextCursor int64, err error)
	Update(ctx context.Context, p *Exam) error
	// UpdateSummary applies only when the stored value still equals expectedOld (NULL = empty),
	// reporting whether it wrote. Callers retry on false (concurrent append) instead of clobbering
	// a newer summary (lost update).
	UpdateSummary(ctx context.Context, id int64, expectedOld, next string) (bool, error)
	SoftDelete(ctx context.Context, id int64) error
	// Restore revives a soft-deleted exam. Matches only status=deleted rows, otherwise
	// affectedOrNotFound -> ErrNotFound (the service checks state first). Uses the write pool.
	Restore(ctx context.Context, id int64) error
	// WithTx returns a new instance bound to tx.
	WithTx(tx Tx) ExamRepository
}
