package api

import (
	"github.com/labstack/echo/v5"

	"paperly/internal/seed"
)

// EnsureSeed handles POST /api/admin/seed/ensure.
func (h *AdminHandler) EnsureSeed(c *echo.Context) error {
	if _, err := seed.EnsureSeed(h.dataDir); err != nil {
		return fail(c, CodeInternal, "seed ensure failed", err.Error())
	}
	return h.SeedInfo(c)
}

// SeedInfo handles GET /api/admin/seed.
func (h *AdminHandler) SeedInfo(c *echo.Context) error {
	files, err := seed.BuiltinFiles()
	if err != nil {
		return fail(c, CodeInternal, "failed to read built-in seed list", err.Error())
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name)
	}
	return ok(c, map[string]any{"builtin_count": len(files), "builtin_files": names})
}
