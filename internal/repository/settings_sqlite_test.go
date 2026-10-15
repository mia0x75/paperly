package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// insertTask writes a task row directly; task writes go through the Worker,
// and the task repo has no Insert.
func insertTask(t *testing.T, db *DB, kind, status int, detail string) int64 {
	t.Helper()
	res, err := db.Write().ExecContext(context.Background(), `
INSERT INTO tasks (kind, priority, status, detail) VALUES (?, 50, ?, ?)`,
		kind, status, detail)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

func TestTaskRepoListOrdersByIDDesc(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	for i := 0; i < 3; i++ {
		insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusPending, "{}")
	}
	rows, err := repo.List(ctx, domain.ListTasksFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	// id DESC: newest first.
	require.Greater(t, rows[0].ID, rows[1].ID)
	require.Greater(t, rows[1].ID, rows[2].ID)
}

// TestTaskRepoListFilterNeedsHasStatus is the guard test for "0 being both pending and a sentinel".
func TestTaskRepoListFilterNeedsHasStatus(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusPending, "{}")
	insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusDone, "{}")

	// HasStatus=false: no filter, both come out.
	all, err := repo.List(ctx, domain.ListTasksFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, all, 2)

	// HasStatus=true + status=0: really filters pending, and only 1 comes out.
	pending, err := repo.List(ctx, domain.ListTasksFilter{
		Status: domain.TaskStatusPending, HasStatus: true, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, domain.TaskStatusPending, pending[0].Status)
}

func TestTaskRepoListKeysetCursor(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	ids := make([]int64, 0, 4)
	for i := 0; i < 4; i++ {
		ids = append(ids, insertTask(t, db, domain.TaskKindOther, domain.TaskStatusPending, "{}"))
	}
	page1, err := repo.List(ctx, domain.ListTasksFilter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)

	page2, err := repo.List(ctx, domain.ListTasksFilter{Limit: 2, Cursor: page1[1].ID})
	require.NoError(t, err)
	require.Len(t, page2, 2)
	// No duplicates no misses: each of the four ids appears exactly once.
	seen := map[int64]bool{page1[0].ID: true, page1[1].ID: true, page2[0].ID: true, page2[1].ID: true}
	for _, id := range ids {
		require.True(t, seen[id], "任务 %d 应恰好出现一次", id)
	}
}

func TestTaskRepoCountByStatus(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusPending, "{}")
	insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusPending, "{}")
	insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusFailed, "{}")

	counts, err := repo.CountByStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, counts[domain.TaskStatusPending])
	require.Equal(t, 1, counts[domain.TaskStatusFailed])
	require.NotContains(t, counts, domain.TaskStatusDone, "无行不该凭空出现 0")
}

// TestTaskRepoRetryOnlyFailed guards the atomic retry: the UPDATE must carry
// status=3 in its WHERE, or a running task would be knocked back to pending.
func TestTaskRepoRetryOnlyFailed(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	failed := insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusFailed, "{}")
	running := insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusRunning, "{}")
	done := insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusDone, "{}")

	n, err := repo.Retry(ctx, failed)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	for _, id := range []int64{running, done} {
		n, err := repo.Retry(ctx, id)
		require.NoError(t, err)
		require.Zero(t, n, "非 failed 任务不应被重试")
	}

	got, err := repo.GetByID(ctx, failed)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusPending, got.Status)
	require.Equal(t, 1, got.RetryCount)
	require.Nil(t, got.LastError, "重试必须清掉上次错误，否则日志面板一直显示旧错")
	require.Nil(t, got.FinishedAt)
}

// TestTaskRepoRetryMissingID: retrying a non-existent task returns 0 rows instead of an error.
func TestTaskRepoRetryMissingID(t *testing.T) {
	ctx := context.Background()
	repo := NewTaskRepoSQLite(openTestDB(t))
	n, err := repo.Retry(ctx, 9999)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestTaskRepoClearFinishedKeepsPendingAndRunning(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewTaskRepoSQLite(db)

	insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusPending, "{}")
	insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusRunning, "{}")
	insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusDone, "{}")
	insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusSkipped, "{}")
	insertTask(t, db, domain.TaskKindBackup, domain.TaskStatusFailed, "{}")

	n, err := repo.ClearFinished(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, n, "只清 done + skipped")

	left, err := repo.List(ctx, domain.ListTasksFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, left, 3, "pending / running / failed 必须留着")
}

