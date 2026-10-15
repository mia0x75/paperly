package dto

import (
	"paperly/internal/domain"
	"paperly/internal/service"
)

// DashboardResponse is the API contract for the home-page aggregation.
//
// **Shared between the parent and kid sides**: the block structure is identical item by item.
// Every count is single-scope — the caller picks the scope at request time — so there is no paired
// kid/all payload; splitting into two DTOs would make inconsistent numbers between the two sides a
// question of when, not if.
type DashboardResponse struct {
	// Exams archived-exam counts: total (ROW1 card 1) + this-month new (card 2) +
	// last-month (period-over-period base).
	Exams ExamCountsSummary `json:"exams"`
	// ExamPages total digitized pages card.
	ExamPages int `json:"exam_pages"`
	// Series upload persistence (kid-scoped), one entry per day in the window,
	// length always equals the window size.
	Series []DailyUploadSummary `json:"series"`
	// Distribution exam distribution by subject (kid-scoped).
	Distribution []SubjectShareSummary `json:"distribution"`
	// Kid whose numbers these are (Summary view: id + business identifier name + status).
	Kid KidRefSummary `json:"kid"`
}

// ExamCountsSummary is the three time bases of archived exams.
type ExamCountsSummary struct {
	// Total archived exams (card 1 main number).
	Total int `json:"total"`
	// ThisMonth new this month (card 2 main number).
	ThisMonth int `json:"this_month"`
	// LastMonth the same measure for last month (the base of card 1's "compared to last month +12%").
	//
	// Provided as-is rather than letting the frontend subtract: the "compared to last month"
	// denominator is **all of last month**, not "total - this month" (the total still includes exams
	// from earlier months), so a frontend computation would necessarily be wrong.
	LastMonth int `json:"last_month"`
}

// KidRefSummary is the kid reference carried in the dashboard.
// Excludes access_code / pin_hash — credentials never appear in any response body.
type KidRefSummary struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Grade  int    `json:"grade"`
	Status int    `json:"status"`
}

// DailyUploadSummary is one date bucket of upload persistence.
type DailyUploadSummary struct {
	// Date "YYYY-MM-DD" (UTC).
	Date string `json:"date"`
	// Count exams archived that day.
	Count int `json:"count"`
}

// PersistenceItemSummary is one kid's line series in "kids' persistence".
type PersistenceItemSummary struct {
	// KidID the owning kid id.
	KidID int64 `json:"kid_id"`
	// Name the kid's name (business identifier, used for the legend and x-axis tooltips).
	Name string `json:"name"`
	// Series one entry per day in the window, length always equals the window
	// size (missing days filled with 0).
	Series []DailyUploadSummary `json:"series"`
}

// PersistenceResponse is the "kids' persistence" response (parent side).
type PersistenceResponse struct {
	Items []PersistenceItemSummary `json:"items"`
}

// SubjectShareSummary is one item of the subject distribution.
type SubjectShareSummary struct {
	SubjectID int64   `json:"subject_id"`
	Name      string  `json:"name"`
	Count     int     `json:"count"`
	Percent   float64 `json:"percent"`
}

// DistributionResponse is the "exam distribution" card's response
// (GET /api/dashboard/distribution).
//
// Separated from DashboardResponse: this card can be filtered by kid
// independently (omitting kid_id = all kids),
// so the whole home-page aggregation need not be refetched just to change the filter scope.
type DistributionResponse struct {
	Items []SubjectShareSummary `json:"items"`
}

// ScopeCountsResponse is the single-scope count for the top-row KPI cards
// (GET /api/dashboard/counts).
//
// This endpoint serves card-level "all / one kid" filtering and answers one scope at a time, so all
// fields are single-valued without a kid/all split.
type ScopeCountsResponse struct {
	// ExamTotal total archived exams (card 1 main number).
	ExamTotal int `json:"exam_total"`
	// ExamThisMonth new this month (card 2 main number).
	ExamThisMonth int `json:"exam_this_month"`
	// ExamLastMonth the same measure for last month (base of card 2's "compared to last month +12%").
	ExamLastMonth int `json:"exam_last_month"`
	// ExamPages total digitized pages (card 3 main number).
	ExamPages int `json:"exam_pages"`
}

func NewScopeCountsResponse(c domain.ScopeCounts) ScopeCountsResponse {
	return ScopeCountsResponse{
		ExamTotal:     c.ExamTotal,
		ExamThisMonth: c.ExamThisMonth,
		ExamLastMonth: c.ExamLastMonth,
		ExamPages:     c.ExamPages,
	}
}

func NewDashboardResponse(v *service.DashboardView, kid KidRefSummary) DashboardResponse {
	return DashboardResponse{
		Exams: ExamCountsSummary{
			Total:     v.Counts.ExamTotal,
			ThisMonth: v.Counts.ExamThisMonth,
			LastMonth: v.Counts.ExamLastMonth,
		},
		ExamPages:    v.Counts.ExamPages,
		Series:       NewDailyUploadSummarys(v.Series),
		Distribution: NewSubjectShareSummarys(v.Distribution),
		Kid:          kid,
	}
}

func NewDailyUploadSummarys(days []service.UploadDay) []DailyUploadSummary {
	out := make([]DailyUploadSummary, 0, len(days))
	for _, d := range days {
		out = append(out, DailyUploadSummary{Date: d.Date.UTC().Format("2006-01-02"), Count: d.Count})
	}
	return out
}

func NewPersistenceResponse(items []service.KidPersistenceView) PersistenceResponse {
	out := make([]PersistenceItemSummary, 0, len(items))
	for _, it := range items {
		out = append(out, PersistenceItemSummary{
			KidID:  it.KidID,
			Name:   it.KidName,
			Series: NewDailyUploadSummarys(it.Series),
		})
	}
	return PersistenceResponse{Items: out}
}

func NewSubjectShareSummarys(items []service.SubjectShareItem) []SubjectShareSummary {
	out := make([]SubjectShareSummary, 0, len(items))
	for _, it := range items {
		out = append(out, SubjectShareSummary{
			SubjectID: it.SubjectID,
			Name:      it.Name,
			Count:     it.Count,
			Percent:   it.Percent,
		})
	}
	return out
}

func NewDistributionResponse(items []service.SubjectShareItem) DistributionResponse {
	return DistributionResponse{Items: NewSubjectShareSummarys(items)}
}
