package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	"paperly/internal/service"
)

// KidInviteHandler handles the kid-side **anonymous** invite flow (/api/kid/invite/*).
//
// The /invite/:code sole-entry responsibility: validate the invite code → set the PIN → issue the kid JWT directly.
// Both endpoints are rate-limited to 5 requests/minute per client IP (verify/setup share one bucket),
// preventing invite-code brute-forcing/credential-stuffing. access_code is a login credential: only consumed,
// never returned, never logged.
type KidInviteHandler struct {
	svc *service.AuthService
}

// NewKidInviteHandler constructs the invite-flow handler.
func NewKidInviteHandler(svc *service.AuthService) *KidInviteHandler {
	return &KidInviteHandler{svc: svc}
}

// RegisterRoutes registers the anonymous invite routes (mounted under /api, not passing the auth middleware).
func (h *KidInviteHandler) RegisterRoutes(g *echo.Group) {
	g.POST("/kid/invite/verify", h.Verify)
	g.POST("/kid/invite/setup", h.Setup)
}

// inviteRequest is the request body shared by verify/setup (code = invite code, the access_code the parent generated).
type inviteRequest struct {
	Code string `json:"code"`
	Pin  string `json:"pin"`
}

// Verify handles POST /api/kid/invite/verify.
// An invalid code returns HTTP 200 + {valid:false}: it does not distinguish "code absent" from "code valid but the kid
// unusable", nor does it return kid_id — avoiding account enumeration / building a code→id mapping via response differences.
func (h *KidInviteHandler) Verify(c *echo.Context) error {
	if h.svc.InviteRateLimited(inviteIPKey(c)) {
		return c.JSON(http.StatusTooManyRequests, errorResponse{
			Code: CodeLoginLocked, Message: "too many attempts, please retry later",
		})
	}
	var req inviteRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		return ok(c, dto.InviteVerifyResponse{Valid: false})
	}

	k, err := h.svc.InviteVerify(c.Request().Context(), code)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return ok(c, dto.InviteVerifyResponse{Valid: false})
		}
		return fail(c, CodeInternal, "service temporarily unavailable", err.Error())
	}
	return ok(c, dto.InviteVerifyResponse{Valid: true, Name: k.Name})
}

// Setup handles POST /api/kid/invite/setup.
//
// Writes the PIN (bcrypt) + bumps pin_version+1 + issues the JWT. Success enters the kid side directly, skipping the login page.
// access_code never enters any log field (Detail only gives fixed copy).
func (h *KidInviteHandler) Setup(c *echo.Context) error {
	if h.svc.InviteRateLimited(inviteIPKey(c)) {
		return c.JSON(http.StatusTooManyRequests, errorResponse{
			Code: CodeLoginLocked, Message: "too many attempts, please retry later",
		})
	}
	var req inviteRequest
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	code := strings.TrimSpace(req.Code)
	if code == "" || req.Pin == "" {
		return fail(c, CodeValidation, "access code and PIN are required", "")
	}

	token, k, err := h.svc.InviteSetup(c.Request().Context(), code, req.Pin)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidInput):
			return fail(c, CodeInvalidInput, err.Error(), "")
		case errors.Is(err, domain.ErrNotFound):
			return fail(c, CodeInvalidInput, "invalid or expired access code", "")
		default:
			return fail(c, CodeInternal, "failed to set PIN", err.Error())
		}
	}
	return ok(c, dto.InviteSetupResponse{Token: token, Kid: dto.NewMeKid(k)})
}

// inviteIPKey builds the invite-flow rate-limit key. Fixed 5 requests/minute/source IP.
// echo.Context.RealIP already falls back on X-Forwarded-For; a single machine with multiple kids sharing one
// external IP makes 5/minute a usable collaborative cadence for that scenario while still throttling brute force.
func inviteIPKey(c *echo.Context) string {
	return c.RealIP()
}
