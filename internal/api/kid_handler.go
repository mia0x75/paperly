package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	"paperly/internal/service"
)

type KidHandler struct {
	svc *service.KidService
}

func NewKidHandler(svc *service.KidService) *KidHandler {
	return &KidHandler{svc: svc}
}

// createKidRequest is the request body for POST /api/kids.
//
// SubjectIDs opts into the atomic profile+whitelist path: when present (even empty)
// the two writes share one transaction; when absent only the profile is written
// (callers that do not manage subjects use the profile-only path).
type createKidRequest struct {
	Name       string   `json:"name"`
	Grade      int      `json:"grade"`
	Gender     *int     `json:"gender"` // 1=male / 0=female / omitted=not set
	Pin        *string  `json:"pin"`
	SubjectIDs *[]int64 `json:"subject_ids"`
}

// updateKidRequest is the request body for PUT /api/kids/:id.
// SubjectIDs opts into the atomic profile+whitelist path (see createKidRequest).
type updateKidRequest struct {
	Name       string   `json:"name"`
	Grade      int      `json:"grade"`
	Gender     *int     `json:"gender"`
	Track      int      `json:"track"` // 0 undecided / 1 arts / 2 science
	SubjectIDs *[]int64 `json:"subject_ids"`
}

func (h *KidHandler) RegisterRoutes(g *echo.Group) {
	g.POST("/kids", h.Create)
	g.GET("/kids", h.List)
	g.GET("/kids/:id", h.Get)
	g.PUT("/kids/:id", h.Update)
	g.DELETE("/kids/:id", h.Delete)
	g.POST("/kids/:id/restore", h.Restore)
	g.POST("/kids/:id/access_code/reset", h.ResetAccessCode)
	g.POST("/kids/:id/pin/reset", h.ResetPin)
}

func (h *KidHandler) Create(c *echo.Context) error {
	var req createKidRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}

	base := service.CreateKidReq{
		Name:   req.Name,
		Grade:  req.Grade,
		Gender: req.Gender,
		Pin:    req.Pin,
	}

	// subject_ids present (even empty) opts into the atomic profile+whitelist write;
	// absent keeps the profile-only path for callers that do not manage subjects.
	var resp *service.CreateKidResponse
	var err error
	if req.SubjectIDs != nil {
		resp, err = h.svc.CreateWithSubjects(c.Request().Context(), service.CreateKidWithSubjectsReq{
			CreateKidReq: base,
			SubjectIDs:   *req.SubjectIDs,
		})
	} else {
		resp, err = h.svc.Create(c.Request().Context(), base)
	}
	if err != nil {
		return mapError(c, err, "create kid failed")
	}
	return ok(c, dto.KidCreated{
		ID:         resp.ID,
		Name:       resp.Name,
		AccessCode: resp.AccessCode,
		KidURL:     resp.KidURL,
		CreatedAt:  resp.CreatedAt,
	})
}

// List handles GET /api/kids, supporting query: status / grade / include_deleted.
//
// Deleted kids are excluded by default; status=all or include_deleted=true also
// returns deleted kids (the settings page needs to show and restore soft-deleted
// students).
func (h *KidHandler) List(c *echo.Context) error {
	raw := c.QueryParam("status")
	filter := domain.ListKidsFilter{
		Grade:          intParam(c, "grade", 0),
		IncludeDeleted: c.QueryParam("include_deleted") == "true" || raw == "all",
	}
	// status is INTEGER (1/2/3); "all" or omitted means no filter. An illegal number
	// is 400 directly, no silent ignore — otherwise the caller thinks it filtered
	// when it did not.
	if raw != "" && raw != "all" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fail(c, CodeValidation, "invalid status", err.Error())
		}
		filter.Status = n
	}
	kids, err := h.svc.List(c.Request().Context(), filter)
	if err != nil {
		return mapError(c, err, "list kids failed")
	}
	return ok(c, dto.NewKidSummaries(kids))
}

func (h *KidHandler) Get(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid kid id", err.Error())
	}
	kid, err := h.svc.Get(c.Request().Context(), id)
	if err != nil {
		return mapError(c, err, "get kid failed")
	}
	return ok(c, dto.NewKidDetail(kid))
}

