package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"paperly/internal/domain"
)

// knowledgePointColumns lists the query fields explicitly; SELECT * is forbidden.
const knowledgePointColumns = `id, subject_id, name, sort_order, source, created_at, updated_at`

// KnowledgePointRepoSQLite implements domain.KnowledgePointRepository on SQLite.
type KnowledgePointRepoSQLite struct {
	baseRepo
}

var _ domain.KnowledgePointRepository = (*KnowledgePointRepoSQLite)(nil)

func NewKnowledgePointRepoSQLite(db *DB) *KnowledgePointRepoSQLite {
	return &KnowledgePointRepoSQLite{baseRepo: baseRepo{db: db}}
}

// WithTx returns a shallow copy bound to tx; its statements then run inside that
// transaction.
func (r *KnowledgePointRepoSQLite) WithTx(tx any) domain.KnowledgePointRepository {
	return &KnowledgePointRepoSQLite{baseRepo: r.mustBindTx(tx)}
}

// ListBySubject returns a subject's knowledge points; uses the read pool.
func (r *KnowledgePointRepoSQLite) ListBySubject(ctx context.Context, subjectID int64) ([]*domain.KnowledgePoint, error) {
	var out []*domain.KnowledgePoint
	if err := r.readExt().SelectContext(ctx, &out, `
SELECT `+knowledgePointColumns+`
FROM knowledge_points
WHERE subject_id = ?
ORDER BY sort_order ASC, id ASC`, subjectID); err != nil {
		return nil, fmt.Errorf("list knowledge points of subject %d: %w", subjectID, err)
	}
	if out == nil {
		out = make([]*domain.KnowledgePoint, 0)
	}
	return out, nil
}

// GetByID returns domain.ErrNotFound if the knowledge point is absent; uses the read pool.
func (r *KnowledgePointRepoSQLite) GetByID(ctx context.Context, id int64) (*domain.KnowledgePoint, error) {
	var kp domain.KnowledgePoint
	err := r.readExt().GetContext(ctx, &kp,
		`SELECT `+knowledgePointColumns+` FROM knowledge_points WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get knowledge point by id: %w", err)
	}
	return &kp, nil
}

// Insert adds a parent-created knowledge point via the write pool.
//
// sort_order is "current max + 1" instead of caller-supplied: inserts under one subject
// are sparse, and letting the frontend pick the order would inevitably collide — two
// pages adding at once both pick 3.
func (r *KnowledgePointRepoSQLite) Insert(ctx context.Context, kp *domain.KnowledgePoint) (int64, error) {
	var nextOrder int
	if err := r.readExt().GetContext(ctx, &nextOrder, `
SELECT COALESCE(MAX(sort_order), 0) + 1 FROM knowledge_points WHERE subject_id = ?`,
		kp.SubjectID); err != nil {
		return 0, fmt.Errorf("next knowledge point sort_order: %w", err)
	}
	if kp.SortOrder <= 0 {
		kp.SortOrder = nextOrder
	}

	res, err := r.writeExt().ExecContext(ctx, `
INSERT INTO knowledge_points (subject_id, name, sort_order, source)
VALUES (?, ?, ?, ?)`,
		kp.SubjectID, kp.Name, kp.SortOrder, domain.SubjectSourceUser)
	if err != nil {
		return 0, fmt.Errorf("insert knowledge point: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("knowledge point last insert id: %w", err)
	}
	kp.Source = domain.SubjectSourceUser
	return id, nil
}

// UpdateName renames a knowledge point via the write pool.
func (r *KnowledgePointRepoSQLite) UpdateName(ctx context.Context, id int64, name string) (int64, error) {
	res, err := r.writeExt().ExecContext(ctx,
		`UPDATE knowledge_points SET name = ?, updated_at = datetime('now') WHERE id = ?`, name, id)
	if err != nil {
		return 0, fmt.Errorf("update knowledge point name: %w", err)
	}
	return res.RowsAffected()
}

// Delete removes a knowledge point via the write pool.
func (r *KnowledgePointRepoSQLite) Delete(ctx context.Context, id int64) (int64, error) {
	res, err := r.writeExt().ExecContext(ctx, `DELETE FROM knowledge_points WHERE id = ?`, id)
	if err != nil {
		return 0, fmt.Errorf("delete knowledge point: %w", err)
	}
	return res.RowsAffected()
}
