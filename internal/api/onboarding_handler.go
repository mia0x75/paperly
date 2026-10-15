package api

import (
	"errors"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	"paperly/internal/service"
)

// OnboardingHandler handles the first-run onboarding flow (/api/onboarding).
// It exposes only two endpoints: the status query (the frontend guard decides whether to force /welcome) and the completion marker.
//
// The complete endpoint issues the default parent JWT on completion, which the frontend stores to enter the dashboard directly,
// skipping manual login; when no parent is registered it skips issuing and the frontend falls back to manual login.
type OnboardingHandler struct {
	svc     *service.OnboardingService
	authSvc *service.AuthService
}

// NewOnboardingHandler constructs the first-run onboarding HTTP handler. authSvc issues the JWT on completion.
func NewOnboardingHandler(svc *service.OnboardingService, authSvc *service.AuthService) *OnboardingHandler {
	return &OnboardingHandler{svc: svc, authSvc: authSvc}
}

// RegisterRoutes registers the onboarding routes on the given group.
func (h *OnboardingHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/onboarding/status", h.Status)
	g.POST("/onboarding/complete", h.Complete)
}

// onboardingStatusResponse is the response body for GET /api/onboarding/status.
type onboardingStatusResponse struct {
	Done bool `json:"done"`
}

// onboardingCompleteResponse is the response body for POST /api/onboarding/complete.
// token/user are optional fields: they are omitted when no parent is registered, and the frontend falls back to manual login.
type onboardingCompleteResponse struct {
	Done  bool          `json:"done"`
	Token string        `json:"token,omitempty"`
	User  *dto.MeParent `json:"user,omitempty"`
}

// Status handles GET /api/onboarding/status.
func (h *OnboardingHandler) Status(c *echo.Context) error {
	done, err := h.svc.Status(c.Request().Context())
	if err != nil {
		return fail(c, CodeInternal, "onboarding status check failed", err.Error())
	}
	return ok(c, onboardingStatusResponse{Done: done})
}

// Complete handles POST /api/onboarding/complete.
//
// On success it also issues the default parent JWT: onboarding is the trust starting point of the
// self-hosted Box, so no PIN is verified here — it is the application's own "onboarding completed"
// admission ticket. When no parent is registered it skips issuing, the response still has done=true
// but omits token/user, and the frontend falls back to the login page for manual login.
//
// Safety gate: when onboarding is already a complete final state (marker exists and users/kids are
// both non-empty), replay is rejected — otherwise anyone could call this endpoint anonymously and
// repeatedly trade for a parent JWT. Returns 409 (state conflict).
func (h *OnboardingHandler) Complete(c *echo.Context) error {
	done, err := h.svc.Complete(c.Request().Context())
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrOnboardingCompleted):
			return fail(c, CodeConflict, "onboarding already completed", err.Error())
		case errors.Is(err, domain.ErrInvalidState):
			return fail(c, CodeInvalidState, err.Error(), "")
		default:
			return fail(c, CodeInternal, "complete onboarding failed", err.Error())
		}
	}
	resp := onboardingCompleteResponse{Done: done}
	if !done {
		return ok(c, resp)
	}
	token, u, err := h.authSvc.IssueDefaultToken(c.Request().Context())
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrNotFound):
			// Edge case: onboarding was interrupted abnormally and there is no parent in the DB — skip
			// issuing and let the frontend go to manual login.
			return ok(c, resp)
		default:
			return fail(c, CodeInternal, "issue onboarding token failed", err.Error())
		}
	}
	me := dto.NewMeParent(u)
	resp.Token = token
	resp.User = &me
	return ok(c, resp)
}
