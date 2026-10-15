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

// MessageHandler provides the **parent-side** notification center (messages table).
//
// The recipient boundary is "family-level": parent notifications are always
// audience=1, kid_id=NULL, and all parents see the same list and the same
// read-state. Hence no kid_id parameter is accepted here — kid-side notifications
// are viewed by the frontend uniformly through this interface.
type MessageHandler struct {
	svc *service.MessageService
}

func NewMessageHandler(svc *service.MessageService) *MessageHandler {
	return &MessageHandler{svc: svc}
}

// RegisterRoutes registers the parent-side notification routes.
//
// /unread-count and /read-all are **static segments** and must coexist with
// /:id/read: in Echo's route tree static segments take precedence over parameter
// segments, so neither swallows the other.
func (h *MessageHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/messages", h.List)
	g.GET("/messages/unread-count", h.UnreadCount)
	g.POST("/messages/read-all", h.MarkAllRead)
	g.POST("/messages/:id/read", h.MarkRead)
}

// List handles GET /api/messages.
//
// Query: cursor (keyset cursor, 0/default = newest first), limit (default 20), unread_only.
// cursor / limit match /api/exams: illegal values are always 400, only defaults use the default.
func (h *MessageHandler) List(c *echo.Context) error {
	unreadOnly, err := parseBoolParam(c.QueryParam("unread_only"))
	if err != nil {
		return fail(c, CodeValidation, err.Error(), "unread_only="+c.QueryParam("unread_only"))
	}
	cursor, err := parseCursorQuery(c)
	if err != nil {
		return fail(c, CodeValidation, err.Error(), c.QueryParam("cursor"))
	}
	limit, err := parseLimitQuery(c)
	if err != nil {
		return fail(c, CodeValidation, err.Error(), c.QueryParam("limit"))
	}

	aud := httpmw.AudienceFromCtx(c)
	kidID := int64(0)
	if aud == httpmw.AudienceKid {
		kidID = httpmw.KidIDFromCtx(c)
		audDom := domain.MessageAudienceKid
		page, err := h.svc.List(
			c.Request().Context(),
			audDom, kidID,
			cursor,
			limit,
			unreadOnly,
		)
		if err != nil {
			return mapMessageError(c, err)
		}
		return ok(c, dto.NewMessageListResponse(page.Items, page.NextCursor, page.HasMore))
	}
	page, err := h.svc.List(
		c.Request().Context(),
		domain.MessageAudienceParent, 0,
		cursor,
		limit,
		unreadOnly,
	)
	if err != nil {
		return mapMessageError(c, err)
	}
	return ok(c, dto.NewMessageListResponse(page.Items, page.NextCursor, page.HasMore))
}

// UnreadCount handles GET /api/messages/unread-count (the bell's red badge).
func (h *MessageHandler) UnreadCount(c *echo.Context) error {
	aud := httpmw.AudienceFromCtx(c)
	if aud == httpmw.AudienceKid {
		kidID := httpmw.KidIDFromCtx(c)
		n, err := h.svc.UnreadCount(c.Request().Context(), domain.MessageAudienceKid, kidID)
		if err != nil {
			return mapMessageError(c, err)
		}
		return ok(c, dto.MessageUnreadCountResponse{UnreadCount: n})
	}
	n, err := h.svc.UnreadCount(c.Request().Context(), domain.MessageAudienceParent, 0)
	if err != nil {
		return mapMessageError(c, err)
	}
	return ok(c, dto.MessageUnreadCountResponse{UnreadCount: n})
}

// MarkRead handles POST /api/messages/:id/read (idempotent; re-marking does not error).
func (h *MessageHandler) MarkRead(c *echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return fail(c, CodeValidation, "invalid message id", c.Param("id"))
	}
	aud := httpmw.AudienceFromCtx(c)
	kidID := int64(0)
	if aud == httpmw.AudienceKid {
		kidID = httpmw.KidIDFromCtx(c)
	}
	audDom := domain.MessageAudienceParent
	if aud == httpmw.AudienceKid {
		audDom = domain.MessageAudienceKid
	}
	if err := h.svc.MarkRead(c.Request().Context(), id, audDom, kidID); err != nil {
		return mapMessageError(c, err)
	}
	return ok(c, map[string]any{"id": id})
}

func (h *MessageHandler) MarkAllRead(c *echo.Context) error {
	aud := httpmw.AudienceFromCtx(c)
	kidID := int64(0)
	audDom := domain.MessageAudienceParent
	if aud == httpmw.AudienceKid {
		kidID = httpmw.KidIDFromCtx(c)
		audDom = domain.MessageAudienceKid
	}
	n, err := h.svc.MarkAllRead(c.Request().Context(), audDom, kidID)
	if err != nil {
		return mapMessageError(c, err)
	}
	return ok(c, dto.MessageMarkAllReadResponse{Updated: n})
}

func mapMessageError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		return fail(c, CodeInvalidInput, err.Error(), "")
	case errors.Is(err, domain.ErrNotFound):
		return fail(c, CodeNotFound, "message not found", "")
	default:
		return fail(c, CodeInternal, "failed to read message", "")
	}
}
