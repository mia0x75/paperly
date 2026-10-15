// Package api is Paperly's HTTP layer: binding, validation, service calls, unified DTOs.
// Dependency direction: api -> service -> domain.
package api

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// Business error codes.
const (
	CodeOK = 0

	// 4xxxx: client errors
	CodeInvalidBody     = 40001
	CodeValidation      = 40002
	CodeNotFound        = 40004
	CodeInvalidInput    = 40010
	CodeCodeCollision   = 40011
	CodeInvalidState    = 40012
	CodeDuplicateName   = 40013 // Student name already exists
	CodeUnauthorized    = 40100 // Invalid session/credentials
	CodeConflict        = 40900 // State conflict: onboarding already completed, replay rejected
	CodeLoginLocked     = 42900 // Too many login failures, briefly locked
	CodeTooManyRequests = 42901 // System busy (upload queue full); retry later

	// File upload
	CodeFileTooLarge    = 40020
	CodeUnsupportedType = 40021

	// 5xxxx: server errors
	CodeInternal = 50001
)

// response is the success body: {"code":0,"data":...}
type response struct {
	Code int `json:"code"`
	Data any `json:"data"`
}

// errorResponse is the failure body: {"code":...,"message":"...","detail":"..."}
type errorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// ok returns a success response.
func ok(c *echo.Context, data any) error {
	return c.JSON(http.StatusOK, response{Code: CodeOK, Data: data})
}

// fail returns a failure response, deriving the HTTP status from the business code.
func fail(c *echo.Context, code int, message, detail string) error {
	status := http.StatusBadRequest
	switch {
	case code >= 50000:
		status = http.StatusInternalServerError
	case code == CodeNotFound:
		status = http.StatusNotFound
	case code == CodeConflict:
		status = http.StatusConflict
	case code == CodeLoginLocked || code == CodeTooManyRequests:
		status = http.StatusTooManyRequests
	}
	return c.JSON(status, errorResponse{Code: code, Message: message, Detail: detail})
}

// failTooMany returns a 429 response (upload queue full).
func failTooMany(c *echo.Context, message string) error {
	return fail(c, CodeTooManyRequests, message, "")
}
