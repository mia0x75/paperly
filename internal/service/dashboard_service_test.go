package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"paperly/internal/domain"
)

// fixedNow fixes "today" = 2026-09-15 12:00 UTC so the window/this-month/last-month
// are reproducible.
func fixedNow() time.Time {
	return time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
}

// fakeDashRepo is a programmable stub of domain.KidDashboardRepository.
type fakeDashRepo struct {
	counts  domain.ScopeCounts
	series  []domain.UploadPoint
	kids    []domain.KidUploadSeries
	dist    []domain.SubjectDistribution
	distAll []domain.SubjectDistribution

	// Records the received window, to assert the service passed the right time basis.
	gotWindow domain.DashboardWindow
	gotKid    *int64
	gotKidIDs []int64
	err       error
}

func (f *fakeDashRepo) ScopeCounts(_ context.Context, kidID *int64, w domain.DashboardWindow) (domain.ScopeCounts, error) {
	f.gotKid, f.gotWindow = kidID, w
	return f.counts, f.err
}

func (f *fakeDashRepo) UploadSeriesForKid(_ context.Context, _ int64, w domain.DashboardWindow) ([]domain.UploadPoint, error) {
	f.gotWindow = w
	return f.series, f.err
}

func (f *fakeDashRepo) UploadSeriesForKids(_ context.Context, kidIDs []int64, w domain.DashboardWindow) ([]domain.KidUploadSeries, error) {
	f.gotKidIDs, f.gotWindow = kidIDs, w
	return f.kids, f.err
}

func (f *fakeDashRepo) SubjectDistributionForKid(_ context.Context, _ int64) ([]domain.SubjectDistribution, error) {
	return f.dist, f.err
}

func (f *fakeDashRepo) SubjectDistributionAll(_ context.Context) ([]domain.SubjectDistribution, error) {
	return f.distAll, f.err
}

// fakeKidRepo satisfies domain.KidRepository; GetByID returns f.kid, List returns f.kids.
type fakeKidRepo struct {
	kid  *domain.Kid
	kids []*domain.Kid
	err  error
}

func (f *fakeKidRepo) GetByID(_ context.Context, _ int64) (*domain.Kid, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.kid, nil
}
func (f *fakeKidRepo) Insert(_ context.Context, _ *domain.Kid) (int64, error) { return 0, nil }
func (f *fakeKidRepo) List(_ context.Context, f2 domain.ListKidsFilter) ([]*domain.Kid, error) {
	// Same semantics as the real repositories: status filtering belongs to the repo layer,
	// so passing Status=Active only returns active kids.
	out := make([]*domain.Kid, 0, len(f.kids))
	for _, k := range f.kids {
		if f2.Status != 0 && k.Status != f2.Status {
			continue
		}
		if f2.Status == 0 && !f2.IncludeDeleted && k.Status == domain.KidStatusDeleted {
			continue
		}
		out = append(out, k)
	}
	return out, nil
}
func (f *fakeKidRepo) Update(_ context.Context, _ *domain.Kid) error { return nil }
func (f *fakeKidRepo) SoftDelete(_ context.Context, _ int64) error   { return nil }
func (f *fakeKidRepo) ExistsByAccessCode(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (f *fakeKidRepo) ExistsByName(_ context.Context, _ string, _ int64) (bool, error) {
	return false, nil
}
func (f *fakeKidRepo) Restore(_ context.Context, _ int64) error { return nil }
func (f *fakeKidRepo) Graduate(_ context.Context, _ int64) error {
	if f.kid != nil && f.kid.Status == domain.KidStatusActive {
		f.kid.Status = domain.KidStatusGraduated
	}
	return nil
}
func (f *fakeKidRepo) GetByAccessCode(_ context.Context, _ string) (*domain.Kid, error) {
	return nil, domain.ErrNotFound
}
func (f *fakeKidRepo) UpdateAccessCode(_ context.Context, _ int64, _ string) error { return nil }
func (f *fakeKidRepo) UpdatePinHash(_ context.Context, _ int64, _ string) error    { return nil }

func newTestSvc(repo domain.KidDashboardRepository, kid *domain.Kid) *DashboardService {
	return NewDashboardService(repo, &fakeKidRepo{kid: kid}, fixedNow)
}

func TestBuildWindowForTrend7(t *testing.T) {
	repo := &fakeDashRepo{}
	svc := newTestSvc(repo, nil)

	if _, err := svc.Build(context.Background(), 1, TrendWindow7); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The 7-day window covers today and the 6 prior days: 09-09 ~ 09-15
	if got := repo.gotWindow.From.Format("2006-01-02"); got != "2026-09-09" {
		t.Errorf("From = %s, want 2026-09-09", got)
	}
	if got := repo.gotWindow.To.Format("2006-01-02 15:04:05"); got != "2026-09-15 23:59:59" {
		t.Errorf("To = %s, want 2026-09-15 23:59:59（窗口末日必须补到 23:59:59，否则今天归档的卷子落在窗口外）", got)
	}
	if got := repo.gotWindow.ThisMonthFrom.Format("2006-01-02"); got != "2026-09-01" {
		t.Errorf("ThisMonthFrom = %s, want 2026-09-01", got)
	}
	if got := repo.gotWindow.LastMonthFrom.Format("2006-01-02"); got != "2026-08-01" {
		t.Errorf("LastMonthFrom = %s, want 2026-08-01", got)
	}
}

func TestBuildWindowForTrend30AndInvalidFallback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		trendDays  int
		wantFrom   string
		wantLength int
	}{
		{"30 天", TrendWindow30, "2026-08-17", 30},
		{"非法值收敛到 7", 999, "2026-09-09", 7},
		{"0 收敛到 7", 0, "2026-09-09", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeDashRepo{}
			svc := newTestSvc(repo, nil)
			view, err := svc.Build(context.Background(), 1, tc.trendDays)
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if got := repo.gotWindow.From.Format("2006-01-02"); got != tc.wantFrom {
				t.Errorf("From = %s, want %s", got, tc.wantFrom)
			}
			if len(view.Series) != tc.wantLength {
				t.Errorf("序列长度 = %d, want %d", len(view.Series), tc.wantLength)
			}
		})
	}
}

