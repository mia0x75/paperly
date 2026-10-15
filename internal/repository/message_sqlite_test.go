package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

func i64(v int64) *int64 { return &v }

// newParentNotice builds a parent notice (audience=1, kid_id must be NULL).
func newParentNotice(kind int, title string) *domain.Message {
	return &domain.Message{
		Audience: domain.MessageAudienceParent,
		Kind:     kind,
		Title:    title,
	}
}

// newKidNotice builds a kid notice (audience=2, must be bound to kidID).
func newKidNotice(kidID int64, kind int, title string) *domain.Message {
	return &domain.Message{
		Audience: domain.MessageAudienceKid,
		KidID:    i64(kidID),
		Kind:     kind,
		Title:    title,
	}
}

// seedKid inserts a student, for messages' foreign key kid_id.
func seedKid(t *testing.T, db *DB, name string) int64 {
	t.Helper()
	kidID, err := NewKidRepoSQLite(db).Insert(context.Background(), newTestKid(name, "code"+name))
	require.NoError(t, err)
	return kidID
}

func TestMessageRepoInsertReadRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewMessageRepoSQLite(db)

	body := "如因休学/跳级不符，请到孩子档案修改。"
	link := "/kids/1"
	id, err := repo.Insert(ctx, &domain.Message{
		Audience: domain.MessageAudienceParent,
		Kind:     domain.MessageKindGradeAdvanced,
		Title:    "已自动升级到初二",
		Body:     &body,
		Link:     &link,
	})
	require.NoError(t, err)
	require.Positive(t, id)

	rows, err := repo.List(ctx, domain.ListMessagesFilter{
		Audience: domain.MessageAudienceParent,
		Limit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	got := rows[0]
	require.Equal(t, "已自动升级到初二", got.Title)
	require.Equal(t, body, *got.Body)
	require.Equal(t, link, *got.Link)
	require.False(t, got.IsRead, "新插入的通知必须是未读")
	require.Nil(t, got.KidID, "家长通知不得带 kid_id")
	require.Nil(t, got.ReadAt, "未读通知不得有 read_at")
	require.False(t, got.CreatedAt.IsZero(), "created_at 应由 DB 默认值写入")
}

// TestMessageRepoAudienceIsolation is the critical boundary contract here:
// recipients must never see or mark each other's messages.
//
// Parents see parents', kids see their own; crossing the boundary is a no-op.
func TestMessageRepoAudienceIsolation(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewMessageRepoSQLite(db)

	kidA := seedKid(t, db, "A")
	kidB := seedKid(t, db, "B")

	_, err := repo.Insert(ctx, newParentNotice(domain.MessageKindBackupDone, "备份完成"))
	require.NoError(t, err)
	aMsg, err := repo.Insert(ctx, newKidNotice(kidA, domain.MessageKindReviewDue, "A 有错题待复习"))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, newKidNotice(kidB, domain.MessageKindReviewDue, "B 有错题待复习"))
	require.NoError(t, err)

	// Parent sees only own 1 message.
	parent, err := repo.List(ctx, domain.ListMessagesFilter{Audience: domain.MessageAudienceParent, Limit: 10})
	require.NoError(t, err)
	require.Len(t, parent, 1)
	require.Equal(t, "备份完成", parent[0].Title)

	// Kid A sees only own 1 message, not B's.
	kids, err := repo.List(ctx, domain.ListMessagesFilter{
		Audience: domain.MessageAudienceKid, KidID: kidA, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, kids, 1)
	require.Equal(t, int64(kidA), *kids[0].KidID)

	// Kid A marks B's message: not changeable (idempotently swallowed, but B's row must stay unread).
	require.NoError(t, repo.MarkRead(ctx, aMsg, domain.MessageAudienceKid, kidB))
	nB, err := repo.CountUnread(ctx, domain.MessageAudienceKid, kidB)
	require.NoError(t, err)
	require.Equal(t, 1, nB, "越权标记不应改变目标孩子的未读数")

	// "Mark all read" also only applies to own boundary.
	_, err = repo.MarkAllRead(ctx, domain.MessageAudienceKid, kidA)
	require.NoError(t, err)
	nA, err := repo.CountUnread(ctx, domain.MessageAudienceKid, kidA)
	require.NoError(t, err)
	require.Zero(t, nA)
	nB, err = repo.CountUnread(ctx, domain.MessageAudienceKid, kidB)
	require.NoError(t, err)
	require.Equal(t, 1, nB, "孩子 A 的「全部已读」不得波及孩子 B")
}

// Parent notices are family-level: they don't distinguish which parent, so one
// parent reading marks them read for the whole family.
func TestMessageRepoParentMarkAllReadIsFamilyLevel(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewMessageRepoSQLite(db)

	_, err := repo.Insert(ctx, newParentNotice(domain.MessageKindBackupFailed, "备份失败"))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, newParentNotice(domain.MessageKindAIReviewPending, "AI 待审 3 条"))
	require.NoError(t, err)

	n, err := repo.CountUnread(ctx, domain.MessageAudienceParent, 0)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	updated, err := repo.MarkAllRead(ctx, domain.MessageAudienceParent, 0)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated)

	n, err = repo.CountUnread(ctx, domain.MessageAudienceParent, 0)
	require.NoError(t, err)
	require.Zero(t, n)

	// Repeated mark returns 0 (not 2): the frontend need not prompt again.
	updated, err = repo.MarkAllRead(ctx, domain.MessageAudienceParent, 0)
	require.NoError(t, err)
	require.Zero(t, updated)
}

