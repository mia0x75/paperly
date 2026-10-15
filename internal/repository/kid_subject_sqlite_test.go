package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// seedKidAndSubjects inserts a kid and two subjects, returning their ids.
func seedKidAndSubjects(t *testing.T, db *DB) (kidID, chineseID, mathID int64) {
	t.Helper()
	ctx := context.Background()
	kidID, err := NewKidRepoSQLite(db).Insert(ctx, newTestKid("小明", "a7k2m9p4"))
	require.NoError(t, err)
	srepo := NewSubjectRepoSQLite(db)
	chineseID, err = srepo.Insert(ctx, &domain.Subject{Code: "chinese", Name: "语文", SortOrder: 1, Mandatory: 1})
	require.NoError(t, err)
	mathID, err = srepo.Insert(ctx, &domain.Subject{Code: "math", Name: "数学", SortOrder: 2, Mandatory: 1})
	require.NoError(t, err)
	return kidID, chineseID, mathID
}

func TestKidSubjectRepoListSubjectIDsEmpty(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	kidID, _, _ := seedKidAndSubjects(t, db)
	repo := NewKidSubjectRepoSQLite(db)

	ids, err := repo.ListSubjectIDs(ctx, kidID)
	require.NoError(t, err)
	require.Empty(t, ids)
}

func TestKidSubjectRepoReplaceAndList(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	kidID, chineseID, mathID := seedKidAndSubjects(t, db)
	repo := NewKidSubjectRepoSQLite(db)

	require.NoError(t, repo.ReplaceAll(ctx, kidID, []int64{chineseID, mathID}))
	ids, err := repo.ListSubjectIDs(ctx, kidID)
	require.NoError(t, err)
	require.Equal(t, []int64{chineseID, mathID}, ids)

	// Replace is full-replacement: a second call drops the previous set.
	require.NoError(t, repo.ReplaceAll(ctx, kidID, []int64{mathID}))
	ids, err = repo.ListSubjectIDs(ctx, kidID)
	require.NoError(t, err)
	require.Equal(t, []int64{mathID}, ids)

	// Empty list clears the whitelist (the kid then has no subjects).
	require.NoError(t, repo.ReplaceAll(ctx, kidID, nil))
	ids, err = repo.ListSubjectIDs(ctx, kidID)
	require.NoError(t, err)
	require.Empty(t, ids)
}

// TestKidSubjectRepoDeleteSubjectRestricted: kid_subjects.subject_id is ON DELETE
// RESTRICT, so a subject still whitelisted by a kid cannot be deleted.
func TestKidSubjectRepoDeleteSubjectRestricted(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	kidID, chineseID, _ := seedKidAndSubjects(t, db)
	repo := NewKidSubjectRepoSQLite(db)
	require.NoError(t, repo.ReplaceAll(ctx, kidID, []int64{chineseID}))

	_, err := db.Write().ExecContext(ctx, `DELETE FROM subjects WHERE id = ?`, chineseID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FOREIGN KEY constraint failed")
}

// TestKidSubjectRepoKidCascade: deleting a kid cascades its whitelist rows.
func TestKidSubjectRepoKidCascade(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	kidID, chineseID, _ := seedKidAndSubjects(t, db)
	repo := NewKidSubjectRepoSQLite(db)
	require.NoError(t, repo.ReplaceAll(ctx, kidID, []int64{chineseID}))

	_, err := db.Write().ExecContext(ctx, `DELETE FROM kids WHERE id = ?`, kidID)
	require.NoError(t, err)
	ids, err := repo.ListSubjectIDs(ctx, kidID)
	require.NoError(t, err)
	require.Empty(t, ids)
}

// TestSubjectRepoListByIDs: batched expansion of a whitelist, ordered by sort_order;
// an empty id list short-circuits to an empty slice.
func TestSubjectRepoListByIDs(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewSubjectRepoSQLite(db)
	s1, err := repo.Insert(ctx, &domain.Subject{Code: "s1", Name: "一", SortOrder: 2})
	require.NoError(t, err)
	s2, err := repo.Insert(ctx, &domain.Subject{Code: "s2", Name: "二", SortOrder: 1})
	require.NoError(t, err)

	rows, err := repo.ListByIDs(ctx, []int64{s1, s2})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	// sort_order ASC: s2 (1) before s1 (2).
	require.Equal(t, s2, rows[0].ID)
	require.Equal(t, s1, rows[1].ID)

	empty, err := repo.ListByIDs(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, empty)

	// Unknown ids are silently omitted (no error).
	rows, err = repo.ListByIDs(ctx, []int64{s1, 99999})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, s1, rows[0].ID)
}
