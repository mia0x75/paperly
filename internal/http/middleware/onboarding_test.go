package middleware

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type fakeChecker struct {
	done bool
	err  error
}

func (f fakeChecker) Status(context.Context) (bool, error) { return f.done, f.err }

// gate401 simulates requireParent with no token: always 401.
func gate401(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		return c.JSON(http.StatusUnauthorized, map[string]any{"code": 40100, "message": "missing credentials"})
	}
}

// passThrough lets the gate pass (used to verify that requests with a token
// still reach the handler).
func passThrough(next echo.HandlerFunc) echo.HandlerFunc { return next }

// newBypassEcho assembles an echo with WithOnboardingBypass on the /api group,
// covering the exempt registration paths plus non-exempt ones (DELETE /kids/:id,
// GET /exams); the handlers always return 200.
func newBypassEcho(t *testing.T, gate echo.MiddlewareFunc, checker OnboardingChecker) *echo.Echo {
	t.Helper()
	e := echo.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	g := e.Group("/api", WithOnboardingBypass(log, gate, checker))
	ok := func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"ok": true})
	}
	g.GET("/users", ok)
	g.POST("/users", ok)
	g.GET("/kids", ok)
	g.POST("/kids", ok)
	g.GET("/subjects", ok)
	g.POST("/users/me", ok)
	g.GET("/kids/:id", ok)
	g.DELETE("/kids/:id", ok)
	g.GET("/exams", ok)
	return e
}

func doReq(e *echo.Echo, method, target string) (int, string) {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec.Code, rec.Body.String()
}

// While onboarding is incomplete the registration paths are allowed anonymously;
// all other requests and the completed state behave unchanged (fail-closed runs
// the gate).
func TestOnboardingBypass(t *testing.T) {
	pending := fakeChecker{done: false}
	done := fakeChecker{done: true}
	broken := fakeChecker{err: errors.New("db gone")}

	cases := []struct {
		name     string
		checker  OnboardingChecker
		method   string
		target   string
		wantCode int
	}{
		{"pending+GET /api/users 放行", pending, http.MethodGet, "/api/users", http.StatusOK},
		{"pending+POST /api/users 放行", pending, http.MethodPost, "/api/users", http.StatusOK},
		{"pending+GET /api/kids 放行", pending, http.MethodGet, "/api/kids", http.StatusOK},
		{"pending+POST /api/kids 放行", pending, http.MethodPost, "/api/kids", http.StatusOK},
		{"pending+GET /api/subjects 放行", pending, http.MethodGet, "/api/subjects", http.StatusOK},
		{"pending+DELETE /api/kids/:id 走 gate", pending, http.MethodDelete, "/api/kids/1", http.StatusUnauthorized},
		{"pending+GET /api/exams 走 gate", pending, http.MethodGet, "/api/exams", http.StatusUnauthorized},
		{"pending+GET /api/users/me 不豁免", pending, http.MethodGet, "/api/users/me", http.StatusUnauthorized},
		{"pending+PUT /api/kids/:id 走 gate", pending, http.MethodPut, "/api/kids/1", http.StatusUnauthorized},
		{"done+GET /api/users 走 gate", done, http.MethodGet, "/api/users", http.StatusUnauthorized},
		{"done+POST /api/kids 走 gate", done, http.MethodPost, "/api/kids", http.StatusUnauthorized},
		{"done+GET /api/subjects 走 gate", done, http.MethodGet, "/api/subjects", http.StatusUnauthorized},
		{"done+GET /api/exams 走 gate", done, http.MethodGet, "/api/exams", http.StatusUnauthorized},
		{"error+GET /api/users fail-closed", broken, http.MethodGet, "/api/users", http.StatusUnauthorized},
		{"error+POST /api/kids fail-closed", broken, http.MethodPost, "/api/kids", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newBypassEcho(t, gate401, tc.checker)
			code, _ := doReq(e, tc.method, tc.target)
			require.Equal(t, tc.wantCode, code)
		})
	}

	t.Run("带 token 时 gate 放行进 handler", func(t *testing.T) {
		e := newBypassEcho(t, passThrough, done)
		code, _ := doReq(e, http.MethodGet, "/api/users")
		require.Equal(t, http.StatusOK, code)
	})
}
