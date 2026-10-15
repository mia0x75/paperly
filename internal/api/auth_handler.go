package api

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	"paperly/internal/http/middleware"
	"paperly/internal/service"
)

// AuthHandler handles auth interfaces and session interfaces.
//
// Anonymous: GET /api/auth/parent-accounts, GET /api/auth/kid-accounts,
//
//	POST /api/auth/login, POST /api/kid/auth/login
//
// Authenticated (/api/me* single group, three entry points, dispatched by token audience):
//
//	GET  /api/me        requireAny    -- parent returns MeParent, kid returns MeKid
//	POST /api/me/pin    requireAny    -- parent changes parent PIN, kid changes kid PIN
//	POST /api/me/rename requireParent -- parent only; a kid token is blocked with 401 by middleware
type AuthHandler struct {
	svc           *service.AuthService
	requireAny    echo.MiddlewareFunc
	requireParent echo.MiddlewareFunc
}

// NewAuthHandler constructs the auth HTTP handler. Middleware is assembled by
// main using middleware.RequireAnyAudience / RequireAudience.
func NewAuthHandler(svc *service.AuthService, requireAny, requireParent echo.MiddlewareFunc) *AuthHandler {
	return &AuthHandler{svc: svc, requireAny: requireAny, requireParent: requireParent}
}

func (h *AuthHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/auth/parent-accounts", h.Accounts)
	g.GET("/auth/kid-accounts", h.KidAccounts)
	g.POST("/auth/login", h.LoginParent)
	g.POST("/kid/auth/login", h.LoginKid)
	g.GET("/me", h.Me, h.requireAny)
	g.POST("/me/pin", h.ChangePin, h.requireAny)
	g.POST("/me/rename", h.RenameParent, h.requireParent)
}

// authLoginRequest is the login request body for parent/kid (same fields; kid ignores user_id).
type authLoginRequest struct {
	UserID int64  `json:"user_id"`
	Pin    string `json:"pin"`
}

func (h *AuthHandler) LoginParent(c *echo.Context) error {
	var req authLoginRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	if req.UserID <= 0 {
		return fail(c, CodeValidation, "user_id is required", "")
	}
	if req.Pin == "" {
		return fail(c, CodeValidation, "pin is required", "")
	}
	token, u, err := h.svc.LoginParent(c.Request().Context(), req.UserID, req.Pin)
	if err != nil {
		return failAuth(c, err)
	}
	return ok(c, map[string]any{"token": token, "user": dto.NewMeParent(u)})
}

func (h *AuthHandler) LoginKid(c *echo.Context) error {
	var req authLoginRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	if req.UserID <= 0 {
		return fail(c, CodeValidation, "kid_id is required", "")
	}
	if req.Pin == "" {
		return fail(c, CodeValidation, "pin is required", "")
	}
	token, k, err := h.svc.LoginKid(c.Request().Context(), req.UserID, req.Pin)
	if err != nil {
		return failAuth(c, err)
	}
	return ok(c, map[string]any{"token": token, "kid": dto.NewMeKid(k)})
}

// Accounts handles GET /api/auth/parent-accounts (anonymous, parent login page selection list).
func (h *AuthHandler) Accounts(c *echo.Context) error {
	users, err := h.svc.ListAccounts(c.Request().Context())
	if err != nil {
		return failAuth(c, err)
	}
	return ok(c, dto.NewAccountSummaries(users))
}

// KidAccounts handles GET /api/auth/kid-accounts (anonymous, kid login page selection list).
func (h *AuthHandler) KidAccounts(c *echo.Context) error {
	kids, err := h.svc.ListKidAccounts(c.Request().Context())
	if err != nil {
		return failAuth(c, err)
	}
	return ok(c, dto.NewLoginListKids(kids))
}

// Me handles GET /api/me (requireAny). Dispatches by token audience:
// parent -> MeParent{id,display_name}, kid -> MeKid{id,name,grade}.
// The middleware already injected the primary key; we look up once more here
// to cover the deletion window between middleware and handler.
func (h *AuthHandler) Me(c *echo.Context) error {
	if middleware.AudienceFromCtx(c) == middleware.AudienceKid {
		k, err := h.svc.CurrentKid(c.Request().Context(), middleware.KidIDFromCtx(c))
		if err != nil {
			return failAuth(c, err)
		}
		return ok(c, dto.NewMeKid(k))
	}
	u, err := h.svc.CurrentParent(c.Request().Context(), middleware.UserIDFromCtx(c))
	if err != nil {
		return failAuth(c, err)
	}
	return ok(c, dto.NewMeParent(u))
}

