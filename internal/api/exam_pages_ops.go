package api

import (
	"fmt"
	"strconv"
	"strings"

	"encoding/json"
	"github.com/labstack/echo/v5"
	"net/http"

	"paperly/internal/api/dto"
	"paperly/internal/domain"
	httpmw "paperly/internal/http/middleware"
	"paperly/internal/service"
)

// PagesOps handles POST /api/exams/:id/pages/ops (page-level ops: delete + add
// + reorder, one task per request).
//
// Multipart fields: operations (JSON string, see pagesOperations) + optional
// file fields (converted to WebP one by one in final_order's new_N order). On
// success it returns 202 {task_id, exam_id} — the DB page order is in place; disk
// moves are done asynchronously by the kind=5 task, and the frontend then polls the
// detail page.
func (h *ExamHandler) PagesOps(c *echo.Context) error {
	if err := h.acquireUploadSlot(c); err != nil {
		return err
	}
	if h.gate != nil {
		defer h.gate.Release()
	}
	id, err := parseIDParam(c)
	if err != nil {
		return fail(c, CodeValidation, "invalid exam id", err.Error())
	}
	aud := httpmw.AudienceFromCtx(c)
	if aud == httpmw.AudienceKid {
		// Ownership scope is the same as Delete: not one's own exam → 404.
		kidID := httpmw.KidIDFromCtx(c)
		if old, _, e := h.svc.GetDetail(c.Request().Context(), id); e == nil {
			if old.KidID == nil || *old.KidID != kidID {
				return mapExamError(c, domain.ErrNotFound)
			}
		}
	}

	form, err := c.MultipartForm()
	if err != nil {
		return fail(c, CodeInvalidBody, "invalid multipart form", err.Error())
	}
	opsRaw := form.Value["operations"]
	if len(opsRaw) == 0 || opsRaw[0] == "" {
		return fail(c, CodeValidation, "missing operations field", "")
	}
	ops, err := parsePagesOperations(opsRaw[0])
	if err != nil {
		return fail(c, CodeValidation, "invalid operations", err.Error())
	}

	files := make([]service.PageFile, 0, len(form.File["file"]))
	for _, fh := range form.File["file"] {
		pf, err := readUploadFile(c, fh)
		if err != nil {
			return err
		}
		files = append(files, pf)
	}

	taskID, err := h.svc.PagesOps(c.Request().Context(), service.PagesOpsReq{
		ExamID:        id,
		DeletePageIDs: ops.DeletePageIDs,
		FinalOrder:    ops.FinalOrder,
		Files:         files,
	})
	if err != nil {
		return mapExamError(c, err)
	}
	return c.JSON(http.StatusAccepted, response{Code: CodeOK, Data: dto.PageOpsResponse{TaskID: taskID, ExamID: id}})
}

// pagesOperations is the parsed form of the operations form field.
//
// Each final_order item is distinguished by JSON native type: number = an existing
// page id (normalized internally to a decimal string), string = a "new_N"
// placeholder page. delete_page_ids is always a number array.
type pagesOperations struct {
	DeletePageIDs []int64
	FinalOrder    []string
}

// parsePagesOperations parses the operations string. After normalization it hands
// off to the service for unified validation (existence/duplicate/permutation
// constraints live in the service layer, 40010);
// here only shape and types are handled.
func parsePagesOperations(raw string) (*pagesOperations, error) {
	var in struct {
		DeletePageIDs []int64           `json:"delete_page_ids"`
		FinalOrder    []json.RawMessage `json:"final_order"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, fmt.Errorf("bad operations json: %w", err)
	}
	out := &pagesOperations{
		DeletePageIDs: in.DeletePageIDs,
		FinalOrder:    make([]string, 0, len(in.FinalOrder)),
	}
	for _, item := range in.FinalOrder {
		// Distinguish type by the first character rather than Try-ing each: shapes that are neither
		// number nor string (JSON null/true/array, etc.) must be rejected here (null decoding into
		// a string is a silent no-op and would leak through as an empty string to the service).
		trimmed := strings.TrimSpace(string(item))
		if strings.HasPrefix(trimmed, `"`) {
			var s string
			if err := json.Unmarshal(item, &s); err != nil {
				return nil, fmt.Errorf("bad final_order string item: %w", err)
			}
			out.FinalOrder = append(out.FinalOrder, s)
			continue
		}
		var n int64
		if err := json.Unmarshal(item, &n); err != nil {
			return nil, fmt.Errorf("final_order item must be page id or \"new_N\", got: %s", trimmed)
		}
		out.FinalOrder = append(out.FinalOrder, strconv.FormatInt(n, 10))
	}
	return out, nil
}