func newTestSubject(t *testing.T, repo *SubjectRepoSQLite, code, name string, source int) int64 {
	t.Helper()
	id, err := repo.Insert(context.Background(), &domain.Subject{Code: code, Name: name, SortOrder: 1})
	require.NoError(t, err)
	if source == domain.SubjectSourceSystem {
		_, err := repo.db.Write().ExecContext(context.Background(),
			`UPDATE subjects SET source = ? WHERE id = ?`, source, id)
		require.NoError(t, err)
	}
	return id
}

func TestKnowledgePointRepoRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	subjRepo := NewSubjectRepoSQLite(db)
	repo := NewKnowledgePointRepoSQLite(db)
	subjectID := newTestSubject(t, subjRepo, "math", "数学", domain.SubjectSourceUser)

	id, err := repo.Insert(ctx, &domain.KnowledgePoint{SubjectID: subjectID, Name: "分数除法应用题"})
	require.NoError(t, err)
	require.Positive(t, id)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "分数除法应用题", got.Name)
	require.Equal(t, subjectID, got.SubjectID)
	require.Equal(t, domain.SubjectSourceUser, got.Source, "家长新增的知识点恒为自建")

	rows, err := repo.ListBySubject(ctx, subjectID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

// TestKnowledgePointRepoSortOrderAutoIncrement: a new knowledge point is
// auto-placed last. Letting callers compute the order collides when two pages
// add concurrently and both get 3.
func TestKnowledgePointRepoSortOrderAutoIncrement(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	subjRepo := NewSubjectRepoSQLite(db)
	repo := NewKnowledgePointRepoSQLite(db)
	subjectID := newTestSubject(t, subjRepo, "math", "数学", domain.SubjectSourceUser)

	first, err := repo.Insert(ctx, &domain.KnowledgePoint{SubjectID: subjectID, Name: "A"})
	require.NoError(t, err)
	second, err := repo.Insert(ctx, &domain.KnowledgePoint{SubjectID: subjectID, Name: "B"})
	require.NoError(t, err)

	a, err := repo.GetByID(ctx, first)
	require.NoError(t, err)
	b, err := repo.GetByID(ctx, second)
	require.NoError(t, err)
	require.Less(t, a.SortOrder, b.SortOrder)
}

// TestKnowledgePointRepoListIsSubjectScoped: knowledge points are isolated by subject.
func TestKnowledgePointRepoListIsSubjectScoped(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	subjRepo := NewSubjectRepoSQLite(db)
	repo := NewKnowledgePointRepoSQLite(db)
	mathID := newTestSubject(t, subjRepo, "math", "数学", domain.SubjectSourceUser)
	engID := newTestSubject(t, subjRepo, "eng", "英语", domain.SubjectSourceUser)

	_, err := repo.Insert(ctx, &domain.KnowledgePoint{SubjectID: mathID, Name: "圆的面积"})
	require.NoError(t, err)
	_, err = repo.Insert(ctx, &domain.KnowledgePoint{SubjectID: engID, Name: "时态"})
	require.NoError(t, err)

	rows, err := repo.ListBySubject(ctx, mathID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "圆的面积", rows[0].Name)
}

// TestKnowledgePointRepoDeleteCascadesWithSubject: deleting a subject removes its knowledge points
// (DB does ON DELETE CASCADE; the repo should not turn them into orphan rows).
func TestKnowledgePointRepoDeleteCascadesWithSubject(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	subjRepo := NewSubjectRepoSQLite(db)
	repo := NewKnowledgePointRepoSQLite(db)
	subjectID := newTestSubject(t, subjRepo, "math", "数学", domain.SubjectSourceUser)

	id, err := repo.Insert(ctx, &domain.KnowledgePoint{SubjectID: subjectID, Name: "圆的面积"})
	require.NoError(t, err)

	_, err = subjRepo.Delete(ctx, subjectID)
	require.NoError(t, err)

	_, err = repo.GetByID(ctx, id)
	require.ErrorIs(t, err, domain.ErrNotFound, "知识点应随科目级联删除")
}

func TestSubjectRepoWriteOperations(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewSubjectRepoSQLite(db)

	id, err := repo.Insert(ctx, &domain.Subject{Code: "science", Name: "科学", SortOrder: 3})
	require.NoError(t, err)
	require.Positive(t, id)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.SubjectSourceUser, got.Source, "仓储写入的科目恒为自建")

	_, err = repo.UpdateName(ctx, id, "自然科学")
	require.NoError(t, err)
	got, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "自然科学", got.Name)

	n, err := repo.Delete(ctx, id)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

