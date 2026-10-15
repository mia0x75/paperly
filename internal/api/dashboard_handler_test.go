package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"

	"paperly/internal/domain"
	"paperly/internal/service"
)

// dashRepoStub is a dashboard repository stub with fixed return values.
type dashRepoStub struct {
	counts  domain.ScopeCounts
	kids    []domain.KidUploadSeries
	allDist []domain.SubjectDistribution
	kidDist []domain.SubjectDistribution
	err     error
}

func (d *dashRepoStub) ScopeCounts(context.Context, *int64, domain.DashboardWindow) (domain.ScopeCounts, error) {
	return d.counts, d.err
}
func (d *dashRepoStub) UploadSeriesForKid(context.Context, int64, domain.DashboardWindow) ([]domain.UploadPoint, error) {
	return []domain.UploadPoint{
		{Date: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), Count: 2},
	}, d.err
}
func (d *dashRepoStub) UploadSeriesForKids(context.Context, []int64, domain.DashboardWindow) ([]domain.KidUploadSeries, error) {
	return d.kids, d.err
}
func (d *dashRepoStub) SubjectDistributionForKid(context.Context, int64) ([]domain.SubjectDistribution, error) {
	return d.kidDist, d.err
}
func (d *dashRepoStub) SubjectDistributionAll(context.Context) ([]domain.SubjectDistribution, error) {
	return d.allDist, d.err
}

func activeKid(id int64, name string) *domain.Kid {
	return &domain.Kid{ID: id, Name: name, Grade: 8, Status: domain.KidStatusActive}
}

// kidsWith builds a KidRepository stub containing the given kids (reuses apiTestRepo
// from kid_handler_test).
func kidsWith(kids ...*domain.Kid) *apiTestRepo {
	r := newAPITestRepo()
	for _, k := range kids {
		r.byID[k.ID] = k
	}
	return r
}

func newDashSvc(repo domain.KidDashboardRepository, kids ...*domain.Kid) *service.DashboardService {
	return service.NewDashboardService(repo, kidsWith(kids...), fixedTestNow)
}

// fixedTestNow pins "today" so trend_days windows are reproducible in tests.
func fixedTestNow() time.Time {
	return time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
}

// newDashServer builds an echo instance with only the dashboard routes mounted.
func newDashServer(svc *service.DashboardService) *echo.Echo {
	e := echo.New()
	g := e.Group("/api")
	NewDashboardHandler(svc).RegisterRoutes(g)
	return e
}

