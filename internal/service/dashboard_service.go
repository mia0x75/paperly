package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"paperly/internal/domain"
)

// DashboardService builds the homepage aggregate view.
//
// The metrics split into two groups that match the UI blocks:
//   - Top-row KPI cards (ScopeCounts): one scope per request, either all kids (the
//     parent default) or a single kid. The caller picks the scope, and the kid side
//     never receives global counts: an anonymous student has no authorization for
//     global data, and "128 exams system-wide" is both useless and an overreach.
//   - Upload persistence, per-subject recent archives, subject distribution and the
//     exam history list: purely kid-scoped, so parents and kids see the same
//     single-kid view and never a mix.
type DashboardService struct {
	repo domain.KidDashboardRepository
	kids domain.KidRepository
	now  func() time.Time
}

// NewDashboardService constructs the dashboard service. now is injectable so tests can
// fix the window.
func NewDashboardService(repo domain.KidDashboardRepository, kids domain.KidRepository, now func() time.Time) *DashboardService {
	if now == nil {
		now = time.Now
	}
	return &DashboardService{repo: repo, kids: kids, now: now}
}

// DashboardView is the service-layer dashboard aggregate (no JSON tags; serialization
// happens only in the DTO layer).
type DashboardView struct {
	// Counts are the archive KPI counts for the caller-chosen scope.
	Counts domain.ScopeCounts
	// Series is the upload persistence (kid-scoped), zero-filled per day; its length
	// always equals the window size.
	Series []UploadDay
	// Distribution is the subject share (kid-scoped), with Percent rounded to one
	// decimal in 0-100.
	Distribution []SubjectShareItem
}

// UploadDay is one date bucket of the upload persistence series.
type UploadDay struct {
	Date  time.Time
	Count int
}

// KidPersistenceView is one kid's series within "kids' persistence" (parent ROW2).
type KidPersistenceView struct {
	KidID   int64
	KidName string
	Series  []UploadDay
}

// SubjectShareItem is one subject-share item with its computed percentage.
type SubjectShareItem struct {
	SubjectID int64
	Name      string
	Count     int
	Percent   float64
}

// Windows (days) supported for upload persistence. Any other value converges to 7.
const (
	TrendWindow7  = 7
	TrendWindow30 = 30
)

// Build assembles the dashboard view for kidID. The kid side gets only this kid's counts.
//
// Any trendDays other than 7/30 converges to 7: the window is a display scope, so
// prefer the smallest over an unrequested 365 days, which would drag in
// cross-school-year noise.
func (s *DashboardService) Build(ctx context.Context, kidID int64, trendDays int) (*DashboardView, error) {
	if kidID <= 0 {
		return nil, domain.ErrInvalidInput
	}
	if trendDays != TrendWindow7 && trendDays != TrendWindow30 {
		trendDays = TrendWindow7
	}

	w := windowAround(s.now(), trendDays)

	series, err := s.repo.UploadSeriesForKid(ctx, kidID, w)
	if err != nil {
		return nil, err
	}
	dist, err := s.repo.SubjectDistributionForKid(ctx, kidID)
	if err != nil {
		return nil, err
	}

	counts, err := s.repo.ScopeCounts(ctx, &kidID, w)
	if err != nil {
		return nil, err
	}

	return &DashboardView{
		Counts:       counts,
		Series:       fillSeries(series, w.From, w.To),
		Distribution: withPercent(dist),
	}, nil
}

// Distribution returns the subject distribution; a nil kidID aggregates all kids (the
// parent homepage's default scope).
//
// "All" and "a specific kid" are two options of the same card, so they share one entry:
// for a non-nil kidID it first validates the kid is usable (graduated/deleted returns
// ErrNotFound, consistent with the single-kid dashboard scope), then fetches kid-scoped
// data. Both paths reuse the same percentage conversion.
func (s *DashboardService) Distribution(ctx context.Context, kidID *int64) ([]SubjectShareItem, error) {
	var (
		dist []domain.SubjectDistribution
		err  error
	)
	if kidID == nil {
		dist, err = s.repo.SubjectDistributionAll(ctx)
	} else {
		if _, kerr := s.LoadKidForDashboard(ctx, *kidID); kerr != nil {
			return nil, kerr
		}
		dist, err = s.repo.SubjectDistributionForKid(ctx, *kidID)
	}
	if err != nil {
		return nil, err
	}
	return withPercent(dist), nil
}

// ScopeCounts returns the top-row KPI counts for a single scope (a kid, or the all-kids aggregate).
//
// Same trade-off as Distribution: nil kidID = all-kids aggregate (the parent homepage's
// default scope), non-nil = that kid (validated first; graduated/deleted returns ErrNotFound).
func (s *DashboardService) ScopeCounts(ctx context.Context, kidID *int64) (domain.ScopeCounts, error) {
	if kidID != nil {
		if _, err := s.LoadKidForDashboard(ctx, *kidID); err != nil {
			return domain.ScopeCounts{}, err
		}
	}

	// Only the time basis matters for counts (this/last month boundaries); the window
	// size does not affect the result.
	return s.repo.ScopeCounts(ctx, kidID, windowAround(s.now(), TrendWindow7))
}

