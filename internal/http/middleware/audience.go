// Package middleware provides HTTP middleware.
package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"paperly/internal/auth"
)

type ctxKey string

const (
	// CtxAudience is the visitor type: parent or kid.
	CtxAudience ctxKey = "aud"
	// CtxKidID is the kid id carried by a kid token.
	CtxKidID ctxKey = "kid_id"
	// CtxUserID is the user id carried by a parent token.
	CtxUserID ctxKey = "user_id"
)

type Audience string

const (
	AudienceParent Audience = "parent"
	AudienceKid    Audience = "kid"
)

// TokenVerifier verifies and parses the JWT payload, implemented by
// internal/auth.Store.
type TokenVerifier interface {
	Verify(token string) (*auth.Claims, error)
}

// PrincipalValidator checks that the account backing the token payload is still
// valid in the DB (not deleted, allowed status, pin_version unchanged).
// Implemented by AuthService.
//
// Returning nil means it is usable; otherwise the middleware responds with 401.
type PrincipalValidator interface {
	ParentValid(ctx context.Context, userID int64, pinVersion int) error
	KidValid(ctx context.Context, kidID int64, pinVersion int) error
}

// RequireAudience validates a Bearer JWT:
//
//   - missing token / not Bearer -> 401
//   - failed signature / expired / invalid payload (type, primary key, pv) -> 401
//   - typ mismatches expected -> 401
//   - live validation failed (account deleted / status mismatch / pin_version
//     changed) -> 401
//
// On success it injects ctx by typ: parent writes Audience+UserID, kid writes
// Audience+KidID. It is only mounted on the auth-required routes.
func RequireAudience(log *slog.Logger, verifier TokenVerifier, validator PrincipalValidator, expected Audience) echo.MiddlewareFunc {
	if log == nil {
		log = slog.Default()
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			token, err := tokenFrom(c)
			if err != nil {
				log.Warn("auth missing token", slog.String("path", c.Request().URL.Path))
				return unauthorized(c, "missing credentials")
			}
			claims, err := verifier.Verify(token)
			if err != nil {
				log.Warn("auth invalid token", slog.String("reason", err.Error()))
				return unauthorized(c, "invalid session, please log in again")
			}
			switch claims.Typ {
			case auth.TypParent:
				if expected != AudienceParent {
					log.Warn("auth typ mismatch", slog.String("typ", claims.Typ))
					return unauthorized(c, "credential type does not match this endpoint")
				}
				if err := validator.ParentValid(c.Request().Context(), claims.Sub, claims.PV); err != nil {
					log.Warn("auth principal invalid", slog.Int64("user_id", claims.Sub), slog.String("reason", err.Error()))
					return unauthorized(c, "account status invalid, please log in again")
				}
				(*c).Set(string(CtxAudience), AudienceParent)
				(*c).Set(string(CtxUserID), claims.Sub)
			case auth.TypKid:
				if expected != AudienceKid {
					log.Warn("auth typ mismatch", slog.String("typ", claims.Typ))
					return unauthorized(c, "credential type does not match this endpoint")
				}
				if err := validator.KidValid(c.Request().Context(), claims.Sub, claims.PV); err != nil {
					log.Warn("auth principal invalid", slog.Int64("kid_id", claims.Sub), slog.String("reason", err.Error()))
					return unauthorized(c, "account status invalid, please log in again")
				}
				(*c).Set(string(CtxAudience), AudienceKid)
				(*c).Set(string(CtxKidID), claims.Sub)
			default:
				log.Warn("auth unsupported typ", slog.String("typ", claims.Typ))
				return unauthorized(c, "invalid credential type")
			}
			return next(c)
		}
	}
}

// unauthorizedCode aligns with api.CodeUnauthorized (40100). The middleware
// cannot depend on the api package (api -> middleware is the only legal
// direction; the reverse would create a cycle), so it is defined locally here.
const unauthorizedCode = 40100

// unauthorized returns 401 with the same envelope as the api layer:
// {code:40100, message}.
func unauthorized(c *echo.Context, msg string) error {
	return c.JSON(http.StatusUnauthorized, map[string]any{"code": unauthorizedCode, "message": msg})
}

func tokenFrom(c *echo.Context) (string, error) {
	header := c.Request().Header.Get("Authorization")
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", errors.New("missing bearer token")
	}
	return strings.TrimSpace(token), nil
}

// AudienceFromCtx returns the audience, defaulting to parent when not injected.
func AudienceFromCtx(c *echo.Context) Audience {
	if v := (*c).Get(string(CtxAudience)); v != nil {
		if a, ok := v.(Audience); ok {
			return a
		}
	}
	return AudienceParent
}

func KidIDFromCtx(c *echo.Context) int64 {
	if v := (*c).Get(string(CtxKidID)); v != nil {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}

func UserIDFromCtx(c *echo.Context) int64 {
	if v := (*c).Get(string(CtxUserID)); v != nil {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}

// RequireAnyAudience validates that the JWT belongs to a parent or a kid
// (either passes). On success it injects context by typ: parent ->
// AudienceParent + UserID; kid -> AudienceKid + KidID.
func RequireAnyAudience(log *slog.Logger, verifier TokenVerifier, validator PrincipalValidator) echo.MiddlewareFunc {
	if log == nil {
		log = slog.Default()
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			token, err := tokenFrom(c)
			if err != nil {
				log.Warn("auth missing token", slog.String("path", c.Request().URL.Path))
				return unauthorized(c, "missing credentials")
			}
			claims, err := verifier.Verify(token)
			if err != nil {
				log.Warn("auth invalid token", slog.String("reason", err.Error()))
				return unauthorized(c, "invalid session, please log in again")
			}
			switch claims.Typ {
			case auth.TypParent:
				if err := validator.ParentValid(c.Request().Context(), claims.Sub, claims.PV); err != nil {
					log.Warn("auth principal invalid", slog.Int64("user_id", claims.Sub), slog.String("reason", err.Error()))
					return unauthorized(c, "account status invalid, please log in again")
				}
				(*c).Set(string(CtxAudience), AudienceParent)
				(*c).Set(string(CtxUserID), claims.Sub)
				(*c).Set(string(CtxKidID), int64(0))
			case auth.TypKid:
				if err := validator.KidValid(c.Request().Context(), claims.Sub, claims.PV); err != nil {
					log.Warn("auth principal invalid", slog.Int64("kid_id", claims.Sub), slog.String("reason", err.Error()))
					return unauthorized(c, "account status invalid, please log in again")
				}
				(*c).Set(string(CtxAudience), AudienceKid)
				(*c).Set(string(CtxKidID), claims.Sub)
				(*c).Set(string(CtxUserID), int64(0))
			default:
				log.Warn("auth unsupported typ", slog.String("typ", claims.Typ))
				return unauthorized(c, "invalid credential type")
			}
			return next(c)
		}
	}
}
