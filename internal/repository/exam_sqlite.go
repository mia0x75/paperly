package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"paperly/internal/domain"
)

// examColumns lists the query fields explicitly; SELECT * is forbidden (AGENTS.md
// SQLite rules). Order must match the db tags of domain.Exam.
const examColumns = `id, title, subject_id, grade, exam_type, exam_date, kid_id,
	uploaded_by, summary, summary_source, status, created_at, updated_at`

// examColumnsWithPageCount is the list-only set: examColumns plus a page_count
// correlated subquery. GetByID skips it, since the detail response returns a pages
// array separately.
const examColumnsWithPageCount = examColumns + `,
	(SELECT COUNT(*) FROM pages pg WHERE pg.exam_id = exams.id) AS page_count`

// dateLayout is the storage format for exams.exam_date.
const dateLayout = "2006-01-02"

// startOfDay returns UTC midnight of t; used to normalize date-range filters.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// endOfDay is the closed-interval upper bound, paired with startOfDay.
func endOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.UTC)
}

// dateOnly stores a *time.Time into a DATE column as "YYYY-MM-DD".
//
// The driver can't bind time.Time directly: modernc/sqlite writes Go's String() format
// ("2026-09-15 00:00:00 +0000 UTC"), which SQLite's date() can't parse because of the
// suffix, and string comparison is thrown off by the trailing content — the symptom is
// that the closed-interval upper-bound day gets dropped. Normalizing to dateLayout keeps
// the DDL DATE column, the codec, and range comparisons on one convention.
type dateOnly struct {
	t *time.Time
}

// Value implements driver.Valuer; a nil time is written as NULL.
//
// Formats in the value's own timezone (no .UTC() conversion): exam_date has calendar-day
// semantics — the literal year/month/day. Converting to UTC would turn a local 09-15
// early morning into 09-14, shifting the date by a day. Callers should pass a UTC
// midnight; this only takes the calendar components as given.
func (d dateOnly) Value() (driver.Value, error) {
	if d.t == nil {
		return nil, nil
	}
	return d.t.Format(dateLayout), nil
}

// examWrite carries named params for Insert/Update: same fields as domain.Exam except
// ExamDate is a dateOnly, so the domain layer stays untouched.
type examWrite struct {
	ID            int64    `db:"id"`
	Title         *string  `db:"title"`
	SubjectID     *int64   `db:"subject_id"`
	Grade         *int     `db:"grade"`
	ExamType      *int     `db:"exam_type"`
	ExamDate      dateOnly `db:"exam_date"`
	KidID         *int64   `db:"kid_id"`
	UploadedBy    int      `db:"uploaded_by"`
	Summary       *string  `db:"summary"`
	SummarySource *int     `db:"summary_source"`
	Status        int      `db:"status"`
}

func newExamWrite(p *domain.Exam) examWrite {
	return examWrite{
		ID:            p.ID,
		Title:         p.Title,
		SubjectID:     p.SubjectID,
		Grade:         p.Grade,
		ExamType:      p.ExamType,
		ExamDate:      dateOnly{t: p.ExamDate},
		KidID:         p.KidID,
		UploadedBy:    p.UploadedBy,
		Summary:       p.Summary,
		SummarySource: p.SummarySource,
		Status:        p.Status,
	}
}

// ExamRepoSQLite implements domain.ExamRepository on SQLite. Row scans go through sqlx
// (GetContext/SelectContext); SQL is hand-written (AGENTS.md rules 9/10).
type ExamRepoSQLite struct {
	baseRepo
}

var _ domain.ExamRepository = (*ExamRepoSQLite)(nil)

func NewExamRepoSQLite(db *DB) *ExamRepoSQLite {
	return &ExamRepoSQLite{baseRepo: baseRepo{db: db}}
}

// WithTx returns a shallow copy bound to tx; its statements then run inside that
// transaction.
func (r *ExamRepoSQLite) WithTx(tx any) domain.ExamRepository {
	return &ExamRepoSQLite{baseRepo: r.mustBindTx(tx)}
}

