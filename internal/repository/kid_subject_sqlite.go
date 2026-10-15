package repository

import (
	"context"
	"fmt"

	"paperly/internal/domain"
)

// KidSubjectRepoSQLite implements domain.KidSubjectRepository on SQLite.
type KidSubjectRepoSQLite struct {
	baseRepo
}

var _ domain.KidSubjectRepository = (*KidSubjectRepoSQLite)(nil)

func NewKidSubjectRepoSQLite(db *DB) *KidSubjectRepoSQLite {
	return &KidSubjectRepoSQLite{baseRepo: baseRepo{db: db}}
}

// WithTx returns a shallow copy bound to tx; its statements then run inside that
// transaction.
func (r *KidSubjectRepoSQLite) WithTx(tx any) domain.KidSubjectRepository {
	return &KidSubjectRepoSQLite{baseRepo: r.mustBindTx(tx)}
}

// ListSubjectIDs returns the kid's whitelisted subject ids (empty slice if none
// configured); uses the read pool.
func (r *KidSubjectRepoSQLite) ListSubjectIDs(ctx context.Context, kidID int64) ([]int64, error) {
	ids := make([]int64, 0)
	err := r.readExt().SelectContext(ctx, &ids,
		`SELECT subject_id FROM kid_subjects WHERE kid_id = ? ORDER BY subject_id ASC`, kidID)
	if err != nil {
		return nil, fmt.Errorf("list kid_subjects for kid %d: %w", kidID, err)
	}
	return ids, nil
}

// ReplaceAll sets the kid's whitelist to exactly subjectIDs (atomic delete-then-insert).
// An empty list clears the whitelist. If the repo is already bound to a transaction
// (WithTx), it reuses it so a kid + subjects write stays inside the outer transaction;
// otherwise it opens its own on the write pool's single connection.
func (r *KidSubjectRepoSQLite) ReplaceAll(ctx context.Context, kidID int64, subjectIDs []int64) error {
	if r.tx != nil {
		return r.replaceInTx(ctx, r.tx, kidID, subjectIDs)
	}
	tx, err := r.db.Write().BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin kid_subjects replace for kid %d: %w", kidID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := r.replaceInTx(ctx, tx, kidID, subjectIDs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit kid_subjects replace for kid %d: %w", kidID, err)
	}
	return nil
}

// replaceInTx runs the delete-then-insert against the given querier.
func (r *KidSubjectRepoSQLite) replaceInTx(ctx context.Context, q querier, kidID int64, subjectIDs []int64) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM kid_subjects WHERE kid_id = ?`, kidID); err != nil {
		return fmt.Errorf("clear kid_subjects for kid %d: %w", kidID, err)
	}
	for _, subjectID := range subjectIDs {
		if _, err := q.ExecContext(ctx,
			`INSERT INTO kid_subjects (kid_id, subject_id) VALUES (?, ?)`, kidID, subjectID); err != nil {
			return fmt.Errorf("insert kid_subjects (%d, %d): %w", kidID, subjectID, err)
		}
	}
	return nil
}
