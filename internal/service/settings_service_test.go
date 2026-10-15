package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// fakeUserRepo is an in-memory implementation of domain.UserRepository.
type fakeUserRepo struct {
	user      *domain.User
	nameCalls int
	pinCalls  int
	nextVer   int
	pinErr    error
}

func (f *fakeUserRepo) GetFirst(context.Context) (*domain.User, error) {
	if f.user == nil {
		return nil, domain.ErrNotFound
	}
	cp := *f.user
	return &cp, nil
}

func (f *fakeUserRepo) GetByID(_ context.Context, id int64) (*domain.User, error) {
	if f.user == nil || f.user.ID != id {
		return nil, domain.ErrNotFound
	}
	cp := *f.user
	return &cp, nil
}

func (f *fakeUserRepo) List(_ context.Context) ([]*domain.User, error) {
	if f.user == nil {
		return nil, nil
	}
	return []*domain.User{{ID: f.user.ID, DisplayName: f.user.DisplayName, PinHash: f.user.PinHash, Status: f.user.Status}}, nil
}

func (f *fakeUserRepo) Insert(_ context.Context, u *domain.User) (int64, error) {
	if f.user != nil && f.user.DisplayName == u.DisplayName {
		return 0, domain.ErrDuplicateDisplayName
	}
	clone := *u
	clone.ID = 1
	f.user = &clone
	return 1, nil
}

func (f *fakeUserRepo) ExistsByDisplayName(_ context.Context, name string, excludeID int64) (bool, error) {
	if f.user == nil || excludeID == f.user.ID {
		return false, nil
	}
	return f.user.DisplayName == name, nil
}

func (f *fakeUserRepo) UpdateDisplayName(_ context.Context, _ int64, name string) (int64, error) {
	f.nameCalls++
	f.user.DisplayName = name
	return 1, nil
}

func (f *fakeUserRepo) UpdatePinHash(_ context.Context, _ int64, hash string) (int64, error) {
	if f.pinErr != nil {
		return 0, f.pinErr
	}
	f.pinCalls++
	if f.nextVer == 0 {
		f.nextVer = 2
	} else {
		f.nextVer++
	}
	f.user.PinHash = hash
	f.user.PinVersion = f.nextVer
	return 1, nil
}

// fakeTaskRepo records the calls the service makes, to assert rules like "only retry failed".
type fakeTaskRepo struct {
	tasks      []*domain.Task
	cursorID   int64
	clearCalls int
	limit      int
	status     int
	hasStatus  bool
	retryCalls int
	retryIDs   []int64
	listErr    error
	claimErr   error
}

func (f *fakeTaskRepo) Insert(_ context.Context, t *domain.Task) (int64, error) {
	max := int64(0)
	for _, task := range f.tasks {
		if task.ID > max {
			max = task.ID
		}
	}
	t.ID = max + 1
	f.tasks = append(f.tasks, t)
	return t.ID, nil
}