// Insert writes an exam via the write pool; NamedExec maps params through db tags.
func (r *ExamRepoSQLite) Insert(ctx context.Context, p *domain.Exam) (int64, error) {
	const q = `INSERT INTO exams
		(title, subject_id, grade, exam_type, exam_date, kid_id,
		 uploaded_by, summary, summary_source, status)
		VALUES (:title, :subject_id, :grade, :exam_type, :exam_date, :kid_id,
			:uploaded_by, :summary, :summary_source, :status)`
	res, err := r.writeExt().NamedExecContext(ctx, q, newExamWrite(p))
	if err != nil {
		return 0, fmt.Errorf("insert exam: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("exam last insert id: %w", err)
	}
	return id, nil
}

// GetByID returns domain.ErrNotFound if the exam is absent; uses the read pool.
// Soft-deleted rows are still returned — soft delete only filters List, so the detail
// stays queryable.
func (r *ExamRepoSQLite) GetByID(ctx context.Context, id int64) (*domain.Exam, error) {
	var p domain.Exam
	err := r.readExt().GetContext(ctx, &p,
		`SELECT `+examColumns+` FROM exams WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get exam by id: %w", err)
	}
	return &p, nil
}

// List returns a page of exams plus the next-page cursor, using the read pool.
//
// Keyset pagination (id < cursor, id DESC) rather than LIMIT/OFFSET: offset skips or
// duplicates rows when rows are deleted/inserted mid-paging. id is monotonic, so a
// deletion doesn't disturb the already-paged window.
func (r *ExamRepoSQLite) List(ctx context.Context, f domain.ListExamFilter) ([]*domain.Exam, int64, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	where := ` WHERE 1=1`
	var args []any
	if f.Status != 0 {
		where += ` AND status = ?`
		args = append(args, f.Status)
	}
	if f.SubjectID != nil {
		where += ` AND subject_id = ?`
		args = append(args, *f.SubjectID)
	}
	if f.Grade != nil {
		where += ` AND grade = ?`
		args = append(args, *f.Grade)
	}
	if f.ExamType != nil {
		where += ` AND exam_type = ?`
		args = append(args, *f.ExamType)
	}
	if f.KidID != nil {
		where += ` AND kid_id = ?`
		args = append(args, *f.KidID)
	}
	// Swap reversed endpoints (from > to). domain.ListExamFilter documents that
	// normalization is the repository's job. Without the swap, exam_date >= later AND
	// <= earlier is always false, so the user gets an empty list while believing the
	// range really has no exams — a silently wrong answer is worse than an error.
	dateFrom, dateTo := f.DateFrom, f.DateTo
	if dateFrom != nil && dateTo != nil && dateFrom.After(*dateTo) {
		dateFrom, dateTo = dateTo, dateFrom
	}
	// Bound values use dateLayout: the write side (dateOnly) already normalizes exam_date
	// to "YYYY-MM-DD", matching the DDL DATE and the codec.
	if dateFrom != nil {
		where += ` AND exam_date >= ?`
		args = append(args, startOfDay(*dateFrom).Format(dateLayout))
	}
	if dateTo != nil {
		where += ` AND exam_date <= ?`
		args = append(args, endOfDay(*dateTo).Format(dateLayout))
	}
	if f.Cursor > 0 {
		where += ` AND id < ?`
		args = append(args, f.Cursor)
	}

	// Fetch one extra row to detect a next page: cheaper than a second COUNT and
	// unaffected by concurrent inserts.
	// page_count comes from a correlated subquery, not a JOIN of pages: a JOIN expands one
	// exam into N rows, turning LIMIT into "trim by page" and corrupting pagination.
	q := `SELECT ` + examColumnsWithPageCount + ` FROM exams` + where +
		` ORDER BY id DESC LIMIT ?`
	args = append(args, limit+1)

	var rows []*domain.Exam
	if err := r.readExt().SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, 0, fmt.Errorf("list exams: %w", err)
	}

	var nextCursor int64
	if len(rows) > limit {
		rows = rows[:limit]
		nextCursor = rows[len(rows)-1].ID
	}
	if rows == nil {
		rows = make([]*domain.Exam, 0)
	}
	return rows, nextCursor, nil
}

// Update writes the exam's editable fields
// (subject_id/grade/exam_type/exam_date/kid_id/summary) through the write pool.
// uploaded_by is a done fact — editing shouldn't rewrite "who uploaded it" — so it's
// left alone.
func (r *ExamRepoSQLite) Update(ctx context.Context, p *domain.Exam) error {
	res, err := r.writeExt().NamedExecContext(ctx, `
UPDATE exams SET
	title = :title,
	subject_id = :subject_id, grade = :grade, exam_type = :exam_type,
	exam_date = :exam_date, kid_id = :kid_id, summary = :summary,
	updated_at = CURRENT_TIMESTAMP
WHERE id = :id`, newExamWrite(p))
	if err != nil {
		return fmt.Errorf("update exam: %w", err)
	}
	return affectedOrNotFound(res, "exam")
}

// UpdateSummary writes the summary only if it still equals expectedOld (optimistic
// concurrency; COALESCE treats NULL as empty). Returns false when the summary changed
// underneath the caller, signalling a retry.
func (r *ExamRepoSQLite) UpdateSummary(ctx context.Context, id int64, expectedOld, next string) (bool, error) {
	const q = `UPDATE exams SET summary = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND COALESCE(summary, '') = ?`
	res, err := r.writeExt().ExecContext(ctx, q, next, id, expectedOld)
	if err != nil {
		return false, fmt.Errorf("update exam summary: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("update exam summary rows: %w", err)
	}
	return n == 1, nil
}

// SoftDelete sets status to ExamStatusDeleted via the write pool.
func (r *ExamRepoSQLite) SoftDelete(ctx context.Context, id int64) error {
	const q = `UPDATE exams SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	res, err := r.writeExt().ExecContext(ctx, q, domain.ExamStatusDeleted, id)
	if err != nil {
		return fmt.Errorf("soft delete exam: %w", err)
	}
	return affectedOrNotFound(res, "exam")
}

// Restore flips a soft-deleted exam back to active via the write pool. It matches only
// status=deleted rows, so anything else falls through affectedOrNotFound → ErrNotFound
// (the service distinguishes states up front).
func (r *ExamRepoSQLite) Restore(ctx context.Context, id int64) error {
	const q = `UPDATE exams SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND status = ?`
	res, err := r.writeExt().ExecContext(ctx, q, domain.ExamStatusActive, id, domain.ExamStatusDeleted)
	if err != nil {
		return fmt.Errorf("restore exam: %w", err)
	}
	return affectedOrNotFound(res, "exam")
}
