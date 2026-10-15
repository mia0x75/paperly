package api

import (
	"errors"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	"paperly/internal/service"
)

// KidSubjectHandler handles requests under /api/kids/:id/subjects (the per-kid
// subject whitelist). The path param is the kid id, aligning with the kids table.
type KidSubjectHandler struct {
	svc *service.KidSubjectService
}

// NewKidSubjectHandler constructs the kid×subject HTTP handler.
func NewKidSubjectHandler(svc *service.KidSubjectService) *KidSubjectHandler {
	return &KidSubjectHandler{svc: svc}
}

// RegisterRoutes registers the kid-subject routes on the given group.
func (h *KidSubjectHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/kids/:id/subjects", h.List)
	g.PUT("/kids/:id/subjects", h.Set)
}

// List handles GET /api/kids/:id/subjects: the kid's whitelisted subjects
// (empty list when not configured — the editor shows "not configured").
func (h *KidSubjectHandler) List(c *echo.Context) error {
	kidID, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid kid id", err.Error())
	}
	rows, err := h.svc.List(c.Request().Context(), kidID)
	if err != nil {
		return failKidSubject(c, err)
	}
	return ok(c, dto.NewSubjectSummaries(rows))
}

// setKidSubjectsRequest is the request body for PUT /api/kids/:id/subjects.
type setKidSubjectsRequest struct {
	SubjectIDs []int64 `json:"subject_ids"`
}

// Set handles PUT /api/kids/:id/subjects: replaces the kid's whitelist with the
// posted subject ids (full replacement; an empty list clears it).
func (h *KidSubjectHandler) Set(c *echo.Context) error {
	kidID, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid kid id", err.Error())
	}
	var req setKidSubjectsRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	if err := h.svc.Set(c.Request().Context(), kidID, req.SubjectIDs); err != nil {
		return failKidSubject(c, err)
	}
	return ok(c, nil)
}

// failKidSubject maps kid-subject errors.
func failKidSubject(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fail(c, CodeNotFound, "student not found", err.Error())
	case errors.Is(err, domain.ErrInvalidInput):
		return fail(c, CodeInvalidInput, err.Error(), "")
	default:
		return fail(c, CodeInternal, "kid subject operation failed", err.Error())
	}
}
