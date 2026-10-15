package dto

import (
	"time"

	"paperly/internal/domain"
)

// UploadExamResponse is the response body for POST /api/exams/upload.
type UploadExamResponse struct {
	ID         int64     `json:"id"`
	Title      *string   `json:"title,omitempty"`
	SubjectID  *int64    `json:"subject_id,omitempty"`
	Grade      *int      `json:"grade,omitempty"`
	ExamType   *int      `json:"exam_type,omitempty"`
	UploadedBy int       `json:"uploaded_by"`
	Status     int       `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// PageOpsResponse is the 202 response for POST /api/exams/:id/pages/ops.
// DB page order and paths are already in place; disk moves are done
// asynchronously by the kind=5 task pointed to by task_id.
type PageOpsResponse struct {
	TaskID int64 `json:"task_id"`
	ExamID int64 `json:"exam_id"`
}

// ExamSummary is a GET /api/exams list item.
type ExamSummary struct {
	ID int64 `json:"id"`
	// Title exam title (e.g. "2026-初三-上学期-数学-09月-月考"), manually editable by the parent.
	Title      *string `json:"title,omitempty"`
	SubjectID  *int64  `json:"subject_id,omitempty"`
	Grade      *int    `json:"grade,omitempty"`
	ExamType   *int    `json:"exam_type,omitempty"`
	ExamDate   string  `json:"exam_date,omitempty"`
	KidID      *int64  `json:"kid_id,omitempty"`
	UploadedBy int     `json:"uploaded_by"`
	Status     int     `json:"status"`
	// Summary exam summary (manual, or a future automated producer). The title column is Title.
	Summary *string `json:"summary,omitempty"`
	// PageCount number of digitized pages (the "pages" column in lists).
	PageCount int `json:"page_count"`
}

// PageSummary is the public contract for an exam page.
//
// Internal paths stay server-side (AGENTS.md DTO rules): file_path and hashes
// never leave the backend; clients fetch images via GET /api/pages/:id/file.
type PageSummary struct {
	ID        int64  `json:"id"`
	PageIndex int    `json:"page_index"`
	SHA256    string `json:"sha256"`
}

// ExamDetail is the response body for GET/PUT /api/exams/:id.
//
// Flat struct (no embedding of ExamSummary): embedding plus a redeclared
// Summary produced two "summary" JSON keys. The three views (Summary /
// Detail / Created) must not reuse one struct.
type ExamDetail struct {
	ID            int64         `json:"id"`
	Title         *string       `json:"title,omitempty"`
	SubjectID     *int64        `json:"subject_id,omitempty"`
	Grade         *int          `json:"grade,omitempty"`
	ExamType      *int          `json:"exam_type,omitempty"`
	ExamDate      string        `json:"exam_date,omitempty"`
	KidID         *int64        `json:"kid_id,omitempty"`
	UploadedBy    int           `json:"uploaded_by"`
	Status        int           `json:"status"`
	Summary       string        `json:"summary,omitempty"`
	SummarySource *int          `json:"summary_source,omitempty"`
	PageCount     int           `json:"page_count"`
	Pages         []PageSummary `json:"pages"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// SummaryResponse is the response of POST/DELETE /api/exams/:id/summary[/:sig].
//
// Sig is only present on append (the new segment's identifier); both endpoints
// return the full updated summary so the client can re-render in one round trip.
type SummaryResponse struct {
	Sig     string `json:"sig,omitempty"`
	Summary string `json:"summary"`
}

// ExamListResponse is the paging envelope of the list response.
type ExamListResponse struct {
	Items []ExamSummary `json:"items"`
	// NextCursor next-page cursor; 0 means no more (at which point HasMore is false).
	// The frontend passes it back verbatim to GET /api/exams?cursor=....
	NextCursor int64 `json:"next_cursor"`
	HasMore    bool  `json:"has_more"`
}

func NewUploadExamResponse(p *domain.Exam) UploadExamResponse {
	return UploadExamResponse{
		ID:         p.ID,
		Title:      p.Title,
		SubjectID:  p.SubjectID,
		Grade:      p.Grade,
		ExamType:   p.ExamType,
		UploadedBy: p.UploadedBy,
		Status:     p.Status,
		CreatedAt:  p.CreatedAt,
	}
}

func NewExamSummary(p *domain.Exam) ExamSummary {
	return ExamSummary{
		ID:         p.ID,
		Title:      p.Title,
		SubjectID:  p.SubjectID,
		Grade:      p.Grade,
		ExamType:   p.ExamType,
		ExamDate:   formatPtrDate(p.ExamDate),
		KidID:      p.KidID,
		UploadedBy: p.UploadedBy,
		Status:     p.Status,
		Summary:    p.Summary,
		PageCount:  p.PageCount,
	}
}

func NewExamDetail(p *domain.Exam, pages []*domain.Page) ExamDetail {
	out := ExamDetail{
		ID:            p.ID,
		Title:         p.Title,
		SubjectID:     p.SubjectID,
		Grade:         p.Grade,
		ExamType:      p.ExamType,
		ExamDate:      formatPtrDate(p.ExamDate),
		KidID:         p.KidID,
		UploadedBy:    p.UploadedBy,
		Status:        p.Status,
		SummarySource: p.SummarySource,
		Pages:         NewPageSummaries(pages),
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
	if p.Summary != nil {
		out.Summary = *p.Summary
	}
	// The detail page's page_count uses len(pages), not p.PageCount: GetByID does
	// not run a page-count subquery (the detail already returns pages separately),
	// so p.PageCount is always 0; passing it straight through would show "0 pages"
	// while the images appear below — self-contradictory.
	out.PageCount = len(pages)
	return out
}

// NewPageSummaries builds the page list response (guaranteeing an empty list serializes to []).
func NewPageSummaries(pages []*domain.Page) []PageSummary {
	out := make([]PageSummary, 0, len(pages))
	for _, pg := range pages {
		out = append(out, PageSummary{
			ID:        pg.ID,
			PageIndex: pg.PageIndex,
			SHA256:    pg.SHA256,
		})
	}
	return out
}

// formatPtrDate formats *time.Time as YYYY-MM-DD; nil returns an empty string.
func formatPtrDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}