func TestBuildFillSeriesPadsGaps(t *testing.T) {
	repo := &fakeDashRepo{
		series: []domain.UploadPoint{
			{Date: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), Count: 2},
			{Date: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC), Count: 1},
		},
	}
	svc := newTestSvc(repo, nil)
	view, err := svc.Build(context.Background(), 1, TrendWindow7)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The window 09-09..09-15 spans 7 days, one item per day
	if len(view.Series) != 7 {
		t.Fatalf("序列长度 = %d, want 7", len(view.Series))
	}
	byDate := map[string]int{}
	for _, d := range view.Series {
		byDate[d.Date.Format("2006-01-02")] = d.Count
	}
	// Missing days must be filled with 0 rather than dropped from the series —
	// a continuous line is needed to see persistence
	if byDate["2026-09-09"] != 0 || byDate["2026-09-11"] != 0 || byDate["2026-09-15"] != 0 {
		t.Errorf("缺失日未补 0: %+v", byDate)
	}
	if byDate["2026-09-10"] != 2 || byDate["2026-09-13"] != 1 {
		t.Errorf("有数据的日被改写: %+v", byDate)
	}
}

func TestBuildEmptySeriesStillHasFullWindow(t *testing.T) {
	repo := &fakeDashRepo{} // nothing passed at all
	svc := newTestSvc(repo, nil)
	view, err := svc.Build(context.Background(), 1, TrendWindow30)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// An empty array would leave the line chart without a time axis; 30 zeros must be produced
	if len(view.Series) != 30 {
		t.Fatalf("序列长度 = %d, want 30", len(view.Series))
	}
	for _, d := range view.Series {
		if d.Count != 0 {
			t.Fatalf("桶 %s = %d, want 0", d.Date, d.Count)
		}
	}
}

func TestWithPercent(t *testing.T) {
	items := []domain.SubjectDistribution{
		{SubjectID: 1, Name: "数学", Count: 2},
		{SubjectID: 2, Name: "语文", Count: 1},
		{SubjectID: 3, Name: "英语", Count: 1},
	}
	got := withPercent(items)
	if got[0].Percent != 50.0 {
		t.Errorf("数学 = %v%%, want 50", got[0].Percent)
	}
	if got[1].Percent != 25.0 || got[2].Percent != 25.0 {
		t.Errorf("语文/英语 = %v/%v, want 25/25", got[1].Percent, got[2].Percent)
	}
}

func TestWithPercentAllZeroIsNotNaN(t *testing.T) {
	// When all are 0, give 0 rather than NaN: NaN passed into Chart.js makes the whole
	// canvas fail to render.
	got := withPercent([]domain.SubjectDistribution{{SubjectID: 1, Name: "数学", Count: 0}})
	if got[0].Percent != 0 {
		t.Errorf("Percent = %v, want 0", got[0].Percent)
	}
}

