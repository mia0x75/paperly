package dto

import (
	"time"

	"paperly/internal/domain"
)

// MessageSummary is a GET /api/messages list item (Summary view).
//
// The bell dropdown shows "title + type + when + read/unread"; body stays out (it would bloat the
// dropdown), but link is included so a click can jump straight to the relevant page
// (e.g. the grade-9 reminder links to /settings/subjects). Business identifier is title
// (messages has no status column).
type MessageSummary struct {
	ID        int64     `json:"id"`
	Kind      int       `json:"kind"`
	Title     string    `json:"title"`
	Link      string    `json:"link"`
	IsRead    bool      `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`
}

// MessageListResponse is the response body for GET /api/messages.
//
// Keyset cursor pagination (ADR 0003): no total returned, next_cursor=0 ⟺ has_more=false.
type MessageListResponse struct {
	Items      []MessageSummary `json:"items"`
	NextCursor int64            `json:"next_cursor"`
	HasMore    bool             `json:"has_more"`
}

// MessageUnreadCountResponse is the response body for GET /api/messages/unread-count.
//
// The badge is its own endpoint rather than being stuffed into the list response: the bell shows on
// every page, and fetching the whole page of notifications just for the badge would be "downloading
// 50 records for 2 characters".
type MessageUnreadCountResponse struct {
	UnreadCount int `json:"unread_count"`
}

// MessageMarkAllReadResponse is the response body for POST /api/messages/read-all.
//
// Updated is the number of messages actually flipped from unread to read this time (not the total in
// the database) — it will be 0 when "mark all read" is tapped again, so the frontend need not re-prompt.
type MessageMarkAllReadResponse struct {
	Updated int64 `json:"updated"`
}

// NewMessageSummaries maps list items in batch.
func NewMessageSummaries(rows []*domain.Message) []MessageSummary {
	items := make([]MessageSummary, 0, len(rows))
	for _, m := range rows {
		items = append(items, NewMessageSummary(m))
	}
	return items
}

// NewMessageSummary maps a single list item.
//
// Link uses an empty string rather than null: the frontend can then treat ""
// as "not navigable" without a null check.
func NewMessageSummary(m *domain.Message) MessageSummary {
	s := MessageSummary{
		ID:        m.ID,
		Kind:      m.Kind,
		Title:     m.Title,
		IsRead:    m.IsRead,
		CreatedAt: m.CreatedAt,
	}
	if m.Link != nil {
		s.Link = *m.Link
	}
	return s
}

// NewMessageListResponse assembles the list response.
func NewMessageListResponse(items []*domain.Message, nextCursor int64, hasMore bool) MessageListResponse {
	return MessageListResponse{
		Items:      NewMessageSummaries(items),
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}
}
