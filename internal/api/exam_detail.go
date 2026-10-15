package api

import (
	"strconv"

	"github.com/labstack/echo/v5"
	"net/http"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/service"
)

// PageFile handles GET /api/pages/:page_id/file: returns the raw page-image bytes.
//
// Returns the raw byte stream directly, without the envelope (an image is not JSON).
// The frontend consumes it via <img :src>.
func (h *ExamHandler) PageFile(c *echo.Context) error {
	raw := c.Param("page_id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return fail(c, CodeValidation, "invalid page id", raw)
	}
	res, err := h.svc.GetPageFile(c.Request().Context(), id)
	if err != nil {
		return mapExamError(c, err)
	}
	// Kid-audience ownership check: page images must be matched against the exam's
	// kid_id (see API.md), same scope as Get: not one's own exam → 404, without
	// distinguishing "absent" from "unauthorized".
	if httpmw.AudienceFromCtx(c) == httpmw.AudienceKid {
		kidID := httpmw.KidIDFromCtx(c)
		exam, err := h.svc.Get(c.Request().Context(), res.ExamID)
		if err != nil {
			return mapExamError(c, err)
		}
		if exam.KidID == nil || *exam.KidID != kidID {
			return mapExamError(c, domain.ErrNotFound)
		}
	}
	return c.Blob(http.StatusOK, res.ContentType, res.Data)
}

// Get handles GET /api/exams/:id (with pages).
func (h *ExamHandler) Get(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid exam id", err.Error())
	}
	exam, pages, err := h.svc.GetDetail(c.Request().Context(), id)
	if err != nil {
		return mapExamError(c, err)
	}
	aud := httpmw.AudienceFromCtx(c)
	if aud == httpmw.AudienceKid {
		kidID := httpmw.KidIDFromCtx(c)
		if exam.KidID == nil || *exam.KidID != kidID {
			return mapExamError(c, domain.ErrNotFound)
		}
	}
	return ok(c, dto.NewExamDetail(exam, pages))
}

// Update handles PUT /api/exams/:id (metadata, full PUT semantics).
func (h *ExamHandler) Update(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid exam id", err.Error())
	}
	var req struct {
		Title         *string `json:"title"`
		SubjectID     *int64  `json:"subject_id"`
		Grade         *int    `json:"grade"`
		ExamType      *int    `json:"exam_type"`
		ExamDate      *string `json:"exam_date"`
		KidID         *int64  `json:"kid_id"`
		Summary       *string `json:"summary"`
		SummarySource *int    `json:"summary_source"`
	}
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}

	aud := httpmw.AudienceFromCtx(c)
	if aud == httpmw.AudienceKid {
		kidID := httpmw.KidIDFromCtx(c)
		// Force-keep kid_id
		if req.KidID != nil && *req.KidID != kidID {
			req.KidID = &kidID
		}
		if old, _, e := h.svc.GetDetail(c.Request().Context(), id); e == nil {
			if old.KidID == nil || *old.KidID != kidID {
				return mapExamError(c, domain.ErrNotFound)
			}
		}
	}
	exam, err := h.svc.Update(c.Request().Context(), id, service.UpdateExamReq{
		Title:         req.Title,
		SubjectID:     req.SubjectID,
		Grade:         req.Grade,
		ExamType:      req.ExamType,
		ExamDate:      req.ExamDate,
		KidID:         req.KidID,
		Summary:       req.Summary,
		SummarySource: req.SummarySource,
	})
	if err != nil {
		return mapExamError(c, err)
	}
	return ok(c, dto.NewExamDetail(exam, nil))
}

// Delete handles DELETE /api/exams/:id (soft delete).
func (h *ExamHandler) Delete(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid exam id", err.Error())
	}
	aud := httpmw.AudienceFromCtx(c)
	if aud == httpmw.AudienceKid {
		kidID := httpmw.KidIDFromCtx(c)
		if old, _, e := h.svc.GetDetail(c.Request().Context(), id); e == nil {
			if old.KidID == nil || *old.KidID != kidID {
				return mapExamError(c, domain.ErrNotFound)
			}
		}
	}
	if err := h.svc.Delete(c.Request().Context(), id); err != nil {
		return mapExamError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

// Restore handles POST /api/exams/:id/restore (restores a soft-deleted exam to active).
//
// Ownership scope is the same as Delete: a kid-audience request can only restore
// its own exam, else 404.
func (h *ExamHandler) Restore(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid exam id", err.Error())
	}
	if httpmw.AudienceFromCtx(c) == httpmw.AudienceKid {
		kidID := httpmw.KidIDFromCtx(c)
		old, _, e := h.svc.GetDetail(c.Request().Context(), id)
		if e != nil {
			return mapExamError(c, e)
		}
		if old.KidID == nil || *old.KidID != kidID {
			return mapExamError(c, domain.ErrNotFound)
		}
	}
	exam, err := h.svc.Restore(c.Request().Context(), id)
	if err != nil {
		return mapExamError(c, err)
	}
	return ok(c, dto.NewExamDetail(exam, nil))
}