func TestBuildRejectsNonPositiveKidID(t *testing.T) {
	svc := newTestSvc(&fakeDashRepo{}, nil)
	for _, id := range []int64{0, -1} {
		if _, err := svc.Build(context.Background(), id, TrendWindow7); !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("kidID=%d: err = %v, want ErrInvalidInput", id, err)
		}
	}
}

func TestLoadKidForDashboard(t *testing.T) {
	active := &domain.Kid{ID: 1, Name: "甲", Grade: 8, Status: domain.KidStatusActive}
	svc := newTestSvc(&fakeDashRepo{}, active)
	kid, err := svc.LoadKidForDashboard(context.Background(), 1)
	if err != nil {
		t.Fatalf("active 应放行: %v", err)
	}
	if kid.Name != "甲" {
		t.Errorf("Name = %s, want 甲", kid.Name)
	}

	// graduated / deleted kids' data is still in the DB, but it must not appear
	// on the parent dashboard
	for _, status := range []int{domain.KidStatusGraduated, domain.KidStatusDeleted} {
		svc := newTestSvc(&fakeDashRepo{}, &domain.Kid{ID: 1, Status: status})
		if _, err := svc.LoadKidForDashboard(context.Background(), 1); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("status=%d: err = %v, want ErrNotFound", status, err)
		}
	}

	svc2 := NewDashboardService(&fakeDashRepo{}, &fakeKidRepo{err: domain.ErrNotFound}, fixedNow)
	if _, err := svc2.LoadKidForDashboard(context.Background(), 9); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("不存在: err = %v, want ErrNotFound", err)
	}
}

func TestBuildPropagatesRepoError(t *testing.T) {
	boom := errors.New("db down")
	svc := newTestSvc(&fakeDashRepo{err: boom}, nil)
	if _, err := svc.Build(context.Background(), 1, TrendWindow7); !errors.Is(err, boom) {
		t.Errorf("err = %v, want 包装后的 boom", err)
	}
}

func TestDistributionNilKidUsesAllKidsAggregate(t *testing.T) {
	repo := &fakeDashRepo{
		dist:    []domain.SubjectDistribution{{SubjectID: 1, Name: "数学", Count: 1}},
		distAll: []domain.SubjectDistribution{{SubjectID: 1, Name: "数学", Count: 3}, {SubjectID: 2, Name: "语文", Count: 1}},
	}
	svc := newTestSvc(repo, nil)

	// kidID=nil → all-kids aggregate, and no kid should be validated
	got, err := svc.Distribution(context.Background(), nil)
	if err != nil {
		t.Fatalf("Distribution(nil): %v", err)
	}
	if len(got) != 2 || got[0].Count != 3 || got[0].Percent != 75 {
		t.Errorf("got = %+v, want 全部聚合且数学占 75%%", got)
	}
}

func TestDistributionKidUsesKidScopedAndValidates(t *testing.T) {
	active := &domain.Kid{ID: 1, Name: "甲", Status: domain.KidStatusActive}
	repo := &fakeDashRepo{
		dist:    []domain.SubjectDistribution{{SubjectID: 1, Name: "数学", Count: 2}},
		distAll: []domain.SubjectDistribution{{SubjectID: 1, Name: "数学", Count: 9}},
	}
	svc := newTestSvc(repo, active)

	kidID := int64(1)
	got, err := svc.Distribution(context.Background(), &kidID)
	if err != nil {
		t.Fatalf("Distribution(kid): %v", err)
	}
	if len(got) != 1 || got[0].Count != 2 {
		t.Errorf("got = %+v, want 该孩子收窄（Count=2）", got)
	}

	// Graduated kid: data is still in the DB, but the distribution card must not let it through
	grad := &domain.Kid{ID: 1, Status: domain.KidStatusGraduated}
	svcGrad := newTestSvc(&fakeDashRepo{}, grad)
	if _, err := svcGrad.Distribution(context.Background(), &kidID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("graduated: err = %v, want ErrNotFound", err)
	}
}