// renameParentRequest is the request body for POST /api/me/rename.
type renameParentRequest struct {
	DisplayName string `json:"display_name"`
}

// RenameParent handles POST /api/me/rename (rename the current parent while logged in).
// Only requireParent is attached: a kid token calling this endpoint is 401 at
// the middleware. It locates by the user_id in the token and never touches
// another account (parent-to-parent management is out of scope).
func (h *AuthHandler) RenameParent(c *echo.Context) error {
	var req renameParentRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	u, err := h.svc.RenameParent(c.Request().Context(), middleware.UserIDFromCtx(c), req.DisplayName)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidInput):
			return fail(c, CodeInvalidInput, err.Error(), "")
		case errors.Is(err, domain.ErrDuplicateDisplayName):
			return fail(c, CodeDuplicateName, "parent display name already exists", err.Error())
		case errors.Is(err, domain.ErrNotFound),
			errors.Is(err, service.ErrAccountInactive):
			return failAuth(c, err)
		default:
			return fail(c, CodeInternal, "rename parent failed", err.Error())
		}
	}
	return ok(c, dto.NewMeParent(u))
}

// changePinRequest is the request body for POST /api/me/pin (shared by parent/kid).
type changePinRequest struct {
	OldPin string `json:"old_pin"` // an account that never set a PIN may send empty (first-time setup)
	NewPin string `json:"new_pin"` // 4~6 digits, required
}

// ChangePin handles POST /api/me/pin (requireAny). Dispatches by token
// audience to ChangeParentPin / ChangeKidPin; when no PIN is set (parent
// empty hash / kid placeholder), the
// old PIN check is skipped and the PIN is set for the first time.
// On success returns {token: new JWT}: pin_version has been incremented so all
// old tokens are invalidated, and the frontend seamlessly continues with the new
// token without re-login. A wrong old PIN returns 400 (CodeInvalidInput) rather
// than 401 -- the user is changing their PIN while logged in and should not be
// treated as "not logged in" and have their token cleared.
func (h *AuthHandler) ChangePin(c *echo.Context) error {
	var req changePinRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	var token string
	var err error
	if middleware.AudienceFromCtx(c) == middleware.AudienceKid {
		token, _, err = h.svc.ChangeKidPin(c.Request().Context(), middleware.KidIDFromCtx(c), req.OldPin, req.NewPin)
	} else {
		token, _, err = h.svc.ChangeParentPin(c.Request().Context(), middleware.UserIDFromCtx(c), req.OldPin, req.NewPin)
	}
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPinMismatch):
			return fail(c, CodeInvalidInput, "current PIN is incorrect", err.Error())
		case errors.Is(err, domain.ErrInvalidInput):
			return fail(c, CodeInvalidInput, err.Error(), "")
		case errors.Is(err, domain.ErrNotFound),
			errors.Is(err, service.ErrAccountInactive):
			return failAuth(c, err)
		default:
			return fail(c, CodeInternal, "change pin failed", err.Error())
		}
	}
	return ok(c, map[string]any{"token": token})
}

// failAuth maps auth errors:
//   - bad credentials / account missing / account inactive / pin_version mismatch -> 401
//   - login rate limit -> 429
//   - anything else -> 500
//
// Bad credentials use a unified 401 message to prevent account enumeration via
// response differences.
func failAuth(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrLoginLocked):
		return c.JSON(http.StatusTooManyRequests, errorResponse{
			Code:    CodeLoginLocked,
			Message: "too many login failures, please retry in 15 minutes",
			Detail:  err.Error(),
		})
	case errors.Is(err, service.ErrLoginInvalid),
		errors.Is(err, service.ErrAccountInactive),
		errors.Is(err, service.ErrPinVersionMismatch),
		errors.Is(err, domain.ErrNotFound):
		return c.JSON(http.StatusUnauthorized, errorResponse{
			Code:    CodeUnauthorized,
			Message: "invalid account or PIN",
			Detail:  err.Error(),
		})
	default:
		return fail(c, CodeInternal, "auth operation failed", err.Error())
	}
}