// TestSubjectRepoDeleteMissing: affected rows is 0, so the caller converts it to ErrNotFound.
func TestSubjectRepoDeleteMissing(t *testing.T) {
	ctx := context.Background()
	repo := NewSubjectRepoSQLite(openTestDB(t))
	n, err := repo.Delete(ctx, 9999)
	require.NoError(t, err)
	require.Zero(t, n)
}

// TestSubjectRepoMaskRoundTripAndFilter covers the mask column write/read path
// and the per-stage filter used by GET /api/subjects?grade=N.
func TestSubjectRepoListByStageBitFiltersByMask(t *testing.T) {
	ctx := context.Background()
	repo := NewSubjectRepoSQLite(openTestDB(t))

	_, err := repo.Insert(ctx, &domain.Subject{Code: "chinese", Name: "语文", SortOrder: 1, GradeMask: domain.GradeMaskAll})
	require.NoError(t, err)
	_, err = repo.Insert(ctx, &domain.Subject{Code: "biology", Name: "生物", SortOrder: 2, GradeMask: domain.StageJunior | domain.StageSenior})
	require.NoError(t, err)
	_, err = repo.Insert(ctx, &domain.Subject{Code: "science", Name: "科学", SortOrder: 3, GradeMask: domain.StagePrimary})
	require.NoError(t, err)

	junior, err := repo.ListByStageBit(ctx, domain.StageJunior)
	require.NoError(t, err)
	require.Len(t, junior, 2, "初中可见：语文 + 生物")
	names := []string{junior[0].Name, junior[1].Name}
	require.ElementsMatch(t, []string{"语文", "生物"}, names)

	primary, err := repo.ListByStageBit(ctx, domain.StagePrimary)
	require.NoError(t, err)
	require.Len(t, primary, 2, "小学可见：语文 + 科学")
}

// TestSubjectRepoListFiltered combines stage AND track filters (0 = skip that dimension).
// Masks isolate each filter so every assertion has exactly one intended match.
func TestSubjectRepoListFiltered(t *testing.T) {
	ctx := context.Background()
	repo := NewSubjectRepoSQLite(openTestDB(t))

	_, err := repo.Insert(ctx, &domain.Subject{Code: "chinese", Name: "语文", SortOrder: 1, GradeMask: domain.MaskAll})
	require.NoError(t, err)
	_, err = repo.Insert(ctx, &domain.Subject{Code: "physics", Name: "物理", SortOrder: 2, GradeMask: domain.StageJunior | domain.TrackScience})
	require.NoError(t, err)
	_, err = repo.Insert(ctx, &domain.Subject{Code: "history", Name: "历史", SortOrder: 3, GradeMask: domain.StageJunior | domain.TrackArts})
	require.NoError(t, err)

	// (0,0) == full list.
	all, err := repo.ListFiltered(ctx, 0, 0)
	require.NoError(t, err)
	require.Len(t, all, 3)

	// stage=junior, track=0 → all three (语文/物理/历史 all carry the junior stage).
	junior, err := repo.ListFiltered(ctx, domain.StageJunior, 0)
	require.NoError(t, err)
	require.Len(t, junior, 3)

	// stage=junior, track=science → 语文 (all tracks) + 物理.
	sci, err := repo.ListFiltered(ctx, domain.StageJunior, domain.TrackScience)
	require.NoError(t, err)
	require.Len(t, sci, 2)

	// stage=0, track=arts → 语文 (all tracks) + 历史.
	arts, err := repo.ListFiltered(ctx, 0, domain.TrackArts)
	require.NoError(t, err)
	require.Len(t, arts, 2)
}

// TestSubjectRepoUpdateMask: mask is updatable and read back.
func TestSubjectRepoUpdateMask(t *testing.T) {
	ctx := context.Background()
	repo := NewSubjectRepoSQLite(openTestDB(t))
	id, err := repo.Insert(ctx, &domain.Subject{Code: "biology", Name: "生物", SortOrder: 1})
	require.NoError(t, err)

	n, err := repo.UpdateMask(ctx, id, domain.StagePrimary)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.StagePrimary, got.GradeMask)
}

