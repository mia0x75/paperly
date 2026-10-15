package api

import (
	"strconv"
	"time"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
)

// List handles GET /api/exams, supporting filtering and cursor pagination.
func (h *ExamHandler) List(c *echo.Context) error {
	// Strict validation of query params: illegal values are always 400, never silently
	// fall back/ignored — otherwise callers think they filtered/paginated while the params
	// were quietly dropped. Only defaults (empty string) use the default.
	grade, err := optGradeQuery(c)
	if err != nil {
		return fail(c, CodeValidation, err.Error(), c.QueryParam("grade"))
	}
	examType, err := optExamTypeQuery(c)
	if err != nil {
		return fail(c, CodeValidation, err.Error(), c.QueryParam("exam_type"))
	}
	status, err := optStatusQuery(c)
	if err != nil {
		return fail(c, CodeValidation, err.Error(), c.QueryParam("status"))
	}
	cursor, err := parseCursorQuery(c)
	if err != nil {
		return fail(c, CodeValidation, err.Error(), c.QueryParam("cursor"))
	}
	limit, err := parseLimitQuery(c)
	if err != nil {
		return fail(c, CodeValidation, err.Error(), c.QueryParam("limit"))
	}
	filter := domain.ListExamFilter{
		Grade:    grade,
		ExamType: examType,
		Status:   status,
		Cursor:   cursor,
		Limit:    limit,
	}
	if raw := c.QueryParam("subject_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return fail(c, CodeValidation, "invalid subject_id", raw)
		}
		filter.SubjectID = &id
	}
	aud := httpmw.AudienceFromCtx(c)
	if aud == httpmw.AudienceKid {
		kidID := httpmw.KidIDFromCtx(c)
		if kidID > 0 {
			filter.KidID = &kidID
		}
	} else if raw := c.QueryParam("kid_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return fail(c, CodeValidation, "invalid kid_id", raw)
		}
		filter.KidID = &id
	}
	// exam_date closed-interval filter (the date range of the home-page history exam table).
	// An illegal format errors out — treated the same as subject_id/kid_id: silently ignoring
	// would make the user think they filtered when they didn't.
	if raw := c.QueryParam("date_from"); raw != "" {
		t, err := time.Parse(dateQueryLayout, raw)
		if err != nil {
			return fail(c, CodeValidation, "invalid date_from", raw)
		}
		filter.DateFrom = &t
	}
	if raw := c.QueryParam("date_to"); raw != "" {
		t, err := time.Parse(dateQueryLayout, raw)
		if err != nil {
			return fail(c, CodeValidation, "invalid date_to", raw)
		}
		filter.DateTo = &t
	}

	exams, nextCursor, err := h.svc.List(c.Request().Context(), filter)
	if err != nil {
		return mapExamError(c, err)
	}

	items := make([]dto.ExamSummary, 0, len(exams))
	for _, p := range exams {
		items = append(items, dto.NewExamSummary(p))
	}
	return ok(c, dto.ExamListResponse{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    nextCursor != 0,
	})
}
