package service

import (
	"context"
	"fmt"
	"strings"

	"paperly/internal/domain"
)

// MessagePage is one page of the notification list (keyset cursor).
type MessagePage struct {
	Items      []*domain.Message
	NextCursor int64
	HasMore    bool
}

// MessageService is the business layer for the notification center (messages table).
//
// The recipient boundary is the (Audience, KidID) pair: parents are family-wide
// (KidID=0), kids must be bound to a specific KidID. It is enforced here — the DB's
// CHECK constraint only backstops writes, and omitting kid_id on the read side is a
// detectable authorization leak.
type MessageService struct {
	repo domain.MessageRepository
}

// NewMessageService constructs the notification service.
//
// No clock is injected: messages.created_at is written by SQLite CURRENT_TIMESTAMP,
// and the service never generates timestamps itself.
func NewMessageService(repo domain.MessageRepository) *MessageService {
	return &MessageService{repo: repo}
}

// audienceOf validates and normalizes the recipient boundary.
//
// Rules (mirroring the messages CHECK constraints):
//   - audience=1 (parent): kidID must be 0; a non-zero value means the caller
//     misunderstood the recipient semantics.
//   - audience=2 (kid): kidID must be > 0.
func (s *MessageService) audienceOf(audience int, kidID int64) error {
	switch audience {
	case domain.MessageAudienceParent:
		if kidID != 0 {
			return fmt.Errorf("%w: parent message must not carry kid_id (kid_id=%d)", domain.ErrInvalidInput, kidID)
		}
		return nil
	case domain.MessageAudienceKid:
		if kidID <= 0 {
			return fmt.Errorf("%w: kid message requires kid_id (audience=%d)", domain.ErrInvalidInput, audience)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown audience %d", domain.ErrInvalidInput, audience)
	}
}

// normalizeLimit clamps the page size.
//
// The cap is domain.MessageListLimit: the bell dropdown shows at most this many,
// so inputs like limit=10000 cannot pull the whole table into memory.
func normalizeLimit(limit int) int {
	switch {
	case limit <= 0:
		return domain.MessageListLimit
	case limit > domain.MessageListLimit:
		return domain.MessageListLimit
	default:
		return limit
	}
}

// List returns one page of notifications under the recipient boundary (it fetches
// one extra row to detect has_more).
func (s *MessageService) List(
	ctx context.Context,
	audience int,
	kidID int64,
	cursor int64,
	limit int,
	unreadOnly bool,
) (*MessagePage, error) {
	if err := s.audienceOf(audience, kidID); err != nil {
		return nil, err
	}
	if cursor < 0 {
		return nil, fmt.Errorf("%w: cursor must not be negative (cursor=%d)", domain.ErrInvalidInput, cursor)
	}

	page := normalizeLimit(limit)
	rows, err := s.repo.List(ctx, domain.ListMessagesFilter{
		Audience:   audience,
		KidID:      kidID,
		UnreadOnly: unreadOnly,
		Cursor:     cursor,
		Limit:      page + 1, // fetch one extra: the last row's presence means has_more
	})
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	out := &MessagePage{Items: rows, NextCursor: 0, HasMore: false}
	if len(rows) > page {
		out.HasMore = true
		out.Items = rows[:page]
	}
	if out.HasMore && len(out.Items) > 0 {
		last := out.Items[len(out.Items)-1]
		out.NextCursor = last.ID
	}
	return out, nil
}

// UnreadCount returns the unread count under the recipient boundary (bell badge).
func (s *MessageService) UnreadCount(ctx context.Context, audience int, kidID int64) (int, error) {
	if err := s.audienceOf(audience, kidID); err != nil {
		return 0, err
	}
	n, err := s.repo.CountUnread(ctx, audience, kidID)
	if err != nil {
		return 0, fmt.Errorf("count unread messages: %w", err)
	}
	return n, nil
}

// MarkRead marks a single message read. Idempotent: marking the same message again
// does not error.
func (s *MessageService) MarkRead(ctx context.Context, id int64, audience int, kidID int64) error {
	if err := s.audienceOf(audience, kidID); err != nil {
		return err
	}
	if id <= 0 {
		return fmt.Errorf("%w: message id must be positive (id=%d)", domain.ErrInvalidInput, id)
	}
	if err := s.repo.MarkRead(ctx, id, audience, kidID); err != nil {
		return fmt.Errorf("mark message read: %w", err)
	}
	return nil
}

func (s *MessageService) MarkAllRead(ctx context.Context, audience int, kidID int64) (int64, error) {
	if err := s.audienceOf(audience, kidID); err != nil {
		return 0, err
	}
	n, err := s.repo.MarkAllRead(ctx, audience, kidID)
	if err != nil {
		return 0, fmt.Errorf("mark all messages read: %w", err)
	}
	return n, nil
}

// CreateParentNotice writes a parent notification (system-side writer only).
//
// Parent messages are family-level: kid_id stays NULL (the CHECK constraint). Kid
// notifications go through CreateKidNotice; kinds 5-7 still have no writer (reserved).
func (s *MessageService) CreateParentNotice(ctx context.Context, kind int, title, body, link string) (int64, error) {
	m := &domain.Message{
		Audience: domain.MessageAudienceParent,
		KidID:    nil, // CHECK constraint requires parent messages to carry no kid_id
		Kind:     kind,
		Title:    strings.TrimSpace(title),
	}
	if body != "" {
		m.Body = &body
	}
	if link != "" {
		m.Link = &link
	}
	if m.Title == "" {
		return 0, fmt.Errorf("%w: message title must not be empty", domain.ErrInvalidInput)
	}

	id, err := s.repo.Insert(ctx, m)
	if err != nil {
		return 0, fmt.Errorf("create parent message: %w", err)
	}
	return id, nil
}

// CreateKidNotice writes a kid notification bound to kidID (system-side writer only).
//
// The kid counterpart of CreateParentNotice: audience=2 with a non-null kid_id (the CHECK
// constraint), so kidID must be positive — audienceOf enforces it, and a bad id is an
// authoring bug rather than a silent no-op row.
func (s *MessageService) CreateKidNotice(ctx context.Context, kidID int64, kind int, title, body, link string) (int64, error) {
	if err := s.audienceOf(domain.MessageAudienceKid, kidID); err != nil {
		return 0, err
	}
	m := &domain.Message{
		Audience: domain.MessageAudienceKid,
		KidID:    &kidID,
		Kind:     kind,
		Title:    strings.TrimSpace(title),
	}
	if body != "" {
		m.Body = &body
	}
	if link != "" {
		m.Link = &link
	}
	if m.Title == "" {
		return 0, fmt.Errorf("%w: message title must not be empty", domain.ErrInvalidInput)
	}

	id, err := s.repo.Insert(ctx, m)
	if err != nil {
		return 0, fmt.Errorf("create kid message: %w", err)
	}
	return id, nil
}
