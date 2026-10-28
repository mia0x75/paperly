package repository

import (
	"context"
	"fmt"

	"paperly/internal/domain"
)

// Explicit column list; SELECT * is forbidden (AGENTS.md SQLite rules).
const messageColumns = `id, audience, kid_id, kind, title, body, link, is_read, read_at, created_at`

// Enforces recipient boundary (audience + kid_id) in all queries.
type MessageRepoSQLite struct {
	baseRepo
}

var _ domain.MessageRepository = (*MessageRepoSQLite)(nil)

func NewMessageRepoSQLite(db *DB) *MessageRepoSQLite {
	return &MessageRepoSQLite{baseRepo: baseRepo{db: db}}
}

// WithTx returns a shallow copy bound to tx; its statements then run inside that transaction.
func (r *MessageRepoSQLite) WithTx(tx any) domain.MessageRepository {
	return &MessageRepoSQLite{baseRepo: r.mustBindTx(tx)}
}

// Uses the write pool. created_at comes from the DB's CURRENT_TIMESTAMP default so
// ordering and creation share one clock (server/DB clocks may differ).
func (r *MessageRepoSQLite) Insert(ctx context.Context, m *domain.Message) (int64, error) {
	const q = `INSERT INTO messages
		(audience, kid_id, kind, title, body, link, is_read, read_at)
		VALUES (:audience, :kid_id, :kind, :title, :body, :link, :is_read, :read_at)`
	res, err := r.writeExt().NamedExecContext(ctx, q, m)
	if err != nil {
		return 0, fmt.Errorf("insert message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("message last insert id: %w", err)
	}
	return id, nil
}

// The parent side also asserts kid_id IS NULL: the DB CHECK already guarantees this, but
// repeating it here keeps the privilege-escalation path unreachable even if a future caller
// bypasses the service and hits the repository directly.
func audienceWhere(audience int, kidID int64) (string, []any) {
	if audience == domain.MessageAudienceKid {
		return `audience = ? AND kid_id = ?`, []any{audience, kidID}
	}
	return `audience = ? AND kid_id IS NULL`, []any{audience}
}

// Returns at most Limit+1 rows so the caller can derive next_cursor from the last row's id.
// Ordered id DESC (keyset cursor direction); uses the two partial indexes
// idx_messages_parent / idx_messages_kid. Uses the read pool.
func (r *MessageRepoSQLite) List(ctx context.Context, filter domain.ListMessagesFilter) ([]*domain.Message, error) {
	where, args := audienceWhere(filter.Audience, filter.KidID)
	if filter.UnreadOnly {
		where += ` AND is_read = 0`
	}
	if filter.Cursor > 0 {
		where += ` AND id < ?`
		args = append(args, filter.Cursor)
	}

	q := `SELECT ` + messageColumns + ` FROM messages WHERE ` + where + ` ORDER BY id DESC LIMIT ?`
	args = append(args, filter.Limit)

	var out []*domain.Message
	if err := r.readExt().SelectContext(ctx, &out, q, args...); err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	if out == nil {
		out = make([]*domain.Message, 0)
	}
	return out, nil
}

// CountUnread backs the bell badge. Uses the read pool.
func (r *MessageRepoSQLite) CountUnread(ctx context.Context, audience int, kidID int64) (int, error) {
	where, args := audienceWhere(audience, kidID)
	q := `SELECT COUNT(*) FROM messages WHERE ` + where + ` AND is_read = 0`
	var n int
	if err := r.readExt().GetContext(ctx, &n, q, args...); err != nil {
		return 0, fmt.Errorf("count unread messages: %w", err)
	}
	return n, nil
}

// Uses the write pool. WHERE carries audience + kid_id so kid A cannot mark kid B's message.
// The is_read = 0 guard makes an already-read row match nothing (RowsAffected=0 → ErrNotFound),
// which is treated as success so repeated clicks stay idempotent.
func (r *MessageRepoSQLite) MarkRead(ctx context.Context, id int64, audience int, kidID int64) error {
	where, args := audienceWhere(audience, kidID)
	q := `UPDATE messages SET is_read = 1, read_at = CURRENT_TIMESTAMP
		WHERE id = ? AND ` + where + ` AND is_read = 0`
	res, err := r.writeExt().ExecContext(ctx, q, append([]any{id}, args...)...)
	if err != nil {
		return fmt.Errorf("mark message read: %w", err)
	}
	if err := affectedOrNotFound(res, "message"); err != nil {
		return nil
	}
	return nil
}

// Returns the affected row count. Uses the write pool. Parent notifications are read at
// family level: once any parent reads one, the whole family has read it.
func (r *MessageRepoSQLite) MarkAllRead(ctx context.Context, audience int, kidID int64) (int64, error) {
	where, args := audienceWhere(audience, kidID)
	q := `UPDATE messages SET is_read = 1, read_at = CURRENT_TIMESTAMP WHERE ` + where + ` AND is_read = 0`
	res, err := r.writeExt().ExecContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("mark all messages read: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("message rows affected: %w", err)
	}
	return n, nil
}

// Delete physically removes one notification. Uses the write pool.
//
// WHERE carries the recipient boundary (audience + kid_id), so a kid cannot delete another
// kid's notification and the parent side cannot delete a kid-side row. No match (wrong
// boundary / already deleted / missing id) -> ErrNotFound: the caller turns that into 404.
func (r *MessageRepoSQLite) Delete(ctx context.Context, id int64, audience int, kidID int64) error {
	where, args := audienceWhere(audience, kidID)
	q := `DELETE FROM messages WHERE id = ? AND ` + where
	res, err := r.writeExt().ExecContext(ctx, q, append([]any{id}, args...)...)
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}
	return affectedOrNotFound(res, "message")
}
