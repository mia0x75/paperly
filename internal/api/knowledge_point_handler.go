package api

import (
	"errors"

	"github.com/labstack/echo/v5"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	"paperly/internal/service"
)

// KnowledgePointHandler handles knowledge-point related requests.
//
// The routes hang under subjects: /api/subjects/:subjectId/knowledge_points.
// A knowledge point **must** belong to a subject (DB foreign key); hanging under
// /api/knowledge_points would force every request to carry a subject_id query
// param, and half of those are POST bodies — putting the ownership param in the
// body can be silently dropped, which is exactly the AGENTS pitfall "path/ownership
// params must be passed explicitly". Hence it always goes in the path.
type KnowledgePointHandler struct {
	svc *service.KnowledgePointService
}

func NewKnowledgePointHandler(svc *service.KnowledgePointService) *KnowledgePointHandler {
	return &KnowledgePointHandler{svc: svc}
}

func (h *KnowledgePointHandler) RegisterRoutes(g *echo.Group) {
	subj := g.Group("/subjects/:subjectId")
	subj.GET("/knowledge_points", h.List)
	subj.POST("/knowledge_points", h.Create)
	subj.PUT("/knowledge_points/:id", h.Update)
	subj.DELETE("/knowledge_points/:id", h.Delete)
}

func (h *KnowledgePointHandler) List(c *echo.Context) error {
	subjectID, err := parseSubjectIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid subject id", err.Error())
	}
	rows, err := h.svc.List(c.Request().Context(), subjectID)
	if err != nil {
		return failKnowledgePoint(c, err)
	}
	return ok(c, dto.NewKnowledgePointSummaries(rows))
}

// createKnowledgePointReq is the POST request body.
type createKnowledgePointReq struct {
	Name string `json:"name"`
}

func (h *KnowledgePointHandler) Create(c *echo.Context) error {
	subjectID, err := parseSubjectIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid subject id", err.Error())
	}
	var req createKnowledgePointReq
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	kp, err := h.svc.Create(c.Request().Context(), subjectID, req.Name)
	if err != nil {
		return failKnowledgePoint(c, err)
	}
	return ok(c, dto.NewKnowledgePointCreated(kp))
}

// updateKnowledgePointReq is the PUT request body.
type updateKnowledgePointReq struct {
	Name string `json:"name"`
}

func (h *KnowledgePointHandler) Update(c *echo.Context) error {
	subjectID, err := parseSubjectIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid subject id", err.Error())
	}
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid knowledge point id", err.Error())
	}
	var req updateKnowledgePointReq
	if err := c.Bind(&req); err != nil {
		return fail(c, CodeInvalidBody, "invalid request body", err.Error())
	}
	if err := h.svc.Update(c.Request().Context(), subjectID, id, req.Name); err != nil {
		return failKnowledgePoint(c, err)
	}
	return ok(c, nil)
}

func (h *KnowledgePointHandler) Delete(c *echo.Context) error {
	subjectID, err := parseSubjectIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid subject id", err.Error())
	}
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid knowledge point id", err.Error())
	}
	if err := h.svc.Delete(c.Request().Context(), subjectID, id); err != nil {
		return failKnowledgePoint(c, err)
	}
	return ok(c, nil)
}

// parseSubjectIDParam parses the :subjectId path param.
//
// parseIDParam cannot be reused: it reads a fixed :id, while the param here is
// named subjectId, so reusing it would always get an empty string and then report
// "id invalid", which is very hard to locate.
func parseSubjectIDParam(c *echo.Context) (int64, error) {
	id, err := parseNamedIDParam(c, "subjectId")
	if err != nil {
		return 0, err
	}
	return id, nil
}

func failKnowledgePoint(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return fail(c, CodeNotFound, "knowledge point or subject not found", err.Error())
	case errors.Is(err, domain.ErrInvalidInput):
		return fail(c, CodeInvalidInput, err.Error(), "")
	default:
		return fail(c, CodeInternal, "knowledge point operation failed", err.Error())
	}
}
