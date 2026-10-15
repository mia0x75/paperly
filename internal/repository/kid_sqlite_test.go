package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

func intPtr(i int) *int { return &i }

func newTestKid(name, code string) *domain.Kid {
	return &domain.Kid{
		Name:       name,
		Grade:      9,
		AccessCode: code,
		PinVersion: 1,
		Status:     domain.KidStatusActive,
	}
}

func TestKidRepoInsertGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	id, err := repo.Insert(ctx, newTestKid("小明", "a7k2m9p4"))
	require.NoError(t, err)
	require.Positive(t, id)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "小明", got.Name)
	require.Equal(t, "a7k2m9p4", got.AccessCode)
	require.Equal(t, 9, got.Grade)
	require.Equal(t, 1, got.PinVersion)
	require.Empty(t, got.PinHash, "未显式写入 pin_hash 时落库为空串（NOT NULL，无 NULL 语义）")
	require.Equal(t, domain.KidStatusActive, got.Status)
}

func TestKidRepoGetByIDNotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	_, err := repo.GetByID(ctx, 999)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestKidRepoListFilters(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	idA, _ := repo.Insert(ctx, newTestKid("A", "aaaaaaaa"))
	_, _ = repo.Insert(ctx, newTestKid("B", "bbbbbbbb"))
	_, _ = repo.Insert(ctx, &domain.Kid{
		Name: "C", Grade: 8, AccessCode: "cccccccc", Status: domain.KidStatusActive,
	})

	// Default lists active/graduated, excludes deleted.
	all, err := repo.List(ctx, domain.ListKidsFilter{})
	require.NoError(t, err)
	require.Len(t, all, 3)

	gradeTwo, err := repo.List(ctx, domain.ListKidsFilter{Grade: 8})
	require.NoError(t, err)
	require.Len(t, gradeTwo, 1)
	require.Equal(t, "C", gradeTwo[0].Name)

	// After soft-delete, default list does not contain it.
	require.NoError(t, repo.SoftDelete(ctx, idA))
	after, err := repo.List(ctx, domain.ListKidsFilter{})
	require.NoError(t, err)
	require.Len(t, after, 2)
	for _, s := range after {
		require.NotEqual(t, idA, s.ID)
	}

	deleted, err := repo.List(ctx, domain.ListKidsFilter{Status: domain.KidStatusDeleted})
	require.NoError(t, err)
	require.Len(t, deleted, 1)
	require.Equal(t, idA, deleted[0].ID)

}

func TestKidRepoListIncludeDeleted(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	_, _ = repo.Insert(ctx, newTestKid("A", "aaaaaaaa"))
	graduated := newTestKid("B", "bbbbbbbb")
	graduated.Status = domain.KidStatusGraduated
	_, _ = repo.Insert(ctx, graduated)
	deletedID, _ := repo.Insert(ctx, newTestKid("C", "cccccccc"))
	require.NoError(t, repo.SoftDelete(ctx, deletedID))

	// Default excludes deleted.
	def, err := repo.List(ctx, domain.ListKidsFilter{})
	require.NoError(t, err)
	require.Len(t, def, 2)

	all, err := repo.List(ctx, domain.ListKidsFilter{IncludeDeleted: true})
	require.NoError(t, err)
	require.Len(t, all, 3)
}

func TestKidRepoRestore(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	id, err := repo.Insert(ctx, newTestKid("小明", "a7k2m9p4"))
	require.NoError(t, err)
	require.NoError(t, repo.SoftDelete(ctx, id))

	require.NoError(t, repo.Restore(ctx, id))
	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.KidStatusActive, got.Status)

	// Non-deleted status cannot be restored.
	require.ErrorIs(t, repo.Restore(ctx, id), domain.ErrNotFound)
}

func TestKidRepoUpdate(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	id, err := repo.Insert(ctx, newTestKid("旧名", "dddddddd"))
	require.NoError(t, err)

	err = repo.Update(ctx, &domain.Kid{
		ID:     id,
		Name:   "新名",
		Grade:  8,
		Gender: intPtr(domain.KidGenderFemale),
		Track:  domain.KidTrackScience,
	})
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "新名", got.Name)
	require.Equal(t, 8, got.Grade)
	require.Equal(t, domain.KidGenderFemale, *got.Gender)
	require.Equal(t, domain.KidTrackScience, got.Track, "track 应随 Update 写回")

	err = repo.Update(ctx, &domain.Kid{ID: 9999, Name: "x"})
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestKidRepoSoftDeleteNotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	require.ErrorIs(t, repo.SoftDelete(ctx, 12345), domain.ErrNotFound)
}