func TestMessageRepoListKeysetCursor(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewMessageRepoSQLite(db)

	ids := make([]int64, 0, 5)
	for i := 0; i < 5; i++ {
		id, err := repo.Insert(ctx, newParentNotice(domain.MessageKindOther, "通知"))
		require.NoError(t, err)
		ids = append(ids, id)
	}

	// cursor=0 starts from the newest, id DESC.
	page1, err := repo.List(ctx, domain.ListMessagesFilter{Audience: domain.MessageAudienceParent, Limit: 3})
	require.NoError(t, err)
	require.Len(t, page1, 3)
	require.Equal(t, ids[4], page1[0].ID)
	require.Equal(t, ids[2], page1[2].ID)

	// continue from page1's last row, no duplicates no misses.
	page2, err := repo.List(ctx, domain.ListMessagesFilter{
		Audience: domain.MessageAudienceParent, Limit: 3, Cursor: page1[2].ID,
	})
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.Equal(t, ids[1], page2[0].ID)
	require.Equal(t, ids[0], page2[1].ID)
}

func TestMessageRepoListUnreadOnly(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewMessageRepoSQLite(db)

	read, err := repo.Insert(ctx, newParentNotice(domain.MessageKindOther, "已读"))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, newParentNotice(domain.MessageKindOther, "未读1"))
	require.NoError(t, err)
	_, err = repo.Insert(ctx, newParentNotice(domain.MessageKindOther, "未读2"))
	require.NoError(t, err)
	require.NoError(t, repo.MarkRead(ctx, read, domain.MessageAudienceParent, 0))

	rows, err := repo.List(ctx, domain.ListMessagesFilter{
		Audience: domain.MessageAudienceParent, UnreadOnly: true, Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, m := range rows {
		require.False(t, m.IsRead)
	}
}

func TestMessageRepoMarkReadIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewMessageRepoSQLite(db)

	id, err := repo.Insert(ctx, newParentNotice(domain.MessageKindOther, "通知"))
	require.NoError(t, err)

	require.NoError(t, repo.MarkRead(ctx, id, domain.MessageAudienceParent, 0))
	// Repeated mark does not error: a user double-click should not get an error prompt.
	require.NoError(t, repo.MarkRead(ctx, id, domain.MessageAudienceParent, 0))
	// Marking a non-existent id does not error either: idempotent even when
	// the recipient boundary mismatches.
	require.NoError(t, repo.MarkRead(ctx, 9999, domain.MessageAudienceParent, 0))

	rows, err := repo.List(ctx, domain.ListMessagesFilter{Audience: domain.MessageAudienceParent, Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].IsRead)
	require.NotNil(t, rows[0].ReadAt, "标记已读必须写 read_at")
}

// TestMessageRepoCheckConstraintRejectsMispairedAudience verifies the DB-layer fallback:
// parent notice with kid_id / kid notice missing kid_id are both rejected by the CHECK constraint.
func TestMessageRepoCheckConstraintRejectsMispairedAudience(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewMessageRepoSQLite(db)

	kidID := seedKid(t, db, "A")

	_, err := repo.Insert(ctx, &domain.Message{
		Audience: domain.MessageAudienceParent, KidID: i64(kidID), Kind: domain.MessageKindOther, Title: "错配",
	})
	require.Error(t, err, "家长通知带 kid_id 应被 CHECK 约束拒绝")

	_, err = repo.Insert(ctx, &domain.Message{
		Audience: domain.MessageAudienceKid, Kind: domain.MessageKindOther, Title: "缺 kid_id",
	})
	require.Error(t, err, "孩子通知缺 kid_id 应被 CHECK 约束拒绝")
}
