package api

import (
	"errors"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	"paperly/internal/service"
)

// UserHandler handles requests under /api/users (parent accounts).
//
// Multi-parent model: POST /users registers a new parent, GET /users lists all
// (for onboarding and the settings page). Reading/writing the "logged-in self" has been merged
// into /api/me*; /users only adds to and lists the roster.
type UserHandler struct {
	svc *service.UserService
}

// NewUserHandler constructs the parent-account HTTP handler.
func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// RegisterRoutes registers the parent-account routes on the given group.
//
// The old /users/me three endpoints (Me / UpdateMe / UpdatePin) have been removed —
// reading/writing the logged-in self goes through /api/me* (audience routing); not registering
// them here makes them a 404.
func (h *UserHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/users", h.List)
	g.POST("/users", h.Create)
}

// createUserRequest is the request body for POST /api/users.
type createUserRequest struct {
	DisplayName string  `json:"display_name"`
	Pin         *string `json:"pin"` // optional, 4~6 digits
}

// Create handles POST /api/users (registers a new parent).
func (h *UserHandler) Create(c *echo.Context) error {
	var req createUserRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	u, err := h.svc.Create(c.Request().Context(), service.CreateUserReq{
		DisplayName: req.DisplayName,
		Pin:         req.Pin,
	})
	if err != nil {
		return failUser(c, err)
	}
	return ok(c, dto.NewUserSummary(u))
}

// List handles GET /api/users (all parent accounts).
func (h *UserHandler) List(c *echo.Context) error {
	users, err := h.svc.List(c.Request().Context())
	if err != nil {
		return failUser(c, err)
	}
	return ok(c, dto.NewUserSummaries(users))
}

// failUser maps parent-account related errors.
func failUser(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fail(c, CodeNotFound, "parent account not found", err.Error())
	case errors.Is(err, domain.ErrInvalidInput):
		return fail(c, CodeInvalidInput, err.Error(), "")
	case errors.Is(err, domain.ErrDuplicateDisplayName):
		return fail(c, CodeDuplicateName, "parent display name already exists", err.Error())
	default:
		return fail(c, CodeInternal, "user operation failed", err.Error())
	}
}
