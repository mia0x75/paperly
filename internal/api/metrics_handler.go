package api

import (
	"strconv"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	"paperly/internal/service"
)

// MetricsHandler provides the home-page aggregated metrics.
//
// The gate is assembled by the main program and mounted on each route (the AuthHandler pattern):
// summary is a parent-only ops view (requireParent), storage-trend is shown on both home pages (requireAny).
type MetricsHandler struct {
	svc           *service.MetricsService
	requireAny    echo.MiddlewareFunc
	requireParent echo.MiddlewareFunc
}

// NewMetricsHandler constructs the metrics HTTP handler. Middleware is assembled by the main
// program via middleware.RequireAnyAudience / RequireAudience.
func NewMetricsHandler(svc *service.MetricsService, requireAny, requireParent echo.MiddlewareFunc) *MetricsHandler {
	return &MetricsHandler{svc: svc, requireAny: requireAny, requireParent: requireParent}
}

// RegisterRoutes registers the metrics routes.
func (h *MetricsHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/metrics/summary", h.Summary, h.requireParent)
	g.GET("/metrics/storage-trend", h.StorageTrend, h.requireAny)
}

// StorageTrend handles GET /api/metrics/storage-trend?days=30.
//
// Storage growth trend (data source of the Dashboard top-row 4th card): reads the gauge history of the
// metrics table, not a live reading. days defaults to 30, illegal returns 30, out-of-range clamps into
// [1, 365] — gauge rows are only kept for domain.MetricGaugeRetentionDays days, so a longer window has
// no data on the backend either.
func (h *MetricsHandler) StorageTrend(c *echo.Context) error {
	days := 30
	if raw := c.QueryParam("days"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			switch {
			case n < 1:
				days = 1
			case n > domain.MetricGaugeRetentionDays:
				days = domain.MetricGaugeRetentionDays
			default:
				days = n
			}
		}
	}

	points, err := h.svc.StorageTrend(c.Request().Context(), days)
	if err != nil {
		return fail(c, CodeInternal, "failed to read storage trend", err.Error())
	}
	out := make([]dto.StorageTrendPointSummary, 0, len(points))
	for _, p := range points {
		out = append(out, dto.StorageTrendPointSummary{
			Date:         p.Date,
			DBSizeKB:     p.DBKB,
			AssetsSizeKB: p.AssetsKB,
		})
	}
	return ok(c, dto.StorageTrendResponse{Days: days, Points: out})
}

// Summary handles GET /api/metrics/summary.
//
// A single endpoint returns the home-page **truth** cards: database / storage / system health.
func (h *MetricsHandler) Summary(c *echo.Context) error {
	ctx := c.Request().Context()

	db, err := h.svc.DatabaseInfo(ctx)
	if err != nil {
		return fail(c, CodeInternal, "failed to read database info", err.Error())
	}

	return ok(c, dto.NewMetricsSummaryResponse(
		db,
		h.svc.Storage(),
		h.svc.HealthReport(ctx),
	))
}
