package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// newDeleteSubjectEnv builds a subject service backed by a fake repo with one
// user-created subject and configurable reference counts.
func newDeleteSubjectEnv(id int64, source int, exams, kps int64) (*SubjectService, *fakeSubjectRepo) {
	repo := &fakeSubjectRepo{
		rows:     []*domain.Subject{{ID: id, Code: "u1234abcd", Name: "自建课", Source: source, GradeMask: domain.MaskAll}},
		examRefs: map[int64]int64{id: exams},
		kpRefs:   map[int64]int64{id: kps},
	}
	return NewSubjectService(repo), repo
}

// TestSubjectServiceDeleteBlockedByExams: a user subject still referenced by exams
// is rejected with ErrInvalidState and the row is left untouched.
func TestSubjectServiceDeleteBlockedByExams(t *testing.T) {
	ctx := context.Background()
	svc, repo := newDeleteSubjectEnv(1, domain.SubjectSourceUser, 3, 0)

	err := svc.Delete(ctx, 1)
	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrInvalidState)
	require.Contains(t, err.Error(), "3 exam(s)", "文案应带引用数量")
	require.Equal(t, 0, repo.deleteCalls, "有引用时不得删除")
}

// TestSubjectServiceDeleteBlockedByKnowledgePoints: a user subject still referenced
// by knowledge points is rejected with ErrInvalidState (they are ON DELETE CASCADE).
func TestSubjectServiceDeleteBlockedByKnowledgePoints(t *testing.T) {
	ctx := context.Background()
	svc, repo := newDeleteSubjectEnv(1, domain.SubjectSourceUser, 0, 5)

	err := svc.Delete(ctx, 1)
	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrInvalidState)
	require.Contains(t, err.Error(), "5 knowledge point(s)", "文案应带引用数量")
	require.Equal(t, 0, repo.deleteCalls, "有引用时不得删除")
}

// TestSubjectServiceDeleteUnreferencedUserSubject: a user subject with no references
// is deleted normally.
func TestSubjectServiceDeleteUnreferencedUserSubject(t *testing.T) {
	ctx := context.Background()
	svc, repo := newDeleteSubjectEnv(1, domain.SubjectSourceUser, 0, 0)

	err := svc.Delete(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 1, repo.deleteCalls, "无引用时应当删除")
}

// TestSubjectServiceDeleteSystemSubjectStillRejected: system-preset subjects stay
// undeletable regardless of references.
func TestSubjectServiceDeleteSystemSubjectStillRejected(t *testing.T) {
	ctx := context.Background()
	svc, repo := newDeleteSubjectEnv(9, domain.SubjectSourceSystem, 0, 0)

	err := svc.Delete(ctx, 9)
	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	require.NotErrorIs(t, err, domain.ErrInvalidState)
	require.Equal(t, 0, repo.deleteCalls)
}

// TestSubjectServiceDeleteNotFound: deleting a missing subject returns ErrNotFound.
func TestSubjectServiceDeleteNotFound(t *testing.T) {
	ctx := context.Background()
	svc, _ := newDeleteSubjectEnv(1, domain.SubjectSourceUser, 0, 0)

	err := svc.Delete(ctx, 404)
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.True(t, errors.Is(err, domain.ErrNotFound))
}