// windowAround computes all the time bases for this build on the calendar.
//
// Everything is UTC: created_at in SQLite is a timezone-less text written by
// CURRENT_TIMESTAMP (UTC), so cutting "this month" by local time would push archives
// made early morning (UTC+8 users) into last month.
func windowAround(now time.Time, trendDays int) domain.DashboardWindow {
	now = now.UTC()

	to := endOfDayUTC(now)
	from := startOfDayUTC(now.AddDate(0, 0, -(trendDays - 1)))

	thisMonth := startOfDayUTC(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
	lastMonth := startOfDayUTC(thisMonth.AddDate(0, -1, 0))

	return domain.DashboardWindow{
		From:          from,
		To:            to,
		ThisMonthFrom: thisMonth,
		LastMonthFrom: lastMonth,
		// Last month's closed-interval upper bound is one second before this month
		// starts. Add(-1s) rather than AddDate(0,-1,0) matches the repository's <=
		// comparison exactly and never drops the 31st regardless of month length.
		LastMonthTo: thisMonth.Add(-time.Second),
	}
}

// PersistenceForKids returns upload-persistence series for all active kids (parent ROW2).
//
// Unlike Build's kid-scoped basis, every active kid's series is returned stacked so a
// parent can see at a glance who has not submitted lately. Only active kids are listed:
// graduated/deleted kids are outside the current home-school scope and would add
// permanently flat lines to the chart. Active kids with zero archives this week are
// kept, though: they are the flat line at 0, not a missing line.
func (s *DashboardService) PersistenceForKids(ctx context.Context, trendDays int) ([]KidPersistenceView, error) {
	if trendDays != TrendWindow7 && trendDays != TrendWindow30 {
		trendDays = TrendWindow7
	}

	kids, err := s.kids.List(ctx, domain.ListKidsFilter{Status: domain.KidStatusActive})
	if err != nil {
		return nil, fmt.Errorf("list active kids: %w", err)
	}
	if len(kids) == 0 {
		return []KidPersistenceView{}, nil
	}

	ids := make([]int64, 0, len(kids))
	for _, k := range kids {
		ids = append(ids, k.ID)
	}

	w := windowAround(s.now(), trendDays)
	series, err := s.repo.UploadSeriesForKids(ctx, ids, w)
	if err != nil {
		return nil, err
	}

	// The repo's GROUP BY only returns kids with archives, so kids with zero archives
	// get no rows. Use the full kids table and group points by kidID: every active kid
	// needs one series (fillSeries pads empty point sets to all zeros), otherwise the
	// flat line for "which kid did not submit this week" would be missing and the kid
	// would drop out of the line-chart legend.
	byKid := make(map[int64][]domain.UploadPoint, len(series))
	for _, ks := range series {
		byKid[ks.KidID] = ks.Points
	}

	out := make([]KidPersistenceView, 0, len(kids))
	for _, k := range kids {
		out = append(out, KidPersistenceView{
			KidID:   k.ID,
			KidName: k.Name,
			Series:  fillSeries(byKid[k.ID], w.From, w.To),
		})
	}
	return out, nil
}

// LoadKidForDashboard validates that kidID points to a usable student profile and returns it.
//
// The parent side presents this kid to the user, so it must exist and be active.
// Graduated/deleted kids' data still lives in the DB, and passing them through would
// show a parent a dashboard belonging to a graduated kid with no indication.
func (s *DashboardService) LoadKidForDashboard(ctx context.Context, kidID int64) (*domain.Kid, error) {
	kid, err := s.kids.GetByID(ctx, kidID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get kid: %w", err)
	}
	if kid.Status != domain.KidStatusActive {
		return nil, domain.ErrNotFound
	}
	return kid, nil
}

// fillSeries pads sparse SQL results into a continuous date series.
//
// Zero-filling happens here rather than in SQL: SQL only knows dates with data, and
// "which days had no uploads" is a display concern (a continuous line reveals
// persistence) that the storage layer should not decide. Missing buckets are filled
// with 0; empty input yields an all-zero series rather than an empty array, since an
// empty array renders as "no time axis" on the line chart.
func fillSeries(points []domain.UploadPoint, from, to time.Time) []UploadDay {
	counts := make(map[string]int, len(points))
	for _, p := range points {
		counts[p.Date.UTC().Format(dateLayout)] = p.Count
	}

	out := make([]UploadDay, 0, int(to.Sub(from).Hours()/24)+1)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		out = append(out, UploadDay{Date: d, Count: counts[d.UTC().Format(dateLayout)]})
	}
	return out
}

// withPercent computes each subject's percentage of the kid's total exams.
//
// A total of 0 yields 0 for all rather than NaN: NaN handed to Chart.js makes the whole
// canvas fail to render (not a display glitch; the chart simply disappears).
func withPercent(items []domain.SubjectDistribution) []SubjectShareItem {
	total := 0
	for _, it := range items {
		total += it.Count
	}

	out := make([]SubjectShareItem, 0, len(items))
	for _, it := range items {
		var pct float64
		if total > 0 {
			pct = round1(float64(it.Count) / float64(total) * 100)
		}
		out = append(out, SubjectShareItem{
			SubjectID: it.SubjectID,
			Name:      it.Name,
			Count:     it.Count,
			Percent:   pct,
		})
	}
	return out
}

func round1(f float64) float64 {
	return float64(int64(f*10+0.5)) / 10
}

// dateLayout is the series key format, matching the repository-layer convention.
const dateLayout = "2006-01-02"

func startOfDayUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func endOfDayUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.UTC)
}