// TestSubjectRepoImportSeedCarriesMask: the seed import persists the per-row mask
// (and defaults to the fully-open mask when a row omits it).
func TestSubjectRepoImportSeedCarriesMask(t *testing.T) {
	ctx := context.Background()
	repo := NewSubjectRepoSQLite(openTestDB(t))
	n, err := repo.ImportSeed(ctx, []*domain.Subject{
		{Code: "science", Name: "科学", SortOrder: 1, GradeMask: domain.StagePrimary},
		{Code: "math", Name: "数学", SortOrder: 2},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, n)

	sci, err := repo.GetByID(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, domain.StagePrimary, sci.GradeMask, "显式 mask 应落库")

	math, err := repo.GetByID(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, domain.MaskAll, math.GradeMask, "缺省 mask 应回退为全开掩码")
}

func TestUserRepoGetFirstOnEmptyReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepoSQLite(openTestDB(t))
	_, err := repo.GetFirst(ctx)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestUserRepoInsertListRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepoSQLite(openTestDB(t))

	id1, err := repo.Insert(ctx, &domain.User{DisplayName: "妈妈", PinHash: "", PinVersion: 1, Status: domain.UserStatusActive})
	require.NoError(t, err)
	id2, err := repo.Insert(ctx, &domain.User{DisplayName: "爸爸", PinHash: "$2a$10$hash", PinVersion: 1, Status: domain.UserStatusActive})
	require.NoError(t, err)

	list, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, id1, list[0].ID)
	require.Equal(t, "妈妈", list[0].DisplayName)
	require.Equal(t, id2, list[1].ID)
	require.Equal(t, "$2a$10$hash", list[1].PinHash)

	first, err := repo.GetFirst(ctx)
	require.NoError(t, err)
	require.Equal(t, id1, first.ID, "GetFirst 返回 id 最小者")
}

// TestUserRepoInsertRejectsDuplicateName: a duplicate nickname on INSERT maps
// to ErrDuplicateDisplayName. This is the last line of defense for concurrent
// writes that a pre-check cannot stop.
func TestUserRepoInsertRejectsDuplicateName(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepoSQLite(openTestDB(t))

	_, err := repo.Insert(ctx, &domain.User{DisplayName: "妈妈", PinHash: "", PinVersion: 1, Status: domain.UserStatusActive})
	require.NoError(t, err)
	_, err = repo.Insert(ctx, &domain.User{DisplayName: "妈妈", PinHash: "", PinVersion: 1, Status: domain.UserStatusActive})
	require.ErrorIs(t, err, domain.ErrDuplicateDisplayName)
}

func TestUserRepoExistsByDisplayName(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepoSQLite(openTestDB(t))

	exists, err := repo.ExistsByDisplayName(ctx, "妈妈", 0)
	require.NoError(t, err)
	require.False(t, exists, "空表不应命中")

	_, err = repo.Insert(ctx, &domain.User{DisplayName: "妈妈", PinHash: "", PinVersion: 1, Status: domain.UserStatusActive})
	require.NoError(t, err)

	exists, err = repo.ExistsByDisplayName(ctx, "妈妈", 0)
	require.NoError(t, err)
	require.True(t, exists)

	// excludeID takes effect: excluding self should not hit, which is the
	// rename-to-same-name scenario.
	exists, err = repo.ExistsByDisplayName(ctx, "妈妈", idOfFirstUser(t, ctx, repo))
	require.NoError(t, err)
	require.False(t, exists)
}

func idOfFirstUser(t *testing.T, ctx context.Context, repo *UserRepoSQLite) int64 {
	t.Helper()
	u, err := repo.GetFirst(ctx)
	require.NoError(t, err)
	return u.ID
}

func TestOpsRepoListSchemaMigrations(t *testing.T) {
	ctx := context.Background()
	repo := NewOpsRepoSQLite(openTestDB(t))

	rows, err := repo.ListSchemaMigrations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, rows, "迁移脚本会写入基线版本，不该是空表")
	require.Equal(t, 1, rows[0].Version)
	require.Equal(t, "0001_init.sql", rows[0].Name)
	require.NotNil(t, rows[0].DisplayTag)
	require.Equal(t, "v1.0.0", *rows[0].DisplayTag)
}
