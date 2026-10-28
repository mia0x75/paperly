package domain

import (
	"context"
	"time"
)

// Notification center (messages table).
//
// Orthogonal to tasks: tasks are things the system must do (worker), messages are things people
// need to know. Only two recipient types — parents (family-level, all parents share one message)
// and kids (bound to a kid_id). No user_id: parent permissions are uniform within a family, so
// "you" vs "me" is unnecessary.

// MessageAudience (stored as INTEGER).
const (
	// MessageAudienceParent: shared by the whole family, kid_id must be NULL.
	MessageAudienceParent = 1
	// MessageAudienceKid: bound to a kid_id.
	MessageAudienceKid = 2
)

// MessageKind (no CHECK constraint in the DB; the service validates).
//
// Kinds 5~7 have no writer yet (reserved), 99 is the fallback. Adding a kind needs no schema change.
const (
	MessageKindGradeAdvanced   = 1  // parent: grade auto-advanced
	MessageKindBackupDone      = 2  // parent: backup completed
	MessageKindBackupFailed    = 3  // parent: backup failed
	MessageKindAIReviewPending = 4  // parent: N AI items pending review
	MessageKindStorageCleaned  = 5  // parent: storage cleanup completed (reserved)
	MessageKindGapResolved     = 6  // kid: overcame a weak point (reserved)
	MessageKindReviewDue       = 7  // kid: N wrong questions due for review (reserved)
	MessageKindGradeChanged    = 8  // kid: grade updated (written by the grade-advance worker)
	MessageKindGrade9Reminder  = 9  // parent: kid reached grade 9, hide finished subjects
	MessageKindGrade10Reminder = 10 // parent: kid reached grade 10 (senior high), set arts/science track
	MessageKindGrade7Reminder  = 11 // parent: kid reached grade 7 (junior high), verify subjects are complete
	MessageKindOther           = 99 // parent: other (fallback)
)

// MessageListLimit caps the page size: the bell dropdown needs only the most recent handful, and it
// stops a limit=10000 from dragging the whole table into memory.
const MessageListLimit = 50

// Message corresponds to the messages table.
//
// Nullable fields are pointers (1:1 with SQL NULL): kid_id is NULL on parent notifications, and
// body/link/read_at may be absent too — a zero value would blur "empty" and "absent".
type Message struct {
	ID        int64      `db:"id"`
	Audience  int        `db:"audience"`
	KidID     *int64     `db:"kid_id"`
	Kind      int        `db:"kind"`
	Title     string     `db:"title"`
	Body      *string    `db:"body"`
	Link      *string    `db:"link"`
	IsRead    bool       `db:"is_read"`
	ReadAt    *time.Time `db:"read_at"`
	CreatedAt time.Time  `db:"created_at"`
}

// ListMessagesFilter is the notification-list filter.
//
// Audience + KidID is the recipient boundary, not an optional display filter: the repository pushes
// them into WHERE (via one of the two partial indexes) and the service validates the pairing — a kid
// side omitting KidID would show others' notifications.
type ListMessagesFilter struct {
	// Audience is required, only MessageAudienceParent / MessageAudienceKid.
	Audience int
	// KidID is required only when audience=MessageAudienceKid (>0).
	KidID int64
	// UnreadOnly shows only unread.
	UnreadOnly bool
	// Cursor is keyset: rows with id < Cursor; 0 = from the newest.
	Cursor int64
	// Limit is the page size; the service narrows it when <=0 or >MessageListLimit.
	Limit int
}

// MessageRepository is the messages data-access contract.
type MessageRepository interface {
	// Insert writes a notification, returning the auto-increment id. Uses the write pool.
	Insert(ctx context.Context, m *Message) (int64, error)
	// List returns notifications for the recipient boundary, id DESC. At most Limit+1 rows, so the
	// caller can detect has_more.
	List(ctx context.Context, filter ListMessagesFilter) ([]*Message, error)
	// CountUnread counts unread notifications for that boundary (for the badge).
	CountUnread(ctx context.Context, audience int, kidID int64) (int, error)
	// MarkRead marks one notification read; no match (unauthorized or missing) -> ErrNotFound.
	MarkRead(ctx context.Context, id int64, audience int, kidID int64) error
	// MarkAllRead marks all unread notifications for that boundary read, returning the affected count.
	// Parent notifications are family-level: any parent reading marks it read for the whole family.
	MarkAllRead(ctx context.Context, audience int, kidID int64) (int64, error)
	// Delete physically removes one notification inside the recipient boundary; no match
	// (unauthorized or missing) -> ErrNotFound.
	//
	// Physical delete (not a soft delete): a notification carries no business state beyond
	// read/unread, so a tombstone would only bloat the table and complicate the keyset cursor.
	Delete(ctx context.Context, id int64, audience int, kidID int64) error
	// WithTx returns a new instance bound to tx.
	WithTx(tx Tx) MessageRepository
}
