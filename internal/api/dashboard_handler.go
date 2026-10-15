package api

import (
	"errors"
	"strconv"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/service"
)

// DashboardHandler provides the home-page aggregation endpoints scoped to **a single kid**.
//
// kidID must be given explicitly by the caller: the parent side passes it as a query param
// (defaulting to the first active kid), the kid side takes it from the JWT context.
type DashboardHandler struct {
	svc *service.DashboardService
}

func NewDashboardHandler(svc *service.DashboardService) *DashboardHandler {
	return &DashboardHandler{svc: svc}
}

// Overview handles GET /api/dashboard.
//
// Query: kid_id (required), trend_days (7 / 30, default 7).
//
// kidID goes in the query param rather than the path: dashboard is a view of "the
// currently selected kid", not a sub-resource of a kid; a path would imply
// /api/dashboard/:kid is a separate resource.
func (h *DashboardHandler) Overview(c *echo.Context) error {
	raw := c.QueryParam("kid_id")
	if raw == "" {
		return fail(c, CodeInvalidInput, "missing kid_id parameter", "")
	}
	kidID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || kidID <= 0 {
		return fail(c, CodeValidation, "kid_id must be a positive integer", "kid_id="+raw)
	}

	trendDays, err := parseTrendDays(c)
	if err != nil {
		return fail(c, CodeValidation, err.Error(), "trend_days="+c.QueryParam("trend_days"))
	}

	kid, err := h.svc.LoadKidForDashboard(c.Request().Context(), kidID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fail(c, CodeNotFound, "student not found or unavailable", "kid_id="+raw)
		}
		return fail(c, CodeInternal, "failed to read student", "")
	}

	view, err := h.svc.Build(c.Request().Context(), kid.ID, trendDays)
	if err != nil {
		return fail(c, CodeInternal, "failed to read dashboard", "")
	}

	return ok(c, dto.NewDashboardResponse(view, kidRefFrom(kid)))
}

// KidOverview handles GET /api/kid/dashboard (kid JWT auth).
//
// kidID comes only from the token claim injected by the auth middleware and does
// not accept a client parameter — the kid side is a credential-driven session,
// without the parent side's "switch kid" semantics. It uses the same Build as
// Overview, so the two sides share one measure scope and the kid view never
// carries a global denominator.
func (h *DashboardHandler) KidOverview(c *echo.Context) error {
	kid, err := h.svc.LoadKidForDashboard(c.Request().Context(), httpmw.KidIDFromCtx(c))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fail(c, CodeNotFound, "student not found or unavailable", "")
		}
		return fail(c, CodeInternal, "failed to read student", "")
	}

	trendDays, err := parseTrendDays(c)
	if err != nil {
		return fail(c, CodeValidation, err.Error(), "trend_days="+c.QueryParam("trend_days"))
	}

	view, err := h.svc.Build(c.Request().Context(), kid.ID, trendDays)
	if err != nil {
		return fail(c, CodeInternal, "failed to read dashboard", "")
	}

	return ok(c, dto.NewDashboardResponse(view, kidRefFrom(kid)))
}

// parseTrendDays parses the upload persistence window.
//
// An illegal value errors out directly rather than silently converging to 7: if the
// frontend sent a wrong value yet got 7 days of data, the user would think "I asked
// for 7 days" and never know the filter failed.
func parseTrendDays(c *echo.Context) (int, error) {
	raw := c.QueryParam("trend_days")
	if raw == "" {
		return service.TrendWindow7, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("trend_days must be an integer")
	}
	if n != service.TrendWindow7 && n != service.TrendWindow30 {
		return 0, errors.New("trend_days only supports 7 or 30")
	}
	return n, nil
}

// Persistence handles GET /api/dashboard/persistence (parent side).
//
// Returns the upload persistence series of all active kids, for the parent home
// page ROW2 "kids' persistence" overlaid line chart. kid_id is not part of this
// contract — it does not focus on one kid.
func (h *DashboardHandler) Persistence(c *echo.Context) error {
	trendDays, err := parseTrendDays(c)
	if err != nil {
		return fail(c, CodeValidation, err.Error(), "trend_days="+c.QueryParam("trend_days"))
	}

	items, err := h.svc.PersistenceForKids(c.Request().Context(), trendDays)
	if err != nil {
		return fail(c, CodeInternal, "failed to read persistence", "")
	}

	return ok(c, dto.NewPersistenceResponse(items))
}

// Distribution handles GET /api/dashboard/distribution (parent side).
//
// Query: kid_id (optional). Omitted = aggregation over **all kids** (the default
// scope of the parent home page "exam distribution" card); a positive integer =
// narrowed to that kid. An illegal kid_id is 400, consistent with Overview.
func (h *DashboardHandler) Distribution(c *echo.Context) error {
	var kidID *int64
	if raw := c.QueryParam("kid_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return fail(c, CodeValidation, "kid_id must be a positive integer", "kid_id="+raw)
		}
		kidID = &id
	}

	items, err := h.svc.Distribution(c.Request().Context(), kidID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fail(c, CodeNotFound, "student not found or unavailable", "kid_id="+c.QueryParam("kid_id"))
		}
		return fail(c, CodeInternal, "failed to read subject distribution", "")
	}

	return ok(c, dto.NewDistributionResponse(items))
}

// ScopeCounts handles GET /api/dashboard/counts (parent side).
//
// Query: kid_id (optional). Omitted = aggregation over **all kids** (the default
// scope of the parent home page top-row KPI cards); a positive integer = narrowed
// to that kid. An illegal kid_id is 400, consistent with Overview / Distribution.
// This lets the first three top-row cards fetch on demand for their own "all /
// one kid" filter without refetching the whole page aggregation.
func (h *DashboardHandler) ScopeCounts(c *echo.Context) error {
	var kidID *int64
	if raw := c.QueryParam("kid_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return fail(c, CodeValidation, "kid_id must be a positive integer", "kid_id="+raw)
		}
		kidID = &id
	}

	counts, err := h.svc.ScopeCounts(c.Request().Context(), kidID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fail(c, CodeNotFound, "student not found or unavailable", "kid_id="+c.QueryParam("kid_id"))
		}
		return fail(c, CodeInternal, "failed to read counts", "")
	}

	return ok(c, dto.NewScopeCountsResponse(counts))
}

// kidRefFrom projects a domain.Kid into the dashboard's kid reference.
// Shared by the parent and kid sides: the response must state "whose numbers these are",
// otherwise a parent switching kids has no way to tell whether the numbers changed.
func kidRefFrom(kid *domain.Kid) dto.KidRefSummary {
	return dto.KidRefSummary{ID: kid.ID, Name: kid.Name, Grade: kid.Grade, Status: kid.Status}
}

// The kid-side /kid/dashboard is registered by the main program after mounting the
// requireKid middleware (keeping this layer independent of auth).
func (h *DashboardHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/dashboard", h.Overview)
	// Parent-side "kids' persistence": all active kids fetched in one call
	// (see Persistence).
	g.GET("/dashboard/persistence", h.Persistence)
	// Parent-side "exam distribution" card: kid_id optional, omitted = all kids
	// aggregated (see Distribution).
	g.GET("/dashboard/distribution", h.Distribution)
	// Parent-side top-row KPI card counts: kid_id optional, omitted = all kids
	// aggregated (see ScopeCounts).
	g.GET("/dashboard/counts", h.ScopeCounts)
}
