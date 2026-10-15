package middleware

import (
	"context"
	"log/slog"

	"github.com/labstack/echo/v5"
)

// OnboardingChecker queries whether first-run onboarding has completed
// (implemented by *service.OnboardingService).
//
// (true, nil) means onboarding is done and normal auth applies; (false, nil)
// means onboarding is incomplete, so anonymous requests are allowed on the
// exempt paths; err != nil is handled fail-closed (the gate runs as usual).
type OnboardingChecker interface {
	Status(ctx context.Context) (done bool, err error)
}

// onboardingExemptPaths are the exact (method, path) combinations allowed
// anonymously while onboarding is incomplete. They only cover the paths that
// the Welcome first-run onboarding actually needs:
//
//	GET  /api/users    POST /api/users   (register the first parent)
//	GET  /api/kids     POST /api/kids    (register the first kid)
//	GET  /api/subjects                   (subject dictionary for the kid form)
//
// GET /api/subjects is read-only system preset data (subject names / mandatory
// flags) and is required by the "register kid" dialog to build subject_ids before
// any parent token exists. It is the same exposure level as the already-exempt
// users/kids lists. Exact matching means /api/kids/:id, /api/me, etc. are never
// exempt (they are not registration actions, and anonymous allowance would
// bypass auth).
var onboardingExemptPaths = map[string]struct{}{
	"GET /api/users":    {},
	"POST /api/users":   {},
	"GET /api/kids":     {},
	"POST /api/kids":    {},
	"GET /api/subjects": {},
}

// WithOnboardingBypass wraps a gate (such as requireParent): while first-run
// onboarding is incomplete, it allows the four registration paths through
// (skipping the gate) and hands every other request to the gate unchanged.
//
// fail-closed: when the Status query errors, it is treated as "done", so the
// gate runs and anonymous requests are rejected with a 401. An uncertain state
// blocking the onboarding page is preferred over a middleware error becoming
// an auth bypass.
func WithOnboardingBypass(log *slog.Logger, gate echo.MiddlewareFunc, checker OnboardingChecker) echo.MiddlewareFunc {
	if log == nil {
		log = slog.Default()
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			key := c.Request().Method + " " + c.Request().URL.Path
			if _, exempt := onboardingExemptPaths[key]; exempt {
				done, err := checker.Status(c.Request().Context())
				switch {
				case err != nil:
					log.Warn("onboarding status check failed, fail closed",
						slog.String("path", c.Request().URL.Path),
						slog.String("reason", err.Error()))
				case !done:
					// Onboarding incomplete: allow the four registration
					// paths anonymously.
					return next(c)
				}
			}
			return gate(next)(c)
		}
	}
}
