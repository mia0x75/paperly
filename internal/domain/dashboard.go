package domain

import (
	"context"
	"time"
)

// tasks.status values.
const (
	TaskStatusPending   = 0
	TaskStatusRunning   = 1
	TaskStatusDone      = 2
	TaskStatusFailed    = 3
	TaskStatusSkipped   = 4
	TaskStatusCancelled = 5
)

// ScopeCounts is the archive-side count for one scope (a kid, or all).
type ScopeCounts struct {
	// ExamTotal is the archived exam count (soft-deleted excluded).
	ExamTotal int
	// ExamThisMonth is the count added this month.
	ExamThisMonth int
	// ExamLastMonth is the same for last month. The "vs. last month" ratio is composed upstream —
	// the service does not precompute the percentage (a 0 baseline would divide by zero).
	ExamLastMonth int
	// ExamPages is the total scanned-page count.
	ExamPages int
}

// DashboardWindow is the full set of time definitions for one dashboard build.
//
// The service computes it from the calendar and passes it down: SQL must not depend on the current
// time ("this month"/"last month"), or the window cannot be fixed in tests.
type DashboardWindow struct {
	// From/To is the upload-persistence closed interval; To is the last day at 00:00:00.
	From, To time.Time
	// ThisMonthFrom is the start of this month (UTC midnight).
	ThisMonthFrom time.Time
	// LastMonthFrom/LastMonthTo is the last-month closed interval.
	LastMonthFrom, LastMonthTo time.Time
}

// UploadPoint is one date bucket of the "upload persistence" line/heatmap.
type UploadPoint struct {
	Date  time.Time `db:"date"`
	Count int       `db:"cnt"`
}

// KidUploadSeries is one kid's sparse per-day upload series within the window (no zero filling).
type KidUploadSeries struct {
	KidID  int64
	Points []UploadPoint
}

// SubjectDistribution is one row of the "subject exam distribution" donut.
type SubjectDistribution struct {
	SubjectID int64  `db:"subject_id"`
	Name      string `db:"name"`
	Count     int    `db:"cnt"`
}

// KidDashboardRepository is the data-access contract for home aggregates.
//
// Boundaries differ per group:
//   - scoped counts (ScopeCounts): one scope at a time (all kids or one kid); never both at once.
//   - kid-narrowed (UploadSeriesForKid, SubjectDistributionForKid): that kid alone.
//   - aggregatable (SubjectDistributionAll): the parent home "exam distribution" card defaults to
//     all kids, so an All variant exists. No other kid-narrowed method gets one — it would tempt the
//     UI to show all-kid data on a single kid's page. The global exam list belongs to the exam-list
//     page, not the dashboard.
type KidDashboardRepository interface {
	// ScopeCounts returns archive counts for one scope: kidID == nil aggregates all kids; a non-nil
	// kidID narrows to that kid.
	ScopeCounts(ctx context.Context, kidID *int64, w DashboardWindow) (ScopeCounts, error)

	// UploadSeriesForKid returns the kid's per-day archive count within the window, only dates with
	// data (zero filling is a display concern, done by the service from the window).
	UploadSeriesForKid(ctx context.Context, kidID int64, w DashboardWindow) ([]UploadPoint, error)

	// UploadSeriesForKids fetches several kids' per-day counts in one call (no zero filling), one
	// group per kid, for the parent "kids' persistence" overlay. Kids without data are omitted.
	UploadSeriesForKids(ctx context.Context, kidIDs []int64, w DashboardWindow) ([]KidUploadSeries, error)

	// SubjectDistributionForKid returns the kid's subject distribution (only subjects with archives).
	SubjectDistributionForKid(ctx context.Context, kidID int64) ([]SubjectDistribution, error)

	// SubjectDistributionAll returns the all-kids aggregated distribution (not filtered by kid_id),
	// for the default "all" view. Matches SubjectDistributionForKid: only subjects with archives,
	// soft-deleted exams excluded.
	SubjectDistributionAll(ctx context.Context) ([]SubjectDistribution, error)
	// WithTx returns a new instance bound to tx.
	WithTx(tx Tx) KidDashboardRepository
}