// dashboardEnvelope is the minimal shape of data after unwrapping the response envelope.
type dashboardEnvelope struct {
	Data struct {
		Exams struct {
			Total     int `json:"total"`
			ThisMonth int `json:"this_month"`
			LastMonth int `json:"last_month"`
		} `json:"exams"`
		ExamPages int `json:"exam_pages"`
		Kid       struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"kid"`
	} `json:"data"`
}

func TestDashboardOverviewReturnsKidCounts(t *testing.T) {
	repo := &dashRepoStub{
		counts: domain.ScopeCounts{ExamTotal: 3, ExamThisMonth: 1, ExamPages: 7},
	}
	e := newDashServer(newDashSvc(repo, activeKid(3, "小明")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard?kid_id=3&trend_days=7", nil)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var env dashboardEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if env.Data.Exams.Total != 3 || env.Data.ExamPages != 7 {
		t.Errorf("exams.total/exam_pages = %d/%d, want 3/7", env.Data.Exams.Total, env.Data.ExamPages)
	}
	if env.Data.Kid.Name != "小明" {
		t.Errorf("kid.name = %s, want 小明（必须标明这些数是谁的）", env.Data.Kid.Name)
	}
}

func TestDashboardOverviewRejectsMissingOrBadKidID(t *testing.T) {
	e := newDashServer(newDashSvc(&dashRepoStub{}, activeKid(1, "甲")))

	for _, q := range []string{"", "?kid_id=", "?kid_id=abc", "?kid_id=0", "?kid_id=-3"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("q=%q: status = %d, want 400", q, rec.Code)
		}
	}
}

func TestDashboardOverviewRejectsBadTrendDays(t *testing.T) {
	e := newDashServer(newDashSvc(&dashRepoStub{}, activeKid(1, "甲")))

	// Silently clamping to 7 would make users think the filter means "7 days" and never
	// notice it is ignored.
	for _, q := range []string{"?kid_id=1&trend_days=14", "?kid_id=1&trend_days=abc", "?kid_id=1&trend_days=0"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("q=%q: status = %d, want 400", q, rec.Code)
		}
	}

	for _, q := range []string{"?kid_id=1", "?kid_id=1&trend_days=7", "?kid_id=1&trend_days=30"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard"+q, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("q=%q: status = %d, want 200", q, rec.Code)
		}
	}
}

func TestDashboardOverviewUnknownKidIs404(t *testing.T) {
	grad := &domain.Kid{ID: 1, Name: "甲", Status: domain.KidStatusGraduated}
	e := newDashServer(newDashSvc(&dashRepoStub{}, grad))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard?kid_id=1", nil))

	// A graduated kid's data remains in the DB but must not surface on the parent dashboard
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

func containsAllKey(body string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return false
	}
	var walk func(v any) bool
	walk = func(v any) bool {
		switch t2 := v.(type) {
		case map[string]any:
			for k, val := range t2 {
				if k == "all" {
					return true
				}
				if walk(val) {
					return true
				}
			}
		case []any:
			for _, e := range t2 {
				if walk(e) {
					return true
				}
			}
		}
		return false
	}
	var any1 any
	if err := json.Unmarshal([]byte(body), &any1); err != nil {
		return false
	}
	_ = m
	return walk(any1)
}

type persistenceKidItem struct {
	KidID  int64  `json:"kid_id"`
	Name   string `json:"name"`
	Series []struct {
		Date  string `json:"date"`
		Count int    `json:"count"`
	} `json:"series"`
}

// persistenceEnvelope is the minimal shape of /api/dashboard/persistence after unwrapping.
type persistenceEnvelope struct {
	Data struct {
		Items []persistenceKidItem `json:"items"`
	} `json:"data"`
}

func TestDashboardPersistenceReturnsAllActiveKids(t *testing.T) {
	repo := &dashRepoStub{
		kids: []domain.KidUploadSeries{
			{KidID: 3, Points: []domain.UploadPoint{
				{Date: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), Count: 2},
			}},
			{KidID: 4, Points: []domain.UploadPoint{
				{Date: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), Count: 1},
			}},
		},
	}
	e := newDashServer(newDashSvc(repo, activeKid(3, "小明"), activeKid(4, "小红")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/persistence?trend_days=7", nil)
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var env persistenceEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// One row per active kid, with business identifier and zero-padded window series.
	// kidsWith stores kids in a map, so List order is random; assertions must look up by id,
	// not position.
	if len(env.Data.Items) != 2 {
		t.Fatalf("items 数 = %d, want 2", len(env.Data.Items))
	}
	var kid3, kid4 *persistenceKidItem
	for i := range env.Data.Items {
		switch env.Data.Items[i].KidID {
		case 3:
			kid3 = &env.Data.Items[i]
		case 4:
			kid4 = &env.Data.Items[i]
		}
	}
	if kid3 == nil || kid3.Name != "小明" {
		t.Errorf("缺 kid 3 / 小明，got %+v", env.Data.Items)
	}
	if kid4 == nil || kid4.Name != "小红" {
		t.Errorf("缺 kid 4 / 小红，got %+v", env.Data.Items)
	}
	if kid3 != nil && len(kid3.Series) != 7 {
		t.Errorf("kid3 series 长 = %d, want 7（窗口补零）", len(kid3.Series))
	}
	// kid_id / name / date are the keys the parent UI uses for legend and filtering;
	// they must be present
	if kid4 != nil && kid4.Series[0].Date == "" {
		t.Errorf("日期与补零失真: %+v", kid4.Series)
	}
}

func TestDashboardPersistenceRejectsBadTrendDays(t *testing.T) {
	e := newDashServer(newDashSvc(&dashRepoStub{}, activeKid(1, "甲")))

	for _, q := range []string{"?trend_days=14", "?trend_days=abc", "?trend_days=0"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/persistence"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("q=%q: status = %d, want 400", q, rec.Code)
		}
	}
	for _, q := range []string{"", "?trend_days=7", "?trend_days=30"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/persistence"+q, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("q=%q: status = %d, want 200", q, rec.Code)
		}
	}
}

func TestDashboardPersistenceEmptyItemsIsNotNull(t *testing.T) {
	// With no active kids, items must be [] not null — the frontend forEach would throw on null.
	// One active kid with zero archives does NOT count as empty (it gets a full all-zero series
	// in the legend; see TestPersistenceForKidsIncludesActiveKidWithZeroUploads).
	e := newDashServer(newDashSvc(&dashRepoStub{}))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/persistence", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("items 应为空数组，body=%s", rec.Body.String())
	}
}

func TestDashboardPersistenceZeroUploadKidHasZeroSeries(t *testing.T) {
	// Regression: the legend must not drop zero-archive kids. An active kid (no series at all)
	// must appear with a window-length all-zero series so parents can see "no submissions this week".
	e := newDashServer(newDashSvc(&dashRepoStub{}, activeKid(1, "甲")))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/persistence", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var env persistenceEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Items) != 1 || env.Data.Items[0].KidID != 1 {
		t.Fatalf("items = %+v, want 只含甲（零归档也要在）", env.Data.Items)
	}
	ser := env.Data.Items[0].Series
	if len(ser) != 7 {
		t.Fatalf("甲 series 长 = %d, want 7（窗口补零）", len(ser))
	}
	counts := map[string]int{}
	for _, p := range ser {
		counts[p.Date] = p.Count
	}
	for d := 9; d <= 15; d++ {
		key := fmt.Sprintf("2026-09-%02d", d)
		if counts[key] != 0 {
			t.Errorf("甲 %s = %d, want 全 0", key, counts[key])
		}
	}
}

func TestDashboardExposesThreeExamTimeWindows(t *testing.T) {
	repo := &dashRepoStub{
		counts: domain.ScopeCounts{ExamTotal: 42, ExamThisMonth: 8, ExamLastMonth: 35},
	}
	e := newDashServer(newDashSvc(repo, activeKid(3, "小明")))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard?kid_id=3", nil))

	var env dashboardEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The three windows must be delivered separately: card 1 headline = total, card 2 headline =
	// this month, and card 1's "vs last month +12%" uses last month as denominator.
	// Last month cannot be derived as "total - this month" on the frontend: total includes older months.
	if env.Data.Exams.Total != 42 || env.Data.Exams.ThisMonth != 8 || env.Data.Exams.LastMonth != 35 {
		t.Errorf("三口径 = total:%d thisMonth:%d lastMonth:%d, want 42/8/35",
			env.Data.Exams.Total, env.Data.Exams.ThisMonth, env.Data.Exams.LastMonth)
	}
}

// distributionEnvelope is the minimal shape of /api/dashboard/distribution after unwrapping.
type distributionEnvelope struct {
	Data struct {
		Items []struct {
			SubjectID int64   `json:"subject_id"`
			Name      string  `json:"name"`
			Count     int     `json:"count"`
			Percent   float64 `json:"percent"`
		} `json:"items"`
	} `json:"data"`
}

func TestDashboardDistributionAllKidsWhenKidIDOmitted(t *testing.T) {
	repo := &dashRepoStub{
		allDist: []domain.SubjectDistribution{
			{SubjectID: 1, Name: "数学", Count: 3},
			{SubjectID: 2, Name: "语文", Count: 1},
		},
	}
	e := newDashServer(newDashSvc(repo, activeKid(3, "小明")))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/distribution", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var env distributionEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Items) != 2 {
		t.Fatalf("items = %+v, want 全部孩子聚合的 2 行", env.Data.Items)
	}
	// Percent uses the all-kids total (3+1): math 75 / chinese 25.
	if env.Data.Items[0].Percent != 75 || env.Data.Items[1].Percent != 25 {
		t.Errorf("percent = %v/%v, want 75/25（分母是全部孩子）",
			env.Data.Items[0].Percent, env.Data.Items[1].Percent)
	}
}

func TestDashboardDistributionForKid(t *testing.T) {
	repo := &dashRepoStub{
		kidDist: []domain.SubjectDistribution{{SubjectID: 1, Name: "数学", Count: 2}},
	}
	e := newDashServer(newDashSvc(repo, activeKid(3, "小明")))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/distribution?kid_id=3", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var env distributionEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Items) != 1 || env.Data.Items[0].Name != "数学" || env.Data.Items[0].Percent != 100 {
		t.Errorf("items = %+v, want 单个孩子的数学 100%%", env.Data.Items)
	}
}

func TestDashboardDistributionRejectsBadKidID(t *testing.T) {
	e := newDashServer(newDashSvc(&dashRepoStub{}, activeKid(1, "甲")))

	for _, q := range []string{"?kid_id=abc", "?kid_id=0", "?kid_id=-3"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/distribution"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("q=%q: status = %d, want 400", q, rec.Code)
		}
	}
}

func TestDashboardDistributionUnknownKidIs404(t *testing.T) {
	grad := &domain.Kid{ID: 1, Name: "甲", Status: domain.KidStatusGraduated}
	e := newDashServer(newDashSvc(&dashRepoStub{}, grad))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/distribution?kid_id=1", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404（已毕业孩子不能出现在分布卡）", rec.Code)
	}
}

// scopeCountsEnvelope is the minimal shape of /api/dashboard/counts after unwrapping.
type scopeCountsEnvelope struct {
	Data struct {
		ExamTotal     int `json:"exam_total"`
		ExamThisMonth int `json:"exam_this_month"`
		ExamLastMonth int `json:"exam_last_month"`
		ExamPages     int `json:"exam_pages"`
	} `json:"data"`
}

func TestDashboardScopeCountsAllKidsWhenKidIDOmitted(t *testing.T) {
	repo := &dashRepoStub{
		counts: domain.ScopeCounts{ExamTotal: 120, ExamThisMonth: 30, ExamLastMonth: 28, ExamPages: 400},
	}
	e := newDashServer(newDashSvc(repo, activeKid(3, "小明")))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/counts", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var env scopeCountsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.ExamTotal != 120 || env.Data.ExamThisMonth != 30 ||
		env.Data.ExamLastMonth != 28 || env.Data.ExamPages != 400 {
		t.Errorf("data = %+v, want 全部聚合 120/30/28/400", env.Data)
	}
}

func TestDashboardScopeCountsForKid(t *testing.T) {
	repo := &dashRepoStub{
		counts: domain.ScopeCounts{ExamTotal: 3, ExamThisMonth: 1, ExamLastMonth: 0, ExamPages: 7},
	}
	e := newDashServer(newDashSvc(repo, activeKid(3, "小明")))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/counts?kid_id=3", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var env scopeCountsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.ExamTotal != 3 || env.Data.ExamThisMonth != 1 || env.Data.ExamPages != 7 {
		t.Errorf("data = %+v, want 该孩子 3/1/7", env.Data)
	}
}

func TestDashboardScopeCountsRejectsBadKidID(t *testing.T) {
	e := newDashServer(newDashSvc(&dashRepoStub{}, activeKid(1, "甲")))

	for _, q := range []string{"?kid_id=abc", "?kid_id=0", "?kid_id=-3"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/counts"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("q=%q: status = %d, want 400", q, rec.Code)
		}
	}
}

func TestDashboardScopeCountsUnknownKidIs404(t *testing.T) {
	grad := &domain.Kid{ID: 1, Name: "甲", Status: domain.KidStatusGraduated}
	e := newDashServer(newDashSvc(&dashRepoStub{}, grad))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dashboard/counts?kid_id=1", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404（已毕业孩子不能出现在计数卡）", rec.Code)
	}
}

func (m *dashRepoStub) WithTx(tx domain.Tx) domain.KidDashboardRepository { return m }