func TestScopeCountsNilKidAggregatesAll(t *testing.T) {
	repo := &fakeDashRepo{
		counts: domain.ScopeCounts{ExamTotal: 120, ExamThisMonth: 30, ExamLastMonth: 28, ExamPages: 400},
	}
	svc := newTestSvc(repo, nil)

	got, err := svc.ScopeCounts(context.Background(), nil)
	if err != nil {
		t.Fatalf("ScopeCounts(nil): %v", err)
	}
	if got.ExamTotal != 120 || got.ExamThisMonth != 30 || got.ExamLastMonth != 28 || got.ExamPages != 400 {
		t.Errorf("got = %+v, want 全部聚合 120/30/28/400", got)
	}
	// With nil no kid should be validated, and the repo must receive a nil scope (no kid filter).
	if repo.gotKid != nil {
		t.Errorf("repo kidID = %v, want nil（全部口径）", repo.gotKid)
	}
}

func TestScopeCountsKidUsesKidScopeAndValidates(t *testing.T) {
	active := &domain.Kid{ID: 1, Name: "甲", Status: domain.KidStatusActive}
	repo := &fakeDashRepo{
		counts: domain.ScopeCounts{ExamTotal: 3, ExamThisMonth: 1, ExamLastMonth: 0, ExamPages: 7},
	}
	svc := newTestSvc(repo, active)

	kidID := int64(1)
	got, err := svc.ScopeCounts(context.Background(), &kidID)
	if err != nil {
		t.Fatalf("ScopeCounts(kid): %v", err)
	}
	if got.ExamTotal != 3 || got.ExamThisMonth != 1 || got.ExamPages != 7 {
		t.Errorf("got = %+v, want 该孩子 3/1/7", got)
	}
	if repo.gotKid == nil || *repo.gotKid != 1 {
		t.Errorf("repo kidID = %v, want 1", repo.gotKid)
	}

	// Graduated kid: data is still in the DB, but the counts card must not let it through.
	grad := &domain.Kid{ID: 1, Status: domain.KidStatusGraduated}
	svcGrad := newTestSvc(&fakeDashRepo{}, grad)
	if _, err := svcGrad.ScopeCounts(context.Background(), &kidID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("graduated: err = %v, want ErrNotFound", err)
	}
}

func TestScopeCountsPropagatesRepoError(t *testing.T) {
	svc := newTestSvc(&fakeDashRepo{err: errors.New("boom")}, nil)
	if _, err := svc.ScopeCounts(context.Background(), nil); err == nil {
		t.Fatal("ScopeCounts 应把仓储错误向上传")
	}
}

func TestPersistenceForKidsReturnsAllActiveKidsWithFilledSeries(t *testing.T) {
	repo := &fakeDashRepo{
		kids: []domain.KidUploadSeries{
			{KidID: 3, Points: []domain.UploadPoint{
				{Date: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), Count: 2},
			}},
			{KidID: 4, Points: []domain.UploadPoint{
				{Date: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), Count: 1},
				{Date: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC), Count: 3},
			}},
		},
	}
	kids := []*domain.Kid{
		{ID: 3, Name: "小明", Status: domain.KidStatusActive},
		{ID: 4, Name: "小红", Status: domain.KidStatusActive},
	}
	svc := NewDashboardService(repo, &fakeKidRepo{kids: kids}, fixedNow)

	views, err := svc.PersistenceForKids(context.Background(), TrendWindow7)
	if err != nil {
		t.Fatalf("PersistenceForKids: %v", err)
	}

	// Only the "active kids" ids are fetched from the repo, in the same order they were returned.
	if len(repo.gotKidIDs) != 2 || repo.gotKidIDs[0] != 3 || repo.gotKidIDs[1] != 4 {
		t.Errorf("kidIDs = %v, want [3 4]", repo.gotKidIDs)
	}

	// Both kids are returned, with names resolved from the kids list.
	if len(views) != 2 {
		t.Fatalf("视图数 = %d, want 2", len(views))
	}
	if views[0].KidName != "小明" || views[1].KidName != "小红" {
		t.Errorf("名字 = %q/%q, want 小明/小红", views[0].KidName, views[1].KidName)
	}

	// Every series is zero-filled to 7 days per the window.
	for i, v := range views {
		if len(v.Series) != 7 {
			t.Fatalf("第 %d 个序列长度 = %d, want 7", i, len(v.Series))
		}
	}
	byDate := map[string]int{}
	for _, d := range views[0].Series {
		byDate[d.Date.Format("2006-01-02")] = d.Count
	}
	if byDate["2026-09-12"] != 2 || byDate["2026-09-09"] != 0 {
		t.Errorf("小明序列 = %+v, want 9-12=2 其余补 0", byDate)
	}
}

