package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	"paperly/internal/service"
)

// TaskHandler handles requests under /api/tasks (background task ops).
//
// Read-only + retry + cleanup, no "execution": the Worker is not yet implemented,
// and retry only flips the status back to pending for the Worker to pick up.
type TaskHandler struct {
	svc *service.TaskService
}

func NewTaskHandler(svc *service.TaskService) *TaskHandler {
	return &TaskHandler{svc: svc}
}

// RegisterRoutes registers the task routes on the given group.
//
// The static segment clear-finished and :id do not conflict: Echo matches by segment,
// so "/tasks/clear-finished" never falls onto "/tasks/:id".
func (h *TaskHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/tasks", h.List)
	g.GET("/tasks/:id", h.Get)
	g.POST("/tasks/:id/retry", h.Retry)
	g.POST("/tasks/clear-finished", h.ClearFinished)
}

// List handles GET /api/tasks (keyset cursor pagination).
func (h *TaskHandler) List(c *echo.Context) error {
	cursor, err := int64Query(c, "cursor", 0)
	if err != nil {
		return fail(c, CodeValidation, "invalid cursor", err.Error())
	}
	limit, err := intQuery(c, "limit", 0)
	if err != nil {
		return fail(c, CodeValidation, "invalid limit", err.Error())
	}

	// status defaults to no filter. **status=0 must not be used as the "no filter" sentinel**:
	// 0 is a valid pending, so the frontend filtering "pending" would silently become "all".
	status, hasStatus, err := optionalStatusQuery(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid status", err.Error())
	}

	page, err := h.svc.List(c.Request().Context(), status, hasStatus, cursor, limit)
	if err != nil {
		return failTask(c, err)
	}
	return ok(c, dto.NewTaskListResponse(page.Items, page.NextCursor, page.HasMore, page.Counts))
}

// Get handles GET /api/tasks/:id (includes last_error / detail, not in the list).
func (h *TaskHandler) Get(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid task id", err.Error())
	}
	t, err := h.svc.GetTask(c.Request().Context(), id)
	if err != nil {
		return failTask(c, err)
	}
	return ok(c, dto.NewTaskDetail(t))
}

func (h *TaskHandler) Retry(c *echo.Context) error {
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid task id", err.Error())
	}
	t, err := h.svc.Retry(c.Request().Context(), id)
	if err != nil {
		return failTask(c, err)
	}
	return ok(c, dto.NewTaskDetail(t))
}

func (h *TaskHandler) ClearFinished(c *echo.Context) error {
	n, err := h.svc.ClearFinished(c.Request().Context())
	if err != nil {
		return failTask(c, err)
	}
	return ok(c, dto.TaskClearFinishedResponse{Deleted: n})
}

// optionalStatusQuery reads the optional status query param.
//
// Returns hasStatus=false to mean "param not given"; if given, it must parse into
// an integer enum — it cannot be silently treated as no filter, or a frontend typo
// would return all data and be mistaken for a filter.
func optionalStatusQuery(c *echo.Context) (status int, hasStatus bool, err error) {
	raw := c.QueryParam("status")
	if raw == "" {
		return 0, false, nil
	}
	v, perr := strconv.Atoi(raw)
	if perr != nil {
		return 0, false, perr
	}
	return v, true, nil
}

// int64Query reads an int64 query param (for the cursor), returning def when absent.
func int64Query(c *echo.Context, name string, def int64) (int64, error) {
	raw := c.QueryParam(name)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// intQuery reads an int query param (for the limit), returning def when absent.
func intQuery(c *echo.Context, name string, def int) (int, error) {
	raw := c.QueryParam(name)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func failTask(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fail(c, CodeNotFound, "task not found", err.Error())
	case errors.Is(err, domain.ErrInvalidInput):
		return fail(c, CodeInvalidInput, err.Error(), "")
	default:
		return fail(c, CodeInternal, "task operation failed", err.Error())
	}
}

var _ = http.StatusOK