func TestKidRepoExistsByAccessCode(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	exists, err := repo.ExistsByAccessCode(ctx, "zzzzzzzz")
	require.NoError(t, err)
	require.False(t, exists)

	_, err = repo.Insert(ctx, newTestKid("小明", "zzzzzzzz"))
	require.NoError(t, err)

	exists, err = repo.ExistsByAccessCode(ctx, "zzzzzzzz")
	require.NoError(t, err)
	require.True(t, exists)
}

func TestKidRepoExistsByName(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	id, err := repo.Insert(ctx, newTestKid("小明", "a7k2m9p4"))
	require.NoError(t, err)

	exists, err := repo.ExistsByName(ctx, "小明", 0)
	require.NoError(t, err)
	require.True(t, exists)

	// Excluding self (update scenario): when only self occupies the name, it should be available.
	exists, err = repo.ExistsByName(ctx, "小明", id)
	require.NoError(t, err)
	require.False(t, exists)

	exists, err = repo.ExistsByName(ctx, "小红", 0)
	require.NoError(t, err)
	require.False(t, exists)

	// Globally unique: after soft-delete the name is still occupied.
	require.NoError(t, repo.SoftDelete(ctx, id))
	exists, err = repo.ExistsByName(ctx, "小明", 0)
	require.NoError(t, err)
	require.True(t, exists, "已软删的姓名也不可复用")
}

func TestKidRepoUniqueNameConstraint(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	_, err := repo.Insert(ctx, newTestKid("小明", "a7k2m9p4"))
	require.NoError(t, err)

	_, err = repo.Insert(ctx, newTestKid("小明", "b8l3n0q5"))
	require.Error(t, err, "唯一索引应拒绝重名插入")
}

func TestKidRepoGetByAccessCode(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	id, err := repo.Insert(ctx, newTestKid("小明", "a7k2m9p4"))
	require.NoError(t, err)

	got, err := repo.GetByAccessCode(ctx, "a7k2m9p4")
	require.NoError(t, err)
	require.Equal(t, id, got.ID)

	_, err = repo.GetByAccessCode(ctx, "nope1234")
	require.ErrorIs(t, err, domain.ErrNotFound)

	// Soft-deleted → ErrNotFound (only exposes active).
	require.NoError(t, repo.SoftDelete(ctx, id))
	_, err = repo.GetByAccessCode(ctx, "a7k2m9p4")
	require.ErrorIs(t, err, domain.ErrNotFound)

	// Non-active (graduated) → ErrNotFound.
	grad := newTestKid("毕业", "grad0001")
	grad.Status = domain.KidStatusGraduated
	_, err = repo.Insert(ctx, grad)
	require.NoError(t, err)
	_, err = repo.GetByAccessCode(ctx, "grad0001")
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestKidRepoUpdateAccessCode(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	id, err := repo.Insert(ctx, newTestKid("小明", "oldcode1"))
	require.NoError(t, err)

	require.NoError(t, repo.UpdateAccessCode(ctx, id, "newcode1"))

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "newcode1", got.AccessCode)

	_, err = repo.GetByAccessCode(ctx, "oldcode1")
	require.ErrorIs(t, err, domain.ErrNotFound)
	byNew, err := repo.GetByAccessCode(ctx, "newcode1")
	require.NoError(t, err)
	require.EqualValues(t, id, byNew.ID)
}

func TestKidRepoUpdateAccessCodeNotFoundWhenDeleted(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	id, err := repo.Insert(ctx, newTestKid("小明", "delcode1"))
	require.NoError(t, err)
	require.NoError(t, repo.SoftDelete(ctx, id))

	require.ErrorIs(t, repo.UpdateAccessCode(ctx, id, "x"), domain.ErrNotFound)
}

func TestKidRepoUpdatePinHashSetAndClear(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	id, err := repo.Insert(ctx, newTestKid("小明", "pincode1"))
	require.NoError(t, err)

	hash := "$2a$10$abcdefghijklmnopqrstuv"
	require.NoError(t, repo.UpdatePinHash(ctx, id, hash))
	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, hash, got.PinHash)

	// Clearing writes a placeholder (NOT NULL column, so never NULL).
	placeholder := "!"
	require.NoError(t, repo.UpdatePinHash(ctx, id, placeholder))
	got, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, placeholder, got.PinHash)
}

// pin_version increments on every PIN set and clear.
func TestKidRepoUpdatePinHashBumpsPinVersion(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)

	id, err := repo.Insert(ctx, newTestKid("小明", "pinver01"))
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 1, got.PinVersion, "初始 pin_version = 1")

	hash := "$2a$10$abcdefghijklmnopqrstuv"
	require.NoError(t, repo.UpdatePinHash(ctx, id, hash))
	got, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 2, got.PinVersion, "设置 PIN 后 pin_version = 2")

	require.NoError(t, repo.UpdatePinHash(ctx, id, "!"))
	got, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 3, got.PinVersion, "清除 PIN 后 pin_version = 3")
}