func (f *fakeTaskRepo) Claim(_ context.Context) (*domain.Task, error) {
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	for _, task := range f.tasks {
		if task.Status == domain.TaskStatusPending {
			task.Status = domain.TaskStatusRunning
			return task, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeTaskRepo) Finish(_ context.Context, id int64) error {
	t, err := f.byID(id)
	if err != nil {
		return err
	}
	if t.Status != domain.TaskStatusRunning {
		return domain.ErrNotFound
	}
	t.Status = domain.TaskStatusDone
	return nil
}

func (f *fakeTaskRepo) Skip(_ context.Context, id int64, reason string) error {
	t, err := f.byID(id)
	if err != nil {
		return err
	}
	if t.Status != domain.TaskStatusRunning {
		return domain.ErrNotFound
	}
	t.Status = domain.TaskStatusSkipped
	t.LastError = &reason
	return nil
}

func (f *fakeTaskRepo) Fail(_ context.Context, id int64, lastError string, retryable bool) error {
	t, err := f.byID(id)
	if err != nil {
		return err
	}
	if t.Status != domain.TaskStatusRunning {
		return domain.ErrNotFound
	}
	t.LastError = &lastError
	if retryable && t.RetryCount+1 < domain.TaskMaxRetryCount {
		t.RetryCount++
		t.Status = domain.TaskStatusPending
		return nil
	}
	t.RetryCount++
	t.Status = domain.TaskStatusFailed
	return nil
}

func (f *fakeTaskRepo) ClearOlderThan(context.Context, int) (int64, error) {
	return 0, nil
}

func (f *fakeTaskRepo) byID(id int64) (*domain.Task, error) {
	for _, task := range f.tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeTaskRepo) List(_ context.Context, flt domain.ListTasksFilter) ([]*domain.Task, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.cursorID, f.limit = flt.Cursor, flt.Limit
	f.status, f.hasStatus = flt.Status, flt.HasStatus
	out := make([]*domain.Task, 0, len(f.tasks))
	for _, task := range f.tasks {
		if flt.HasStatus && task.Status != flt.Status {
			continue
		}
		if flt.Cursor != 0 && task.ID >= flt.Cursor {
			continue
		}
		out = append(out, task)
		if len(out) == flt.Limit {
			break
		}
	}
	return out, nil
}

func (f *fakeTaskRepo) CountByStatus(context.Context) (map[int]int, error) {
	counts := map[int]int{}
	for _, task := range f.tasks {
		counts[task.Status]++
	}
	return counts, nil
}

func (f *fakeTaskRepo) Retry(_ context.Context, id int64) (int64, error) {
	f.retryCalls++
	f.retryIDs = append(f.retryIDs, id)
	for _, task := range f.tasks {
		if task.ID == id {
			task.Status = domain.TaskStatusPending
			task.RetryCount++
			task.LastError = nil
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeTaskRepo) GetByID(_ context.Context, id int64) (*domain.Task, error) {
	for _, task := range f.tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeTaskRepo) ClearFinished(context.Context) (int64, error) {
	f.clearCalls++
	kept := make([]*domain.Task, 0, len(f.tasks))
	var removed int64
	for _, task := range f.tasks {
		if task.Status == domain.TaskStatusDone || task.Status == domain.TaskStatusSkipped {
			removed++
			continue
		}
		kept = append(kept, task)
	}
	f.tasks = kept
	return removed, nil
}

func newTask(id int64, status int) *domain.Task {
	return &domain.Task{ID: id, Status: status, Kind: domain.TaskKindBackup}
}

func TestUserServiceCreateValidatesAndHashes(t *testing.T) {
	ctx := context.Background()
	repo := &fakeUserRepo{}
	svc := NewUserService(repo)

	_, err := svc.Create(ctx, CreateUserReq{DisplayName: "  "})
	require.ErrorIs(t, err, domain.ErrInvalidInput)

	// Overlong nickname is rejected (counted in runes).
	_, err = svc.Create(ctx, CreateUserReq{DisplayName: repeatRune("家", domain.UserMaxDisplayNameLen+1)})
	require.ErrorIs(t, err, domain.ErrInvalidInput)

	badPin := "123"
	_, err = svc.Create(ctx, CreateUserReq{DisplayName: "妈妈", Pin: &badPin})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	require.Nil(t, repo.user, "非法 PIN 不得建号")

	goodPin := "246810"
	u, err := svc.Create(ctx, CreateUserReq{DisplayName: "  妈妈  ", Pin: &goodPin})
	require.NoError(t, err)
	require.Equal(t, "妈妈", u.DisplayName)
	require.Equal(t, int64(1), u.ID)
	require.NotEmpty(t, u.PinHash, "PIN 必须慢哈希落库")
	require.NotEqual(t, goodPin, u.PinHash, "绝不能存明文")
	require.Equal(t, domain.UserStatusActive, u.Status)
}

func TestUserServiceCreateRejectsDuplicateName(t *testing.T) {
	ctx := context.Background()
	repo := &fakeUserRepo{user: &domain.User{ID: 1, DisplayName: "妈妈"}}
	svc := NewUserService(repo)

	_, err := svc.Create(ctx, CreateUserReq{DisplayName: "妈妈"})
	require.ErrorIs(t, err, domain.ErrDuplicateDisplayName)
}

func TestUserServiceCreateWithoutPin(t *testing.T) {
	ctx := context.Background()
	repo := &fakeUserRepo{}
	svc := NewUserService(repo)

	u, err := svc.Create(ctx, CreateUserReq{DisplayName: "奶奶"})
	require.NoError(t, err)
	require.Empty(t, u.PinHash, "未设 PIN 落空串（users.pin_hash NOT NULL）")
}

func TestUserServiceList(t *testing.T) {
	ctx := context.Background()
	repo := &fakeUserRepo{user: &domain.User{ID: 1, DisplayName: "妈妈"}}
	svc := NewUserService(repo)

	list, err := svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "妈妈", list[0].DisplayName)
}

// Only failed tasks can be retried: otherwise "retry" would re-run a completed backup task,
// producing one more snapshot.
func TestTaskServiceRetryRefusesNonFailed(t *testing.T) {
	ctx := context.Background()
	repo := &fakeTaskRepo{tasks: []*domain.Task{newTask(1, domain.TaskStatusRunning)}}
	svc := NewTaskService(repo)

	_, err := svc.Retry(ctx, 1)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	require.Zero(t, repo.retryCalls, "非 failed 任务不得下发 Retry")
}

// Once the max retry count is reached no further retries are allowed; otherwise a parent
// clicking repeatedly would occupy the Worker queue forever.
func TestTaskServiceRetryRefusedPastMax(t *testing.T) {
	ctx := context.Background()
	task := newTask(1, domain.TaskStatusFailed)
	task.RetryCount = domain.TaskMaxRetryCount
	svc := NewTaskService(&fakeTaskRepo{tasks: []*domain.Task{task}})

	_, err := svc.Retry(ctx, 1)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestTaskServiceRetryMissing(t *testing.T) {
	ctx := context.Background()
	svc := NewTaskService(&fakeTaskRepo{})
	_, err := svc.Retry(ctx, 7)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestTaskServiceRetryAllowsFailed(t *testing.T) {
	ctx := context.Background()
	repo := &fakeTaskRepo{tasks: []*domain.Task{newTask(7, domain.TaskStatusFailed)}}
	svc := NewTaskService(repo)

	got, err := svc.Retry(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, []int64{7}, repo.retryIDs)
	require.Equal(t, domain.TaskStatusPending, got.Status, "重试后应回到 pending")
}

// Cleanup is one SQL statement in the repository; the service must not List then Delete
// one by one (N+1 is forbidden).
func TestTaskServiceClearFinishedUsesRepo(t *testing.T) {
	ctx := context.Background()
	repo := &fakeTaskRepo{}
	svc := NewTaskService(repo)
	_, err := svc.ClearFinished(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, repo.clearCalls)
	require.Zero(t, repo.retryCalls)
}

func TestTaskServiceListRejectsInvalidFilter(t *testing.T) {
	ctx := context.Background()
	repo := &fakeTaskRepo{}
	svc := NewTaskService(repo)

	_, err := svc.List(ctx, 0, false, -1, 10)
	require.ErrorIs(t, err, domain.ErrInvalidInput, "负 cursor 必须被拒")

	_, err = svc.List(ctx, 999, true, 0, 10)
	require.ErrorIs(t, err, domain.ErrInvalidInput, "未知状态必须被拒")

	// Valid statuses must pass: cancelled=5 shares the same enum as the dashboard.
	_, err = svc.List(ctx, domain.TaskStatusCancelled, true, 0, 10)
	require.NoError(t, err)
}

func TestTaskServiceListCountsTotal(t *testing.T) {
	ctx := context.Background()
	repo := &fakeTaskRepo{tasks: []*domain.Task{
		newTask(3, domain.TaskStatusDone),
		newTask(2, domain.TaskStatusDone),
		newTask(1, domain.TaskStatusFailed),
	}}
	svc := NewTaskService(repo)

	page, err := svc.List(ctx, 0, false, 0, 2)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.Equal(t, []int64{3, 2}, []int64{page.Items[0].ID, page.Items[1].ID})
	require.True(t, page.HasMore)
	// Cursor pagination does not provide an exact total; only per-status aggregate counts
	// are reported.
	require.Equal(t, 2, page.Counts[domain.TaskStatusDone])
	require.Equal(t, 1, page.Counts[domain.TaskStatusFailed])

	last, err := svc.List(ctx, 0, false, 2, 2)
	require.NoError(t, err)
	require.False(t, last.HasMore)
	require.Zero(t, last.NextCursor)
}

// fakeKPRepo is an in-memory implementation of domain.KnowledgePointRepository.
type fakeKPRepo struct {
	rows     []*domain.KnowledgePoint
	nextID   int64
	inserted []*domain.KnowledgePoint
}

func (f *fakeKPRepo) Insert(_ context.Context, kp *domain.KnowledgePoint) (int64, error) {
	f.nextID++
	cp := *kp
	cp.ID = f.nextID
	cp.Source = domain.SubjectSourceUser
	f.inserted = append(f.inserted, &cp)
	f.rows = append(f.rows, &cp)
	return f.nextID, nil
}

func (f *fakeKPRepo) UpdateName(_ context.Context, id int64, name string) (int64, error) {
	for _, kp := range f.rows {
		if kp.ID == id {
			kp.Name = name
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeKPRepo) Delete(_ context.Context, id int64) (int64, error) {
	for i, kp := range f.rows {
		if kp.ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeKPRepo) GetByID(_ context.Context, id int64) (*domain.KnowledgePoint, error) {
	for _, kp := range f.rows {
		if kp.ID == id {
			return kp, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeKPRepo) ListBySubject(_ context.Context, subjectID int64) ([]*domain.KnowledgePoint, error) {
	out := make([]*domain.KnowledgePoint, 0, len(f.rows))
	for _, kp := range f.rows {
		if kp.SubjectID == subjectID {
			out = append(out, kp)
		}
	}
	return out, nil
}

func TestKnowledgePointServiceValidatesInput(t *testing.T) {
	ctx := context.Background()
	svc := NewKnowledgePointService(&fakeKPRepo{}, newSubjectRepoStub(map[int64]bool{1: true}))

	_, err := svc.Create(ctx, 1, "  ")
	require.ErrorIs(t, err, domain.ErrInvalidInput)

	_, err = svc.Create(ctx, 1, repeatRune("长", KnowledgePointMaxNameLen+1))
	require.ErrorIs(t, err, domain.ErrInvalidInput, "超长知识点名应被拒")

	_, err = svc.Create(ctx, 1, "?!*")
	require.ErrorIs(t, err, domain.ErrInvalidInput, "纯符号名无信息量，应被拒")

	// Subject missing: reject to avoid creating an orphan knowledge point
	// (the DB's FK error would be opaque to parents).
	_, err = svc.Create(ctx, 999, "圆的面积")
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestKnowledgePointServiceCreateTrimsName(t *testing.T) {
	ctx := context.Background()
	kpRepo := &fakeKPRepo{}
	svc := NewKnowledgePointService(kpRepo, newSubjectRepoStub(map[int64]bool{1: true}))

	created, err := svc.Create(ctx, 1, "  分数除法应用题  ")
	require.NoError(t, err)
	require.Positive(t, created.ID)
	require.Equal(t, "分数除法应用题", created.Name)
	require.Equal(t, "分数除法应用题", kpRepo.inserted[0].Name)
	require.Equal(t, domain.SubjectSourceUser, kpRepo.inserted[0].Source)
}

// Guards cross-subject overreach: with the path giving subject=1 and the id belonging to
// subject=2, it must 404 so another subject cannot be edited.
func TestKnowledgePointServiceChecksSubjectOwnership(t *testing.T) {
	ctx := context.Background()
	kpRepo := &fakeKPRepo{
		rows: []*domain.KnowledgePoint{
			{ID: 1, SubjectID: 2, Name: "时态"},
			{ID: 2, SubjectID: 1, Name: "圆的面积"},
		},
	}
	svc := NewKnowledgePointService(kpRepo, newSubjectRepoStub(map[int64]bool{1: true, 2: true}))

	require.NoError(t, svc.Update(ctx, 1, 2, "被动语态"), "归属正确时允许改")
	require.Equal(t, "被动语态", kpRepo.rows[1].Name)

	err := svc.Update(ctx, 1, 1, "时态改错科目")
	require.ErrorIs(t, err, domain.ErrNotFound, "id 不属于路径科目必须 404")
	require.Equal(t, "时态", kpRepo.rows[0].Name, "越权请求不得改到数据")

	require.ErrorIs(t, svc.Delete(ctx, 1, 1), domain.ErrNotFound)
	require.Len(t, kpRepo.rows, 2, "越权删除不得生效")
}

func TestKnowledgePointServiceMissingID(t *testing.T) {
	ctx := context.Background()
	svc := NewKnowledgePointService(&fakeKPRepo{}, newSubjectRepoStub(map[int64]bool{1: true}))

	_, err := svc.List(ctx, 1)
	require.NoError(t, err)

	require.ErrorIs(t, svc.Update(ctx, 404, 1, "x"), domain.ErrNotFound)
	require.ErrorIs(t, svc.Delete(ctx, 404, 1), domain.ErrNotFound)
}

// repeatRune builds a string of n copies of r, used to test length limits.
func repeatRune(r string, n int) string {
	out := make([]byte, 0, len(r)*n)
	for i := 0; i < n; i++ {
		out = append(out, r...)
	}
	return string(out)
}

func TestSubjectServiceRejectsMissingRequiredFields(t *testing.T) {
	ctx := context.Background()
	repo := &fakeSubjectRepo{}
	svc := NewSubjectService(repo)

	_, err := svc.Create(ctx, "  ")
	require.ErrorIs(t, err, domain.ErrInvalidInput)

	_, err = svc.Create(ctx, repeatRune("科", domain.SubjectMaxNameLen+1))
	require.ErrorIs(t, err, domain.ErrInvalidInput)

	_, err = svc.Create(ctx, "?!*")
	require.ErrorIs(t, err, domain.ErrInvalidInput, "纯符号名无信息量，应被拒")
}

// User-created subjects have no manual code entry, so the service must derive a stable
// value that does not collide with built-in codes.
func TestSubjectServiceCreateDerivesCode(t *testing.T) {
	ctx := context.Background()
	repo := &fakeSubjectRepo{}
	svc := NewSubjectService(repo)

	created, err := svc.Create(ctx, " 科学 ")
	require.NoError(t, err)
	require.Positive(t, created.ID)
	require.Len(t, repo.rows, 1)
	require.Equal(t, "科学", created.Name, "回显必须是裁剪后的名字，不是请求原文")
	require.NotEmpty(t, created.Code)
	require.Equal(t, domain.SubjectSourceUser, created.Source)
	require.Equal(t, 1, created.SortOrder)

	// Duplicate names are rejected outright: code/name could both collide in the DB,
	// and reporting UNIQUE is meaningless to parents.
	_, err = svc.Create(ctx, "科学")
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	require.Len(t, repo.rows, 1, "重名不得插进去")
}

func TestSubjectServiceDeleteSystemSubjectForbidden(t *testing.T) {
	ctx := context.Background()
	repo := &fakeSubjectRepo{
		rows: []*domain.Subject{{ID: 3, Code: "math", Name: "数学", Source: domain.SubjectSourceSystem}},
	}
	svc := NewSubjectService(repo)

	err := svc.Delete(ctx, 3)
	require.ErrorIs(t, err, domain.ErrInvalidInput, "内置科目不可删")
	require.Zero(t, repo.deleteCalls)
}

func TestSubjectServiceDeleteUserSubject(t *testing.T) {
	ctx := context.Background()
	repo := &fakeSubjectRepo{
		rows: []*domain.Subject{{ID: 9, Code: "sci", Name: "科学", Source: domain.SubjectSourceUser}},
	}
	svc := NewSubjectService(repo)

	require.NoError(t, svc.Delete(ctx, 9))
	require.Equal(t, 1, repo.deleteCalls)
}

func TestSubjectServiceListByGradeFilters(t *testing.T) {
	ctx := context.Background()
	repo := &fakeSubjectRepo{rows: []*domain.Subject{
		{ID: 1, Code: "chinese", Name: "语文", GradeMask: domain.GradeMaskAll},
		{ID: 2, Code: "science", Name: "科学", GradeMask: domain.StagePrimary},
		{ID: 3, Code: "physics", Name: "物理", GradeMask: domain.StageJunior | domain.StageSenior},
	}}
	svc := NewSubjectService(repo)

	// grade<=0 / out-of-range → full dictionary (backward-compatible contract).
	for _, g := range []int{0, -1, 13} {
		rows, err := svc.ListByGrade(ctx, g)
		require.NoError(t, err)
		require.Len(t, rows, 3, "grade=%d 应返回全部", g)
	}

	primary, err := svc.ListByGrade(ctx, 3)
	require.NoError(t, err)
	require.Len(t, primary, 2, "小学：语文 + 科学")

	junior, err := svc.ListByGrade(ctx, 8)
	require.NoError(t, err)
	require.Len(t, junior, 2, "初中：语文 + 物理")

	senior, err := svc.ListByGrade(ctx, 11)
	require.NoError(t, err)
	require.Len(t, senior, 2, "高中：语文 + 物理")
}

func TestSubjectServiceUpdateMask(t *testing.T) {
	ctx := context.Background()
	repo := &fakeSubjectRepo{rows: []*domain.Subject{
		{ID: 5, Code: "biology", Name: "生物", GradeMask: domain.GradeMaskAll},
	}}
	svc := NewSubjectService(repo)

	require.NoError(t, svc.UpdateMask(ctx, 5, domain.StageJunior|domain.StageSenior))
	require.Equal(t, domain.StageJunior|domain.StageSenior, repo.rows[0].GradeMask)

	// High 3 track bits are part of the mask: arts/science/general all accepted.
	require.NoError(t, svc.UpdateMask(ctx, 5, domain.TrackArts))
	require.Equal(t, domain.TrackArts, repo.rows[0].GradeMask)
	require.NoError(t, svc.UpdateMask(ctx, 5, domain.MaskAll))
	require.Equal(t, domain.MaskAll, repo.rows[0].GradeMask)

	require.ErrorIs(t, svc.UpdateMask(ctx, 5, -1), domain.ErrInvalidInput)
	require.ErrorIs(t, svc.UpdateMask(ctx, 5, domain.MaskMax+1), domain.ErrInvalidInput)
	require.ErrorIs(t, svc.UpdateMask(ctx, 999, 1), domain.ErrNotFound)
}

func TestSubjectServiceGetMissing(t *testing.T) {
	svc := NewSubjectService(&fakeSubjectRepo{})
	_, err := svc.Get(context.Background(), 42)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

// Locks the grade+track filter contract: stage AND track, with 0 meaning "do not filter
// that dimension". Masks are chosen to isolate each filter (every subject carries the
// junior stage; math adds senior).
func TestSubjectServiceListByGradeTrack(t *testing.T) {
	ctx := context.Background()
	repo := &fakeSubjectRepo{rows: []*domain.Subject{
		// chinese: core subject visible to every track (carries both track bits, like mask=30).
		{ID: 1, Code: "chinese", GradeMask: domain.StageJunior | domain.StageSenior | domain.TrackArts | domain.TrackScience},
		// physics: junior+senior, science-only.
		{ID: 2, Code: "physics", GradeMask: domain.StageJunior | domain.StageSenior | domain.TrackScience},
		// history: junior+senior, arts-only.
		{ID: 3, Code: "history", GradeMask: domain.StageJunior | domain.StageSenior | domain.TrackArts},
		// math: junior+senior, both tracks (junior-only subject would drop the general bit).
		{ID: 4, Code: "math", GradeMask: domain.StageJunior | domain.StageSenior | domain.TrackArts | domain.TrackScience},
	}}
	svc := NewSubjectService(repo)

	codes := func(rows []*domain.Subject) []string {
		out := make([]string, 0, len(rows))
		for _, s := range rows {
			out = append(out, s.Code)
		}
		return out
	}

	// No grade, no track → full dictionary.
	all, err := svc.ListByGradeTrack(ctx, 0, 0)
	require.NoError(t, err)
	require.Len(t, all, 4)

	// Junior (grade < 10) + undecided(track=0) → stage filter only → all 4.
	junior, err := svc.ListByGradeTrack(ctx, 8, domain.KidTrackUndecided)
	require.NoError(t, err)
	require.Len(t, junior, 4)

	// Junior (grade < 10) + arts(track=1) → track is FORCED OFF below senior high → all 4.
	artsJunior, err := svc.ListByGradeTrack(ctx, 8, domain.KidTrackArts)
	require.NoError(t, err)
	require.Len(t, artsJunior, 4)

	// Junior (grade < 10) + science(track=2) → track still forced off → all 4.
	sciJunior, err := svc.ListByGradeTrack(ctx, 8, domain.KidTrackScience)
	require.NoError(t, err)
	require.Len(t, sciJunior, 4)

	// Senior (grade >= 10) + arts(track=1) → track takes effect: chinese + history + math.
	artsSenior, err := svc.ListByGradeTrack(ctx, 10, domain.KidTrackArts)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"chinese", "history", "math"}, codes(artsSenior))

	// Senior + science(track=2) → chinese + physics + math.
	sciSenior, err := svc.ListByGradeTrack(ctx, 10, domain.KidTrackScience)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"chinese", "physics", "math"}, codes(sciSenior))

	// Track-only (no grade): track applies even without a stage → arts → chinese + history + math.
	anyArts, err := svc.ListByGradeTrack(ctx, 0, domain.KidTrackArts)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"chinese", "history", "math"}, codes(anyArts))
}

// fakeSubjectRepo covers the read/write surface of SubjectRepository.
type fakeSubjectRepo struct {
	rows        []*domain.Subject
	deleteCalls int
	nextID      int64
	examRefs    map[int64]int64 // subject id -> exam count
	kpRefs      map[int64]int64 // subject id -> knowledge point count
}

func (f *fakeSubjectRepo) List(context.Context) ([]*domain.Subject, error) {
	return f.rows, nil
}

func (f *fakeSubjectRepo) GetByID(_ context.Context, id int64) (*domain.Subject, error) {
	for _, s := range f.rows {
		if s.ID == id {
			return s, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeSubjectRepo) Insert(_ context.Context, in *domain.Subject) (int64, error) {
	f.nextID++
	cp := *in
	cp.ID = f.nextID
	cp.Source = domain.SubjectSourceUser
	f.rows = append(f.rows, &cp)
	return f.nextID, nil
}

func (f *fakeSubjectRepo) UpdateName(_ context.Context, id int64, name string) (int64, error) {
	for _, s := range f.rows {
		if s.ID == id {
			s.Name = name
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeSubjectRepo) ListByStageBit(_ context.Context, bit int) ([]*domain.Subject, error) {
	out := make([]*domain.Subject, 0, len(f.rows))
	for _, s := range f.rows {
		if s.GradeMask&bit != 0 {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeSubjectRepo) ListFiltered(_ context.Context, stageBit, trackBit int) ([]*domain.Subject, error) {
	out := make([]*domain.Subject, 0, len(f.rows))
	for _, s := range f.rows {
		if stageBit != 0 && s.GradeMask&stageBit == 0 {
			continue
		}
		if trackBit != 0 && s.GradeMask&trackBit == 0 {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeSubjectRepo) ListByIDs(_ context.Context, ids []int64) ([]*domain.Subject, error) {
	want := make(map[int64]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make([]*domain.Subject, 0, len(ids))
	for _, s := range f.rows {
		if want[s.ID] {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeSubjectRepo) UpdateMask(_ context.Context, id int64, mask int) (int64, error) {
	for _, s := range f.rows {
		if s.ID == id {
			s.GradeMask = mask
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeSubjectRepo) CountExams(_ context.Context, id int64) (int64, error) {
	return f.examRefs[id], nil
}

func (f *fakeSubjectRepo) CountKnowledgePoints(_ context.Context, id int64) (int64, error) {
	return f.kpRefs[id], nil
}

func (f *fakeSubjectRepo) Delete(_ context.Context, id int64) (int64, error) {
	f.deleteCalls++
	for i, s := range f.rows {
		if s.ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeSubjectRepo) ImportSeed(context.Context, []*domain.Subject) (int64, error) {
	return 0, nil
}

// newSubjectRepoStub builds a SubjectRepository that only cares about "which ids exist".
func newSubjectRepoStub(exists map[int64]bool) *fakeSubjectRepo {
	rows := make([]*domain.Subject, 0, len(exists))
	for id := range exists {
		rows = append(rows, &domain.Subject{ID: id, Source: domain.SubjectSourceUser})
	}
	return &fakeSubjectRepo{rows: rows}
}

type fakeOpsRepo struct {
	migrations []*domain.SchemaMigration
	err        error
}

func (f *fakeOpsRepo) ListSchemaMigrations(context.Context) ([]*domain.SchemaMigration, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.migrations, nil
}

func (f *fakeOpsRepo) SchemaDisplayTag(context.Context) (string, error) { return "v1.0.0", nil }

func (f *fakeOpsRepo) IntegrityCheck(context.Context) (string, error) { return "ok", nil }

func (f *fakeOpsRepo) DBSize(context.Context) (int64, error) { return 1, nil }

func (f *fakeOpsRepo) Optimize(context.Context) error { return nil }

func (f *fakeOpsRepo) VacuumInto(context.Context, string) error { return nil }

func (f *fakeOpsRepo) VerifyFileIntegrity(context.Context, string) (string, error) {
	return "ok", nil
}

type fakeCursorRepo struct {
	rows []*domain.MaintenanceCursor
	err  error
}

func (f *fakeCursorRepo) List(context.Context) ([]*domain.MaintenanceCursor, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (f *fakeCursorRepo) Get(_ context.Context, task string) (*domain.MaintenanceCursor, error) {
	return nil, domain.ErrNotFound
}

func (f *fakeCursorRepo) Upsert(_ context.Context, _ *domain.MaintenanceCursor) error {
	return nil
}

func TestSystemOpsServicePassesThrough(t *testing.T) {
	ctx := context.Background()
	ops := &fakeOpsRepo{migrations: []*domain.SchemaMigration{{Version: 1, Name: "0001_init.sql"}}}
	cursors := &fakeCursorRepo{rows: []*domain.MaintenanceCursor{{TaskName: "backup"}}}

	svc := NewSystemOpsService(ops, cursors)
	m, err := svc.SchemaMigrations(ctx)
	require.NoError(t, err)
	require.Len(t, m, 1)

	c, err := svc.Cursors(ctx)
	require.NoError(t, err)
	require.Len(t, c, 1)
}

func TestSystemOpsServiceWrapsErrors(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("db down")
	svc := NewSystemOpsService(&fakeOpsRepo{err: boom}, &fakeCursorRepo{err: boom})

	_, err := svc.SchemaMigrations(ctx)
	require.ErrorIs(t, err, boom)
	_, err = svc.Cursors(ctx)
	require.ErrorIs(t, err, boom)
}

func (m *fakeUserRepo) WithTx(tx domain.Tx) domain.UserRepository { return m }

func (m *fakeTaskRepo) WithTx(tx domain.Tx) domain.TaskRepository { return m }

func (m *fakeKPRepo) WithTx(tx domain.Tx) domain.KnowledgePointRepository { return m }

func (m *fakeSubjectRepo) WithTx(tx domain.Tx) domain.SubjectRepository { return m }

func (m *fakeOpsRepo) WithTx(tx domain.Tx) domain.OpsRepository { return m }

func (m *fakeCursorRepo) WithTx(tx domain.Tx) domain.MaintenanceCursorRepository { return m }
