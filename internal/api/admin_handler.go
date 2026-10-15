package api

import (
	"fmt"
	"strings"

	"github.com/labstack/echo/v5"

	"paperly/internal/backup"
	"paperly/internal/health"
	"paperly/internal/service"
)

// AdminHandler hosts ops endpoints (backup / health check / seed).
//
// It holds concrete types rather than interfaces: ops is a single-instance,
// single-caller concern, so an interface would only add a layer of unearned
// indirection. Tests isolate by substituting the underlying domain repository
// rather than this layer.
type AdminHandler struct {
	backup  *backup.Service
	health  *health.Service
	ops     *service.SystemOpsService
	dataDir string
}

// NewAdminHandler constructs the ops HTTP handler.
func NewAdminHandler(
	backupSvc *backup.Service,
	healthSvc *health.Service,
	opsSvc *service.SystemOpsService,
	dataDir string,
) *AdminHandler {
	return &AdminHandler{
		backup:  backupSvc,
		health:  healthSvc,
		ops:     opsSvc,
		dataDir: dataDir,
	}
}

// RegisterRoutes registers the ops routes.
//
// Five routes, which expand to five callable endpoints:
//
//	POST /api/admin/backup                              trigger one online backup
//	GET  /api/admin/backups                             backup list + last backup time
//	GET  /api/admin/health                              data health check
//	POST /api/admin/seed/ensure                         fill in the runtime seed directory
//	GET  /api/admin/seed                                built-in seed manifest
func (h *AdminHandler) RegisterRoutes(g *echo.Group) {
	admin := g.Group("/admin")

	// Backup.
	admin.POST("/backup", h.CreateBackup)
	admin.GET("/backups", h.ListBackups)

	// Data health check.
	admin.GET("/health", h.Health)

	// First-start seed safeguard.
	admin.POST("/seed/ensure", h.EnsureSeed)
	admin.GET("/seed", h.SeedInfo)

	// Migration list and task cursors (settings page "migrations & cursors" block, read-only).
	admin.GET("/migrations", h.Migrations)
	admin.GET("/cursors", h.Cursors)

	// Backup export (tar.gz). Download streams directly, no temp file.
	admin.GET("/backups/:file_name/download", h.DownloadBackup)
}

// parseBoolParam parses a boolean query parameter (accepts 1/true/yes/on,
// empty = false; case-insensitive).
//
// Case-insensitivity matters: parents often copy "?dry_run=True" from
// Excel/documents, and a strict lowercase check would reject a normal operation
// as 400, costing far more in investigation than it is worth.
func parseBoolParam(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false, nil
	case "1", "true", "yes", "on":
		return true, nil
	}
	return false, fmt.Errorf("%q is not a valid boolean", v)
}

// sanitizeHeaderValue collapses CR/LF in a custom header value into a single
// space to prevent header injection.
//
// Collapsing (rather than replacing each character) is deliberate: CRLF is
// **one** line break, and replacing it with two spaces would introduce odd
// double spaces into "template description" text.
func sanitizeHeaderValue(v string) string {
	out := make([]rune, 0, len(v))
	prevBreak := false
	for _, r := range v {
		if r == '\r' || r == '\n' {
			if !prevBreak {
				out = append(out, ' ')
			}
			prevBreak = true
			continue
		}
		prevBreak = false
		out = append(out, r)
	}
	return string(out)
}
