package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// memKidSubjectRepo is an in-memory fake of domain.KidSubjectRepository.
type memKidSubjectRepo struct {
	byKid map[int64][]int64
}

func newMemKidSubjectRepo() *memKidSubjectRepo {
	return &memKidSubjectRepo{byKid: map[int64][]int64{}}
}

func (m *memKidSubjectRepo) ListSubjectIDs(_ context.Context, kidID int64) ([]int64, error) {
	ids := append([]int64(nil), m.byKid[kidID]...)
	return ids, nil
}

func (m *memKidSubjectRepo) ReplaceAll(_ context.Context, kidID int64, subjectIDs []int64) error {
	m.byKid[kidID] = append([]int64(nil), subjectIDs...)
	return nil
}

func seedKidRepo(t *testing.T, id, grade, track int) *memKidRepo {
	t.Helper()
	repo := newMemRepo()
	repo.byID[int64(id)] = &domain.Kid{ID: int64(id), Name: "小明", Grade: grade, Track: track, Status: domain.KidStatusActive}
	return repo
}

// TestSubjectServiceListForKidWhitelist: a configured whitelist is authoritative
// and ignores the grade/track mask.
func TestSubjectServiceListForKidWhitelist(t *testing.T) {
	ctx := context.Background()
	kidRepo := seedKidRepo(t, 1, 9, 0)
	subRepo := &fakeSubjectRepo{rows: []*domain.Subject{
		{ID: 1, Code: "chinese", GradeMask: domain.MaskAll},
		{ID: 2, Code: "math", GradeMask: domain.MaskAll},
		{ID: 3, Code: "physics", GradeMask: domain.StageJunior | domain.TrackScience},
	}}
	kidSub := newMemKidSubjectRepo()
	kidSub.byKid[1] = []int64{1, 3}
	svc := NewSubjectServiceWithKids(subRepo, kidRepo, kidSub)

	rows, err := svc.ListForKid(ctx, 1)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, int64(1), rows[0].ID)
	require.Equal(t, int64(3), rows[1].ID)
}

// TestSubjectServiceListForKidFallback: no whitelist -> grade+track mask filtering.
// TestSubjectServiceListForKidNoFallback: ListForKid reads kid_subjects directly; a kid
// with no rows gets an empty list (no grade/track mask fallback).
func TestSubjectServiceListForKidNoFallback(t *testing.T) {
	ctx := context.Background()
	kidRepo := seedKidRepo(t, 1, 10, domain.KidTrackScience)
	subRepo := &fakeSubjectRepo{rows: []*domain.Subject{
		{ID: 1, Code: "chinese", GradeMask: domain.MaskAll},
		{ID: 2, Code: "history", GradeMask: domain.StageSenior | domain.TrackArts},
		{ID: 3, Code: "physics", GradeMask: domain.StageSenior | domain.TrackScience},
	}}
	svc := NewSubjectServiceWithKids(subRepo, kidRepo, newMemKidSubjectRepo())

	rows, err := svc.ListForKid(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, rows, "无 kid_subjects 记录应为空，不回退 mask")
}

func TestSubjectServiceListForKidNotFound(t *testing.T) {
	ctx := context.Background()
	svc := NewSubjectServiceWithKids(&fakeSubjectRepo{}, newMemRepo(), newMemKidSubjectRepo())
	_, err := svc.ListForKid(ctx, 42)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestKidSubjectServiceListEmptyWhenUnconfigured(t *testing.T) {
	ctx := context.Background()
	kidRepo := seedKidRepo(t, 1, 9, 0)
	subRepo := &fakeSubjectRepo{rows: []*domain.Subject{{ID: 1, Code: "chinese", GradeMask: domain.MaskAll}}}
	svc := NewKidSubjectService(kidRepo, subRepo, newMemKidSubjectRepo())

	rows, err := svc.List(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestKidSubjectServiceSetRejectsMissingSubject(t *testing.T) {
	ctx := context.Background()
	kidRepo := seedKidRepo(t, 1, 9, 0)
	subRepo := &fakeSubjectRepo{rows: []*domain.Subject{{ID: 1, Code: "chinese", GradeMask: domain.MaskAll}}}
	svc := NewKidSubjectService(kidRepo, subRepo, newMemKidSubjectRepo())

	err := svc.Set(ctx, 1, []int64{1, 999})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestKidSubjectServiceSetAddsMandatory(t *testing.T) {
	ctx := context.Background()
	kidRepo := seedKidRepo(t, 1, 9, 0)
	subRepo := &fakeSubjectRepo{rows: []*domain.Subject{
		{ID: 1, Code: "chinese", Name: "语文", GradeMask: domain.MaskAll, Mandatory: 1},
		{ID: 2, Code: "math", Name: "数学", GradeMask: domain.MaskAll, Mandatory: 1},
		{ID: 3, Code: "physics", Name: "物理", GradeMask: domain.MaskAll},
	}}
	kidSub := newMemKidSubjectRepo()
	svc := NewKidSubjectService(kidRepo, subRepo, kidSub)

	// Omitting a mandatory subject is tolerated: the server force-includes it.
	err := svc.Set(ctx, 1, []int64{1, 3})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{1, 2, 3}, kidSub.byKid[1])

	// Sending only optional subjects still yields the mandatory set.
	err = svc.Set(ctx, 1, []int64{3})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{1, 2, 3}, kidSub.byKid[1])

	// Including both mandatory subjects is a no-op merge.
	err = svc.Set(ctx, 1, []int64{1, 2, 3})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{1, 2, 3}, kidSub.byKid[1])
}

func (m *memKidSubjectRepo) WithTx(tx domain.Tx) domain.KidSubjectRepository { return m }
