package api

import (
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/service"
)

// AppendSummary handles POST /api/exams/:id/summary: appends one signed rich-text segment.
//
// Body: {"content": "<p>...</p>"}. Returns {sig, summary} where summary is the full
// updated HTML, so the client can re-render without a second GET.
func (h *ExamHandler) AppendSummary(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid exam id", err.Error())
	}
	if err := h.requireExamOwnership(c, id); err != nil {
		return mapExamError(c, err)
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	sig, summary, err := h.svc.AppendSummary(c.Request().Context(), id, h.currentSummaryAuthor(c), req.Content, time.Now())
	if err != nil {
		return mapExamError(c, err)
	}
	return ok(c, dto.SummaryResponse{Sig: sig, Summary: summary})
}

// DeleteSummary handles DELETE /api/exams/:id/summary/:sig: removes one signed segment.
//
// Permission is enforced in the service: parents may delete any segment; a kid may
// only delete their own. Returns {summary} (the full updated HTML).
func (h *ExamHandler) DeleteSummary(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid exam id", err.Error())
	}
	sig := strings.TrimSpace(c.Param("sig"))
	if sig == "" {
		return fail(c, CodeValidation, "missing summary sig", "")
	}
	if err := h.requireExamOwnership(c, id); err != nil {
		return mapExamError(c, err)
	}
	summary, err := h.svc.DeleteSummary(c.Request().Context(), id, sig, h.currentSummaryAuthor(c))
	if err != nil {
		return mapExamError(c, err)
	}
	return ok(c, dto.SummaryResponse{Summary: summary})
}

// requireExamOwnership checks the kid-audience exam ownership rule shared by the
// summary handlers: a kid may only touch its own exam. It returns a domain error
// (to be mapped by the caller) or nil when allowed; parents always pass.
func (h *ExamHandler) requireExamOwnership(c *echo.Context, id int64) error {
	if httpmw.AudienceFromCtx(c) != httpmw.AudienceKid {
		return nil
	}
	kidID := httpmw.KidIDFromCtx(c)
	exam, _, err := h.svc.GetDetail(c.Request().Context(), id)
	if err != nil {
		return err
	}
	if exam.KidID == nil || *exam.KidID != kidID {
		return domain.ErrNotFound
	}
	return nil
}

// currentSummaryAuthor resolves the signed author from the request audience.
//
// The author's type/id come from the verified token context (never from the
// request body — a client must not be able to sign as someone else). The display
// name is a convenience for rendering; when auth is unavailable (tests) it falls
// back to a generic label.
func (h *ExamHandler) currentSummaryAuthor(c *echo.Context) service.SummaryAuthor {
	ctx := c.Request().Context()
	if httpmw.AudienceFromCtx(c) == httpmw.AudienceKid {
		id := httpmw.KidIDFromCtx(c)
		name := "孩子"
		if h.auth != nil {
			if k, err := h.auth.CurrentKid(ctx, id); err == nil {
				name = k.Name
			}
		}
		return service.SummaryAuthor{Type: domain.UploadedByKid, ID: id, Name: name}
	}
	id := httpmw.UserIDFromCtx(c)
	name := "家长"
	if h.auth != nil {
		if u, err := h.auth.CurrentParent(ctx, id); err == nil {
			name = u.DisplayName
		}
	}
	return service.SummaryAuthor{Type: domain.UploadedByParent, ID: id, Name: name}
}
