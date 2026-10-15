package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"paperly/internal/domain"
)

// Explicit column list; SELECT * is forbidden (AGENTS.md SQLite rules).
const subjectColumns = `id, code, name, sort_order, source, mask, mandatory, created_at, updated_at`

type SubjectRepoSQLite struct {
	baseRepo
}

var _ domain.SubjectRepository = (*SubjectRepoSQLite)(nil)

func NewSubjectRepoSQLite(db *DB) *SubjectRepoSQLite {
	return &SubjectRepoSQLite{baseRepo: baseRepo{db: db}}
}

// WithTx returns a shallow copy bound to tx; its statements then run inside that transaction.
func (r *SubjectRepoSQLite) WithTx(tx any) domain.SubjectRepository {
	return &SubjectRepoSQLite{baseRepo: r.mustBindTx(tx)}
}

// Idempotent import (INSERT OR IGNORE on unique code), returns insertions. Uses the write pool.
func (r *SubjectRepoSQLite) ImportSeed(ctx context.Context, rows []*domain.Subject) (int64, error) {
	const q = `INSERT OR IGNORE INTO subjects (code, name, sort_order, source, mask, mandatory) VALUES (?, ?, ?, ?, ?, ?)`
	var inserted int64
	for _, s := range rows {
		source := s.Source
		if source == 0 {
			source = domain.SubjectSourceSystem
		}
		mask := s.GradeMask
		if mask == 0 {
			mask = domain.MaskAll
		}
		res, err := r.writeExt().ExecContext(ctx, q, s.Code, s.Name, s.SortOrder, source, mask, s.Mandatory)
		if err != nil {
			return inserted, fmt.Errorf("import subject %s: %w", s.Code, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return inserted, fmt.Errorf("subject rows affected %s: %w", s.Code, err)
		}
		inserted += n
	}
	return inserted, nil
}

func (r *SubjectRepoSQLite) List(ctx context.Context) ([]*domain.Subject, error) {
	var out []*domain.Subject
	if err := r.readExt().SelectContext(ctx, &out,
		`SELECT `+subjectColumns+` FROM subjects ORDER BY sort_order ASC, id ASC`); err != nil {
		return nil, fmt.Errorf("list subjects: %w", err)
	}
	if out == nil {
		out = make([]*domain.Subject, 0)
	}
	return out, nil
}

// Uses the read pool.
func (r *SubjectRepoSQLite) ListByStageBit(ctx context.Context, bit int) ([]*domain.Subject, error) {
	var out []*domain.Subject
	if err := r.readExt().SelectContext(ctx, &out,
		`SELECT `+subjectColumns+` FROM subjects WHERE (mask & ?) != 0 ORDER BY sort_order ASC, id ASC`, bit); err != nil {
		return nil, fmt.Errorf("list subjects by stage bit: %w", err)
	}
	if out == nil {
		out = make([]*domain.Subject, 0)
	}
	return out, nil
}

// A zero bit drops that dimension's clause (matches every row), so ListFiltered(ctx, 0, 0)
// equals List. Uses the read pool; all values are parameterized.
func (r *SubjectRepoSQLite) ListFiltered(ctx context.Context, stageBit, trackBit int) ([]*domain.Subject, error) {
	q := `SELECT ` + subjectColumns + ` FROM subjects WHERE 1=1`
	var args []any
	if stageBit != 0 {
		q += ` AND (mask & ?) != 0`
		args = append(args, stageBit)
	}
	if trackBit != 0 {
		q += ` AND (mask & ?) != 0`
		args = append(args, trackBit)
	}
	q += ` ORDER BY sort_order ASC, id ASC`

	var out []*domain.Subject
	if err := r.readExt().SelectContext(ctx, &out, q, args...); err != nil {
		return nil, fmt.Errorf("list subjects filtered: %w", err)
	}
	if out == nil {
		out = make([]*domain.Subject, 0)
	}
	return out, nil
}

// Empty id list returns an empty slice (no query). Uses the read pool; the IN list is
// expanded into placeholders (never string-interpolated).
func (r *SubjectRepoSQLite) ListByIDs(ctx context.Context, ids []int64) ([]*domain.Subject, error) {
	out := make([]*domain.Subject, 0, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT ` + subjectColumns + ` FROM subjects WHERE id IN (` +
		strings.Join(placeholders, ",") + `) ORDER BY sort_order ASC, id ASC`
	if err := r.readExt().SelectContext(ctx, &out, q, args...); err != nil {
		return nil, fmt.Errorf("list subjects by ids: %w", err)
	}
	return out, nil
}

// Returns domain.ErrNotFound if absent. Uses the read pool.
func (r *SubjectRepoSQLite) GetByID(ctx context.Context, id int64) (*domain.Subject, error) {
	var s domain.Subject
	err := r.readExt().GetContext(ctx, &s,
		`SELECT `+subjectColumns+` FROM subjects WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get subject by id: %w", err)
	}
	return &s, nil
}

// Parent-created: code comes from service (SubjectService.Create); source is fixed to user.
// System preset subjects can only come from seed import. Uses the write pool.
func (r *SubjectRepoSQLite) Insert(ctx context.Context, s *domain.Subject) (int64, error) {
	mask := s.GradeMask
	if mask == 0 {
		mask = domain.MaskAll
	}
	res, err := r.writeExt().ExecContext(ctx,
		`INSERT INTO subjects (code, name, sort_order, source, mask, mandatory) VALUES (?, ?, ?, ?, ?, ?)`,
		s.Code, s.Name, s.SortOrder, domain.SubjectSourceUser, mask, s.Mandatory)
	if err != nil {
		return 0, fmt.Errorf("insert subject %s: %w", s.Code, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("subject last insert id: %w", err)
	}
	s.Source = domain.SubjectSourceUser
	s.GradeMask = mask
	return id, nil
}

func (r *SubjectRepoSQLite) UpdateName(ctx context.Context, id int64, name string) (int64, error) {
	res, err := r.writeExt().ExecContext(ctx,
		`UPDATE subjects SET name = ?, updated_at = datetime('now') WHERE id = ?`, name, id)
	if err != nil {
		return 0, fmt.Errorf("update subject name: %w", err)
	}
	return res.RowsAffected()
}

// mask: low 3 stage bits + high 2 track bits (0~31). Returns affected row count. Uses the write pool.
func (r *SubjectRepoSQLite) UpdateMask(ctx context.Context, id int64, mask int) (int64, error) {
	res, err := r.writeExt().ExecContext(ctx,
		`UPDATE subjects SET mask = ?, updated_at = datetime('now') WHERE id = ?`, mask, id)
	if err != nil {
		return 0, fmt.Errorf("update subject mask: %w", err)
	}
	return res.RowsAffected()
}

func (r *SubjectRepoSQLite) CountExams(ctx context.Context, id int64) (int64, error) {
	var n int64
	if err := r.readExt().GetContext(ctx, &n,
		`SELECT COUNT(*) FROM exams WHERE subject_id = ?`, id); err != nil {
		return 0, fmt.Errorf("count exams by subject: %w", err)
	}
	return n, nil
}

func (r *SubjectRepoSQLite) CountKnowledgePoints(ctx context.Context, id int64) (int64, error) {
	var n int64
	if err := r.readExt().GetContext(ctx, &n,
		`SELECT COUNT(*) FROM knowledge_points WHERE subject_id = ?`, id); err != nil {
		return 0, fmt.Errorf("count knowledge points by subject: %w", err)
	}
	return n, nil
}

func (r *SubjectRepoSQLite) Delete(ctx context.Context, id int64) (int64, error) {
	res, err := r.writeExt().ExecContext(ctx, `DELETE FROM subjects WHERE id = ?`, id)
	if err != nil {
		return 0, fmt.Errorf("delete subject: %w", err)
	}
	return res.RowsAffected()
}
