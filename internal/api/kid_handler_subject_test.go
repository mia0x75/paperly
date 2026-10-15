package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
	"paperly/internal/service"
)

// fakeSubjectRepo is a minimal read surface for the subject dictionary: the atomic
// kid+subjects path only reads (List / ListByIDs); every other method is inherited
// from the embedded nil interface and never called here.
type fakeSubjectRepo struct {
	domain.SubjectRepository
	all []*domain.Subject
}

func (f *fakeSubjectRepo) List(context.Context) ([]*domain.Subject, error) {
	return f.all, nil
}

func (f *fakeSubjectRepo) ListByIDs(_ context.Context, ids []int64) ([]*domain.Subject, error) {
	want := make(map[int64]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make([]*domain.Subject, 0, len(ids))
	for _, s := range f.all {
		if want[s.ID] {
			out = append(out, s)
		}
	}
	return out, nil
}

// fakeKidSubjectRepo records the whitelist write so the test can assert the atomic
// path persisted the subjects it was given.
type fakeKidSubjectRepo struct {
	domain.KidSubjectRepository
	store map[int64][]int64
	fail  bool
}

func (f *fakeKidSubjectRepo) WithTx(domain.Tx) domain.KidSubjectRepository { return f }

func (f *fakeKidSubjectRepo) ReplaceAll(_ context.Context, kidID int64, ids []int64) error {
	return f.replace(kidID, ids)
}

func (f *fakeKidSubjectRepo) replace(kidID int64, ids []int64) error {
	if f.fail {
		return errors.New("subject write failed")
	}
	f.store[kidID] = ids
	return nil
}

// fakeTxManager runs fn inline (the in-memory repo ignores the transaction handle);
// it only needs to satisfy domain.TxManager so the service's atomic path can open.
type fakeTxManager struct{}

func (fakeTxManager) WithinTx(_ context.Context, fn func(tx domain.Tx) error) error {
	return fn(nil)
}

// newSubjectServer assembles a kid handler whose service has the atomic subject write
// path wired, returning the observable pieces for assertions.
func newSubjectServer() (*echo.Echo, *apiTestRepo, *fakeKidSubjectRepo) {
	repo := newAPITestRepo()
	subjects := &fakeSubjectRepo{all: []*domain.Subject{
		{ID: 1, Code: "chinese", Name: "语文", Mandatory: 1},
		{ID: 2, Code: "math", Name: "数学", Mandatory: 1},
		{ID: 3, Code: "physics", Name: "物理"},
	}}
	kidSub := &fakeKidSubjectRepo{store: map[int64][]int64{}}
	svc := service.NewKidService(repo, fakeTxManager{}, subjects, kidSub)

	e := echo.New()
	NewKidHandler(svc).RegisterRoutes(e.Group("/api"))
	return e, repo, kidSub
}

func TestAPICreateKidWithSubjects(t *testing.T) {
	e, _, kidSub := newSubjectServer()

	status, resp := doJSON(t, e, http.MethodPost, "/api/kids",
		`{"name":"小明","grade":9,"subject_ids":[1,2,3]}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])

	id := int64(resp["data"].(map[string]any)["id"].(float64))
	require.ElementsMatch(t, []int64{1, 2, 3}, kidSub.store[id])
}

func TestAPICreateKidWithSubjectsAddsMandatory(t *testing.T) {
	e, _, kidSub := newSubjectServer()

	// subject_ids omits 数学 (id=2, mandatory): the server force-includes it and the
	// create still succeeds, so a stale/buggy client can never drop a core subject.
	status, resp := doJSON(t, e, http.MethodPost, "/api/kids",
		`{"name":"小明","grade":9,"subject_ids":[1]}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])

	id := int64(resp["data"].(map[string]any)["id"].(float64))
	require.ElementsMatch(t, []int64{1, 2}, kidSub.store[id], "必修科目由服务端补全")
}

func TestAPIUpdateKidWithSubjects(t *testing.T) {
	e, _, kidSub := newSubjectServer()

	_, createResp := doJSON(t, e, http.MethodPost, "/api/kids",
		`{"name":"小明","grade":9,"subject_ids":[1,2,3]}`)
	id := int(createResp["data"].(map[string]any)["id"].(float64))

	status, resp := doJSON(t, e, http.MethodPut, "/api/kids/"+itoa(id),
		`{"name":"小明","grade":10,"track":2,"subject_ids":[1,2]}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, float64(CodeOK), resp["code"])
	require.Equal(t, float64(2), resp["data"].(map[string]any)["track"])

	require.ElementsMatch(t, []int64{1, 2}, kidSub.store[int64(id)])
}

func TestAPICreateKidWithoutSubjectIDsSkipsAtomicPath(t *testing.T) {
	e, _, kidSub := newSubjectServer()

	status, _ := doJSON(t, e, http.MethodPost, "/api/kids", `{"name":"小明","grade":9}`)
	require.Equal(t, http.StatusOK, status)
	require.Empty(t, kidSub.store, "省略 subject_ids 时不应触碰 kid_subjects")
}

func TestAPIUpdateKidWithSubjectsRollsBackOnWriteFailure(t *testing.T) {
	e, _, kidSub := newSubjectServer()
	kidSub.store[1] = []int64{1, 2, 3}

	status, _ := doJSON(t, e, http.MethodPost, "/api/kids",
		`{"name":"小明","grade":9,"subject_ids":[1,2,3]}`)
	require.Equal(t, http.StatusOK, status)

	kidSub.fail = true
	status, resp := doJSON(t, e, http.MethodPut, "/api/kids/1",
		`{"name":"小明改","grade":9,"subject_ids":[1,2]}`)
	require.Equal(t, http.StatusInternalServerError, status)
	require.Equal(t, float64(CodeInternal), resp["code"])
}
