package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	"paperly/internal/repository"
)

// failingKidSubjectRepo wraps a real KidSubjectRepository but fails on writes, to prove
// the kid+subjects atomic path rolls the kid write back when the subject write fails.
type failingKidSubjectRepo struct {
	domain.KidSubjectRepository
}

func (f failingKidSubjectRepo) WithTx(tx domain.Tx) domain.KidSubjectRepository { return f }

func (failingKidSubjectRepo) ReplaceAll(context.Context, int64, []int64) error {
	return errors.New("subjects write failed")
}

// atomicKidEnv builds a KidService wired to a real SQLite DB plus a real subject dictionary
// (语文/数学 mandatory, 物理 optional).
type atomicKidEnv struct {
	db         *repository.DB
	svc        *KidService
	kidRepo    domain.KidRepository
	kidSubRepo domain.KidSubjectRepository
	chineseID  int64
	mathID     int64
	physicsID  int64
}

func newAtomicKidEnv(t *testing.T) *atomicKidEnv {
	t.Helper()
	ctx := context.Background()
	db, err := repository.Open(ctx, filepath.Join(t.TempDir(), "paperly.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	subjectRepo := repository.NewSubjectRepoSQLite(db)
	chineseID, err := subjectRepo.Insert(ctx, &domain.Subject{Code: "chinese", Name: "语文", SortOrder: 1, Mandatory: 1})
	require.NoError(t, err)
	mathID, err := subjectRepo.Insert(ctx, &domain.Subject{Code: "math", Name: "数学", SortOrder: 2, Mandatory: 1})
	require.NoError(t, err)
	physicsID, err := subjectRepo.Insert(ctx, &domain.Subject{Code: "physics", Name: "物理", SortOrder: 3})
	require.NoError(t, err)

	kidRepo := repository.NewKidRepoSQLite(db)
	kidSubRepo := repository.NewKidSubjectRepoSQLite(db)
	svc := NewKidService(kidRepo, db, repository.NewSubjectRepoSQLite(db), kidSubRepo)
	return &atomicKidEnv{
		db: db, svc: svc, kidRepo: kidRepo, kidSubRepo: kidSubRepo,
		chineseID: chineseID, mathID: mathID, physicsID: physicsID,
	}
}

func TestKidServiceCreateWithSubjectsCommits(t *testing.T) {
	ctx := context.Background()
	env := newAtomicKidEnv(t)

	resp, err := env.svc.CreateWithSubjects(ctx, CreateKidWithSubjectsReq{
		CreateKidReq: CreateKidReq{Name: "小明", Grade: 9},
		SubjectIDs:   []int64{env.chineseID, env.mathID, env.physicsID},
	})
	require.NoError(t, err)
	require.Positive(t, resp.ID)

	ids, err := env.kidSubRepo.ListSubjectIDs(ctx, resp.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{env.chineseID, env.mathID, env.physicsID}, ids)
}

func TestKidServiceCreateWithSubjectsRollsBackOnSubjectFailure(t *testing.T) {
	ctx := context.Background()
	env := newAtomicKidEnv(t)
	env.svc = NewKidService(env.kidRepo, env.db, repository.NewSubjectRepoSQLite(env.db), failingKidSubjectRepo{env.kidSubRepo})

	_, err := env.svc.CreateWithSubjects(ctx, CreateKidWithSubjectsReq{
		CreateKidReq: CreateKidReq{Name: "小红", Grade: 9},
		SubjectIDs:   []int64{env.chineseID, env.mathID},
	})
	require.Error(t, err)

	exists, err := env.kidRepo.ExistsByName(ctx, "小明", 0)
	require.NoError(t, err)
	require.False(t, exists)
	exists, err = env.kidRepo.ExistsByName(ctx, "小红", 0)
	require.NoError(t, err)
	require.False(t, exists, "kids insert must roll back when the subject write fails")
}

func TestKidServiceCreateWithSubjectsAddsMissingMandatory(t *testing.T) {
	ctx := context.Background()
	env := newAtomicKidEnv(t)

	// The caller omits 数学 (mandatory): the server must still create the kid and
	// force-include the missing mandatory subject rather than reject the request.
	resp, err := env.svc.CreateWithSubjects(ctx, CreateKidWithSubjectsReq{
		CreateKidReq: CreateKidReq{Name: "小明", Grade: 9},
		SubjectIDs:   []int64{env.chineseID},
	})
	require.NoError(t, err)
	require.Positive(t, resp.ID)

	ids, err := env.kidSubRepo.ListSubjectIDs(ctx, resp.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{env.chineseID, env.mathID}, ids, "服务端强制补全必修科目")
}

func TestKidServiceUpdateWithSubjectsCommits(t *testing.T) {
	ctx := context.Background()
	env := newAtomicKidEnv(t)

	created, err := env.svc.CreateWithSubjects(ctx, CreateKidWithSubjectsReq{
		CreateKidReq: CreateKidReq{Name: "小明", Grade: 9},
		SubjectIDs:   []int64{env.chineseID, env.mathID},
	})
	require.NoError(t, err)

	updated, err := env.svc.UpdateWithSubjects(ctx, created.ID, UpdateKidWithSubjectsReq{
		UpdateKidReq: UpdateKidReq{Name: "小明改", Grade: 10, Track: domain.KidTrackScience},
		SubjectIDs:   []int64{env.chineseID, env.mathID},
	})
	require.NoError(t, err)
	require.Equal(t, "小明改", updated.Name)
	require.Equal(t, 10, updated.Grade)
	require.Equal(t, domain.KidTrackScience, updated.Track)
}

func TestKidServiceUpdateWithSubjectsRollsBackOnSubjectFailure(t *testing.T) {
	ctx := context.Background()
	env := newAtomicKidEnv(t)

	created, err := env.svc.CreateWithSubjects(ctx, CreateKidWithSubjectsReq{
		CreateKidReq: CreateKidReq{Name: "小明", Grade: 9},
		SubjectIDs:   []int64{env.chineseID, env.mathID},
	})
	require.NoError(t, err)

	// Swap in a service whose subject writer always fails.
	failing := NewKidService(env.kidRepo, env.db, repository.NewSubjectRepoSQLite(env.db), failingKidSubjectRepo{env.kidSubRepo})
	_, err = failing.UpdateWithSubjects(ctx, created.ID, UpdateKidWithSubjectsReq{
		UpdateKidReq: UpdateKidReq{Name: "小明改", Grade: 10, Track: domain.KidTrackScience},
		SubjectIDs:   []int64{env.chineseID, env.mathID},
	})
	require.Error(t, err)

	// Both the profile update and the subject write must have rolled back.
	got, err := env.kidRepo.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "小明", got.Name, "profile update must roll back with the subject write")
	ids, err := env.kidSubRepo.ListSubjectIDs(ctx, created.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{env.chineseID, env.mathID}, ids)
}

func TestKidServiceCreateWithSubjectsRequiresWiring(t *testing.T) {
	ctx := context.Background()
	svc := NewKidService(nil, nil, nil, nil)
	_, err := svc.CreateWithSubjects(ctx, CreateKidWithSubjectsReq{})
	require.Error(t, err)
}
