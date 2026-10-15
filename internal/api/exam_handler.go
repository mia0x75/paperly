package api

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"

	"paperly/internal/domain"
	"paperly/internal/service"
	"paperly/internal/system"
)

// dateQueryLayout is the format of the list date-filter parameter (matches the DATE
// of exams.exam_date).
const dateQueryLayout = "2006-01-02"

type ExamHandler struct {
	svc  *service.ExamService
	gate *system.Gate
	// auth resolves the signed name of the current actor for appended summary
	// segments. It may be nil in tests; the name then falls back to a generic
	// "家长"/"孩子" label (permission checks use the id, not the name).
	auth *service.AuthService
}

// NewExamHandler constructs the exam HTTP handler. gate may be nil to disable
// concurrency limiting (tests, or when no gate was assembled); auth may be nil
// to fall back to generic summary-author labels.
func NewExamHandler(svc *service.ExamService, gate *system.Gate, auth *service.AuthService) *ExamHandler {
	return &ExamHandler{svc: svc, gate: gate, auth: auth}
}

func (h *ExamHandler) RegisterRoutes(g *echo.Group) {
	g.POST("/exams/upload", h.Upload)
	g.GET("/exams", h.List)
	g.GET("/exams/:id", h.Get)
	g.PUT("/exams/:id", h.Update)
	g.DELETE("/exams/:id", h.Delete)
	// Restore soft-deleted exams. Symmetric to DELETE, reusing the same :id param
	// node.
	g.POST("/exams/:id/restore", h.Restore)
	// Page-level ops (delete + add + reorder, one kind=5 task per request). Subpaths
	// must be registered under the same method tree as /exams/:id, keeping them
	// param/static siblings with /exams/upload.
	g.POST("/exams/:id/pages/ops", h.PagesOps)
	// Page-image preview/download. The path belongs to the pages table, so the route
	// lives under /pages (not exams/:id/pages).
	g.GET("/pages/:page_id/file", h.PageFile)
	// Append-only signed summary segments (rich text). Parent or kid (route group
	// uses requireAny); kid ownership is checked per exam in each handler.
	g.POST("/exams/:id/summary", h.AppendSummary)
	g.DELETE("/exams/:id/summary/:sig", h.DeleteSummary)
}

// mapExamError maps exam-module errors.
//
// Not-found uses the shared CodeNotFound (one 404 code for every entity;
// callers distinguish entities via message/detail, not via code).
func mapExamError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrFileTooLarge):
		return fail(c, CodeFileTooLarge, "file too large", err.Error())
	case errors.Is(err, domain.ErrPDFNotSupported):
		return fail(c, CodeUnsupportedType, "PDF not supported; upload an image or screenshot", err.Error())
	case errors.Is(err, domain.ErrUnsupportedFileType):
		return fail(c, CodeUnsupportedType, "unsupported file type (JPG/PNG/WEBP/GIF only)", err.Error())
	case errors.Is(err, domain.ErrNotFound):
		return fail(c, CodeNotFound, "exam not found", err.Error())
	case errors.Is(err, domain.ErrInvalidInput):
		return fail(c, CodeInvalidInput, err.Error(), "")
	case errors.Is(err, domain.ErrInvalidState):
		return fail(c, CodeInvalidState, err.Error(), "")
	default:
		return fail(c, CodeInternal, "exam operation failed", err.Error())
	}
}

// optFormValue reads a form/query parameter; empty string returns nil.
func optFormValue(c *echo.Context, name string) *string {
	v := strings.TrimSpace(c.FormValue(name))
	if v == "" {
		return nil
	}
	return &v
}

// optIntForm reads an integer form parameter; empty or invalid returns nil.
func optIntForm(c *echo.Context, name string) *int {
	v := strings.TrimSpace(c.FormValue(name))
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return &n
}

// Default and range of the list pagination limit. Out-of-range values are always
// 400, never silently clamped by the repository layer.
const (
	listLimitDefault = 20
	listLimitMax     = 100
)

// optStatusQuery parses the exam-list status: empty string = all (0, includes deleted);
// only 1 (active)/2 (deleted) are accepted; anything else (including non-numeric) errors.
func optStatusQuery(c *echo.Context) (int, error) {
	raw := strings.TrimSpace(c.QueryParam("status"))
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || (n != domain.ExamStatusActive && n != domain.ExamStatusDeleted) {
		return 0, fmt.Errorf("invalid status: %s", raw)
	}
	return n, nil
}

// parseLimitQuery parses the list limit: default 20; non-numeric / < 1 / > 100 all error.
func parseLimitQuery(c *echo.Context) (int, error) {
	raw := strings.TrimSpace(c.QueryParam("limit"))
	if raw == "" {
		return listLimitDefault, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid limit: %s", raw)
	}
	if n < 1 || n > listLimitMax {
		return 0, fmt.Errorf("limit out of range (1-%d): %d", listLimitMax, n)
	}
	return n, nil
}

// parseCursorQuery parses the keyset cursor: default 0 (newest first); non-numeric or < 0 errors.
func parseCursorQuery(c *echo.Context) (int64, error) {
	raw := strings.TrimSpace(c.QueryParam("cursor"))
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor: %s", raw)
	}
	if n < 0 {
		return 0, fmt.Errorf("cursor must be >= 0: %d", n)
	}
	return n, nil
}

// optGradeQuery parses the grade filter: empty string = no filter; non-numeric /
// out of range (< 1 or > 12) errors.
func optGradeQuery(c *echo.Context) (*int, error) {
	raw := strings.TrimSpace(c.QueryParam("grade"))
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid grade: %s", raw)
	}
	if n < domain.GradeMin || n > domain.GradeMax {
		return nil, fmt.Errorf("grade out of range (%d-%d): %d", domain.GradeMin, domain.GradeMax, n)
	}
	return &n, nil
}

// optExamTypeQuery parses the exam_type filter: empty string = no filter;
// non-numeric / not in the enum errors.
func optExamTypeQuery(c *echo.Context) (*int, error) {
	raw := strings.TrimSpace(c.QueryParam("exam_type"))
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid exam_type: %s", raw)
	}
	if !isValidExamType(n) {
		return nil, fmt.Errorf("exam_type out of range: %d", n)
	}
	return &n, nil
}

// isValidExamType reports whether t is a valid exam_type enum value (the full
// domain.ExamType* set).
func isValidExamType(t int) bool {
	switch t {
	case domain.ExamTypeWeekly, domain.ExamTypeMonthly, domain.ExamTypeMidterm,
		domain.ExamTypeFinal, domain.ExamTypeMock, domain.ExamTypeSpecial,
		domain.ExamTypeOther:
		return true
	default:
		return false
	}
}

// intParam reads an integer query parameter, falling back to the default when invalid or missing.
func intParam(c *echo.Context, name string, def int) int {
	v := c.QueryParam(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