func (h *KidHandler) Update(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid kid id", err.Error())
	}
	var req updateKidRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}

	base := service.UpdateKidReq{
		Name:   req.Name,
		Grade:  req.Grade,
		Gender: req.Gender,
		Track:  req.Track,
	}

	// subject_ids present opts into the atomic profile+whitelist path (see Create).
	var kid *domain.Kid
	if req.SubjectIDs != nil {
		kid, err = h.svc.UpdateWithSubjects(c.Request().Context(), id, service.UpdateKidWithSubjectsReq{
			UpdateKidReq: base,
			SubjectIDs:   *req.SubjectIDs,
		})
	} else {
		kid, err = h.svc.Update(c.Request().Context(), id, base)
	}
	if err != nil {
		return mapError(c, err, "update kid failed")
	}
	return ok(c, dto.NewKidDetail(kid))
}

// Delete handles DELETE /api/kids/:id (soft delete).
func (h *KidHandler) Delete(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid kid id", err.Error())
	}
	if err := h.svc.Delete(c.Request().Context(), id); err != nil {
		return mapError(c, err, "delete kid failed")
	}
	return c.NoContent(http.StatusNoContent)
}

// Restore handles POST /api/kids/:id/restore (restores a soft-deleted student to active).
func (h *KidHandler) Restore(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid kid id", err.Error())
	}
	kid, err := h.svc.Restore(c.Request().Context(), id)
	if err != nil {
		return mapError(c, err, "restore kid failed")
	}
	return ok(c, dto.NewKidDetail(kid))
}

// resetPinRequest is the request body for POST /api/kids/:id/pin/reset.
// NewPin omitted or empty string = clear the PIN; non-empty = set a new PIN.
type resetPinRequest struct {
	NewPin *string `json:"new_pin"`
}

func (h *KidHandler) ResetAccessCode(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid kid id", err.Error())
	}
	kid, err := h.svc.ResetAccessCode(c.Request().Context(), id)
	if err != nil {
		return mapError(c, err, "reset access_code failed")
	}
	return ok(c, dto.NewResetAccessCodeResponseonse(kid))
}

// ResetPin handles POST /api/kids/:id/pin/reset (clears or sets a student PIN).
// No body or an empty new_pin → clear; non-empty → validate and set.
func (h *KidHandler) ResetPin(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid kid id", err.Error())
	}

	var req resetPinRequest
	// Allow an empty body: treat EOF as "no new_pin provided" = clear the PIN.
	if c.Request().ContentLength != 0 {
		if err := c.Bind(&req); err != nil {
			return fail(c, CodeInvalidBody, "invalid request body", err.Error())
		}
	}
	newPin := ""
	if req.NewPin != nil {
		newPin = *req.NewPin
	}

	kid, err := h.svc.ResetPin(c.Request().Context(), id, newPin)
	if err != nil {
		return mapError(c, err, "reset pin failed")
	}
	return ok(c, dto.NewResetPinResponseonse(kid))
}

// parseIDParam reads the :id path param and parses it as a positive integer.
func parseIDParam(c *echo.Context) (int64, error) {
	return parseNamedIDParam(c, "id")
}

// parseNamedIDParam reads the named primary-key path param and parses it as a positive integer.
//
// A named variant exists because the knowledge-point routes hang under /subjects/:subjectId/...,
// where there is no :id — reusing the fixed-read-:id version would always get an empty string.
func parseNamedIDParam(c *echo.Context, name string) (int64, error) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil {
		return 0, err
	}
	if id <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return id, nil
}

func mapError(c *echo.Context, err error, fallbackMsg string) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fail(c, CodeNotFound, "kid not found", err.Error())
	case errors.Is(err, domain.ErrInvalidInput):
		return fail(c, CodeInvalidInput, err.Error(), "")
	case errors.Is(err, domain.ErrAccessCodeCollision):
		return fail(c, CodeCodeCollision, "access_code collision", err.Error())
	case errors.Is(err, domain.ErrDuplicateName):
		return fail(c, CodeDuplicateName, "kid name already exists", err.Error())
	case errors.Is(err, domain.ErrInvalidState):
		return fail(c, CodeInvalidState, err.Error(), "")
	default:
		return fail(c, CodeInternal, fallbackMsg, err.Error())
	}
}
