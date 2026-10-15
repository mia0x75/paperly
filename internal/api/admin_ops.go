package api

import (
	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
)

func (h *AdminHandler) Migrations(c *echo.Context) error {
	rows, err := h.ops.SchemaMigrations(c.Request().Context())
	if err != nil {
		return fail(c, CodeInternal, "failed to read migration list", err.Error())
	}
	return ok(c, dto.NewSchemaMigrationSummarys(rows))
}

// Cursors handles GET /api/admin/cursors (persisted task cursor state).
func (h *AdminHandler) Cursors(c *echo.Context) error {
	rows, err := h.ops.Cursors(c.Request().Context())
	if err != nil {
		return fail(c, CodeInternal, "failed to read task cursors", err.Error())
	}
	return ok(c, dto.NewCursorSummarys(rows))
}

// Health handles GET /api/admin/health.
func (h *AdminHandler) Health(c *echo.Context) error {
	return ok(c, dto.NewHealthResponse(h.health.Check(c.Request().Context())))
}
