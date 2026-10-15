package domain

import (
	"context"
	"time"
)

// KnowledgePoint corresponds to the knowledge_points table.
//
// No self-referencing parent_id: the design's "tree" is one level grouped by subject (📁 Algebra ->
// 📄 fractional division). The group name lives in subjects; a point carries only subject_id + name.
// A real three-level tree would need a schema change, not forced here.
type KnowledgePoint struct {
	ID        int64     `db:"id"`
	SubjectID int64     `db:"subject_id"`
	Name      string    `db:"name"`
	SortOrder int       `db:"sort_order"`
	Source    int       `db:"source"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// KnowledgePointRepository is the knowledge_points data-access contract.
type KnowledgePointRepository interface {
	// ListBySubject returns the knowledge points under a subject, ordered by sort_order ASC, id ASC.
	ListBySubject(ctx context.Context, subjectID int64) ([]*KnowledgePoint, error)
	// GetByID queries by id; returns ErrNotFound when missing.
	GetByID(ctx context.Context, id int64) (*KnowledgePoint, error)
	// Insert adds a knowledge point (source=SubjectSourceUser), returning the new id.
	Insert(ctx context.Context, kp *KnowledgePoint) (int64, error)
	// UpdateName renames, returning the affected row count.
	UpdateName(ctx context.Context, id int64, name string) (int64, error)
	// Delete removes a knowledge point, returning the affected row count.
	Delete(ctx context.Context, id int64) (int64, error)
	// WithTx returns a new instance bound to the given transaction.
	WithTx(tx Tx) KnowledgePointRepository
}