func TestPersistenceForKidsIncludesActiveKidWithZeroUploads(t *testing.T) {
	// BUG regression: the repo's GROUP BY only returns "kids with archives" — an active kid
	// with zero archives gets no rows at all (mirrors real UploadSeriesForKids behavior).
	// If the service iterated the repo results, kid B would disappear entirely from the
	// legend — the parent could not see "this kid did not submit this week".
	// So the full kids table must be the source of truth: B is still returned with an all-zero series.
	kids := []*domain.Kid{
		{ID: 1, Name: "甲", Status: domain.KidStatusActive},
		{ID: 2, Name: "乙", Status: domain.KidStatusActive},
	}
	repo := &fakeDashRepo{
		kids: []domain.KidUploadSeries{
			{KidID: 1, Points: []domain.UploadPoint{
				{Date: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), Count: 2},
			}},
		},
	}
	svc := NewDashboardService(repo, &fakeKidRepo{kids: kids}, fixedNow)

	views, err := svc.PersistenceForKids(context.Background(), TrendWindow7)
	if err != nil {
		t.Fatalf("PersistenceForKids: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("视图数 = %d, want 2（甲有数据、乙零归档都要在）", len(views))
	}
	if views[1].KidID != 2 || views[1].KidName != "乙" {
		t.Errorf("第 2 个视图 = %+v, want 乙", views[1])
	}
	if len(views[0].Series) != 7 || len(views[1].Series) != 7 {
		t.Errorf("两条序列都应补满窗口：甲 %d 天 / 乙 %d 天", len(views[0].Series), len(views[1].Series))
	}
	for _, d := range views[1].Series {
		if d.Count != 0 {
			t.Errorf("乙序列 = %+v, want 全 0", views[1].Series)
			break
		}
	}
}

func TestPersistenceForKidsFiltersToActiveOnly(t *testing.T) {
	// Graduated / deleted kids must not enter persistence — mixing them in would add
	// permanently flat lines to the chart.
	kids := []*domain.Kid{
		{ID: 1, Name: "甲", Status: domain.KidStatusActive},
		{ID: 2, Name: "乙", Status: domain.KidStatusGraduated},
		{ID: 3, Name: "丙", Status: domain.KidStatusDeleted},
	}
	repo := &fakeDashRepo{kids: []domain.KidUploadSeries{{KidID: 1}}}
	svc := NewDashboardService(repo, &fakeKidRepo{kids: kids}, fixedNow)

	views, err := svc.PersistenceForKids(context.Background(), TrendWindow7)
	if err != nil {
		t.Fatalf("PersistenceForKids: %v", err)
	}
	if len(repo.gotKidIDs) != 1 || repo.gotKidIDs[0] != 1 {
		t.Errorf("kidIDs = %v, want 只含 active 的 [1]", repo.gotKidIDs)
	}
	if len(views) != 1 || views[0].KidName != "甲" {
		t.Errorf("视图 = %+v, want 只有甲", views)
	}
}

func TestPersistenceForKidsNoActiveKidsReturnsEmpty(t *testing.T) {
	svc := NewDashboardService(&fakeDashRepo{}, &fakeKidRepo{}, fixedNow)
	views, err := svc.PersistenceForKids(context.Background(), TrendWindow7)
	if err != nil {
		t.Fatalf("PersistenceForKids: %v", err)
	}
	if views == nil || len(views) != 0 {
		t.Fatalf("无活孩子时 = %+v, want 非 nil 空切片", views)
	}
}

func TestPersistenceForKidsInvalidWindowFallsBackTo7(t *testing.T) {
	repo := &fakeDashRepo{}
	kids := []*domain.Kid{{ID: 1, Name: "甲", Status: domain.KidStatusActive}}
	svc := NewDashboardService(repo, &fakeKidRepo{kids: kids}, fixedNow)

	if _, err := svc.PersistenceForKids(context.Background(), 999); err != nil {
		t.Fatalf("PersistenceForKids: %v", err)
	}
	if got := repo.gotWindow.From.Format("2006-01-02"); got != "2026-09-09" {
		t.Errorf("From = %s, want 2026-09-09（非法窗口收敛到 7 天）", got)
	}
}

func (m *fakeDashRepo) WithTx(tx domain.Tx) domain.KidDashboardRepository { return m }

func (m *fakeKidRepo) WithTx(tx domain.Tx) domain.KidRepository { return m }
