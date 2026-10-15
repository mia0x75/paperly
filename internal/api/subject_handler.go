package api

import (
	"errors"
	"strconv"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	"paperly/internal/service"
)

type SubjectHandler struct {
	svc *service.SubjectService
}

func NewSubjectHandler(svc *service.SubjectService) *SubjectHandler {
	return &SubjectHandler{svc: svc}
}

func (h *SubjectHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/subjects", h.List)
	g.POST("/subjects", h.Create)
	g.PUT("/subjects/:id", h.Update)
	g.DELETE("/subjects/:id", h.Delete)
}

// List handles GET /api/subjects.
//
// Optional ?kid_id=N returns exactly the subjects assigned to that kid via
// kid_subjects (no mask fallback; a kid with none configured returns an empty
// list). It takes precedence over ?grade / ?track (which are the raw dictionary
// facets). Optional ?grade=N filters to the subjects visible in that grade's stage
// (primary/junior/senior). Optional ?track=M filters by arts/science (1=arts,
// 2=science); track omitted or 0 does not filter by track. Omitting all — or
// passing non-positive values — returns the full dictionary.
func (h *SubjectHandler) List(c *echo.Context) error {
	if raw := c.QueryParam("kid_id"); raw != "" {
		kidID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || kidID <= 0 {
			return fail(c, CodeValidation, "kid_id must be a positive integer", "kid_id="+raw)
		}
		rows, err := h.svc.ListForKid(c.Request().Context(), kidID)
		if err != nil {
			return failSubject(c, err)
		}
		return ok(c, dto.NewSubjectSummaries(rows))
	}
	grade := 0
	if raw := c.QueryParam("grade"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fail(c, CodeValidation, "invalid grade", err.Error())
		}
		grade = n
	}
	track := 0
	if raw := c.QueryParam("track"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fail(c, CodeValidation, "invalid track", err.Error())
		}
		track = n
	}
	if grade > 0 || track > 0 {
		rows, err := h.svc.ListByGradeTrack(c.Request().Context(), grade, track)
		if err != nil {
			return failSubject(c, err)
		}
		return ok(c, dto.NewSubjectSummaries(rows))
	}
	rows, err := h.svc.List(c.Request().Context())
	if err != nil {
		return failSubject(c, err)
	}
	return ok(c, dto.NewSubjectSummaries(rows))
}

// createSubjectReq is the request body for POST /api/subjects.
type createSubjectReq struct {
	Name string `json:"name"`
}

// Create handles POST /api/subjects (adds a parent-created subject).
//
// Accepts only name, not code / source / sort_order:
// code is derived server-side from the name (see service.deriveSubjectCode),
// source is always user, and sort_order goes to the end — letting the client decide
// these three fields would expose two business rules ("deletable or not", "ranked where")
// as tamperable inputs.
func (h *SubjectHandler) Create(c *echo.Context) error {
	var req createSubjectReq
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	sub, err := h.svc.Create(c.Request().Context(), req.Name)
	if err != nil {
		return failSubject(c, err)
	}
	return ok(c, dto.NewSubjectCreated(sub))
}

// Update handles PUT /api/subjects/:id.
//
// Body fields are optional and independent: name renames, mask edits stage
// visibility. System-preset subjects **may be renamed and re-masked** (the
// design marks them "renamable / 🔒 system built-in"); only deletion is forbidden.
func (h *SubjectHandler) Update(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid subject id", err.Error())
	}
	var req dto.SubjectUpdateRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	ctx := c.Request().Context()
	if req.Name != nil {
		if err := h.svc.Update(ctx, id, *req.Name); err != nil {
			return failSubject(c, err)
		}
	}
	if req.Mask != nil {
		if err := h.svc.UpdateMask(ctx, id, *req.Mask); err != nil {
			return failSubject(c, err)
		}
	}
	// Return the Updated view rather than an empty body: the frontend would otherwise have to
	// refetch the list by id to refresh, and saving one GET is not worth it — return the latest
	// entity directly here to save a round trip.
	sub, err := h.svc.Get(ctx, id)
	if err != nil {
		return failSubject(c, err)
	}
	return ok(c, dto.NewSubjectSummary(sub))
}

func (h *SubjectHandler) Delete(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid subject id", err.Error())
	}
	if err := h.svc.Delete(c.Request().Context(), id); err != nil {
		return failSubject(c, err)
	}
	return ok(c, nil)
}

func failSubject(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fail(c, CodeNotFound, "subject not found", err.Error())
	case errors.Is(err, domain.ErrInvalidInput):
		return fail(c, CodeInvalidInput, err.Error(), "")
	case errors.Is(err, domain.ErrInvalidState):
		return fail(c, CodeInvalidState, err.Error(), "")
	default:
		return fail(c, CodeInternal, "subject operation failed", err.Error())
	}
}
