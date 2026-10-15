package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// fakeMessageRepo is an in-memory MessageRepository for verifying the service's
// page-size clamping and recipient-boundary checks (the clamping logic itself is covered separately by repository integration tests).
type fakeMessageRepo struct {
	rows []*domain.Message

	// Records the last recipient boundary passed, asserting the service forwarded kid_id.
	gotAudience int
	gotKidID    int64

	listErr error
}

func (r *fakeMessageRepo) Insert(_ context.Context, m *domain.Message) (int64, error) {
	m.ID = int64(len(r.rows) + 1)
	r.rows = append(r.rows, m)
	return m.ID, nil
}

func (r *fakeMessageRepo) List(_ context.Context, f domain.ListMessagesFilter) ([]*domain.Message, error) {
	r.gotAudience, r.gotKidID = f.Audience, f.KidID
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]*domain.Message, 0, len(r.rows))
	for _, m := range r.rows {
		if m.Audience != f.Audience {
			continue
		}
		if f.Audience == domain.MessageAudienceKid {
			if m.KidID == nil || *m.KidID != f.KidID {
				continue
			}
		}
		if f.Cursor > 0 && m.ID >= f.Cursor {
			continue
		}
		if f.UnreadOnly && m.IsRead {
			continue
		}
		out = append(out, m)
	}
	// Repo contract: id DESC, at most Limit rows (the caller fetches one extra to detect has_more).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (r *fakeMessageRepo) CountUnread(_ context.Context, audience int, kidID int64) (int, error) {
	r.gotAudience, r.gotKidID = audience, kidID
	n := 0
	for _, m := range r.rows {
		if m.Audience != audience || m.IsRead {
			continue
		}
		if audience == domain.MessageAudienceKid && (m.KidID == nil || *m.KidID != kidID) {
			continue
		}
		n++
	}
	return n, nil
}

func (r *fakeMessageRepo) MarkRead(_ context.Context, id int64, audience int, kidID int64) error {
	for _, m := range r.rows {
		if m.ID != id || m.Audience != audience || m.IsRead {
			continue
		}
		if audience == domain.MessageAudienceKid && (m.KidID == nil || *m.KidID != kidID) {
			continue
		}
		m.IsRead = true
	}
	return nil
}

func (r *fakeMessageRepo) MarkAllRead(_ context.Context, audience int, kidID int64) (int64, error) {
	var n int64
	for _, m := range r.rows {
		if m.Audience != audience || m.IsRead {
			continue
		}
		if audience == domain.MessageAudienceKid && (m.KidID == nil || *m.KidID != kidID) {
			continue
		}
		m.IsRead = true
		n++
	}
	return n, nil
}

func int64Ptr(v int64) *int64 { return &v }

func newFakeMessageRepo(n int) *fakeMessageRepo {
	r := &fakeMessageRepo{}
	for i := 1; i <= n; i++ {
		r.rows = append(r.rows, &domain.Message{
			ID: int64(i), Audience: domain.MessageAudienceParent, Kind: domain.MessageKindOther,
			Title: fmt.Sprintf("通知 %d", i),
		})
	}
	return r
}

// TestMessageServiceListPaginatesWithKeyset verifies the fetch-one-extra / next_cursor semantics of cursor pagination.
func TestMessageServiceListPaginatesWithKeyset(t *testing.T) {
	repo := newFakeMessageRepo(5)
	svc := NewMessageService(repo)

	page, err := svc.List(context.Background(), domain.MessageAudienceParent, 0, 0, 2, false)
	require.NoError(t, err)
	require.Len(t, page.Items, 2, "limit=2 时只返回 2 条，多取的那条用于探测 has_more")
	require.True(t, page.HasMore)
	require.EqualValues(t, 4, page.NextCursor, "next_cursor 是本页最后一行的 id")

	page2, err := svc.List(context.Background(), domain.MessageAudienceParent, 0, page.NextCursor, 2, false)
	require.NoError(t, err)
	require.Len(t, page2.Items, 2)
	require.True(t, page2.HasMore)

	page3, err := svc.List(context.Background(), domain.MessageAudienceParent, 0, page2.NextCursor, 2, false)
	require.NoError(t, err)
	require.Len(t, page3.Items, 1)
	require.False(t, page3.HasMore, "最后一页 has_more=false")
	require.Zero(t, page3.NextCursor, "has_more=false ⟺ next_cursor=0")
}

// TestMessageServiceLimitIsClamped verifies the limit clamping:
// 0 or oversized inputs both converge to domain.MessageListLimit, and the repo never receives 10000.
func TestMessageServiceLimitIsClamped(t *testing.T) {
	for _, in := range []int{0, -5, 10000} {
		repo := newFakeMessageRepo(60)
		svc := NewMessageService(repo)
		page, err := svc.List(context.Background(), domain.MessageAudienceParent, 0, 0, in, false)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page.Items), domain.MessageListLimit,
			"limit=%d 应被收窄到 %d", in, domain.MessageListLimit)
	}
}

// TestMessageServiceRejectsMispairedAudience guards the recipient boundary:
// a parent carrying kid_id, a kid missing kid_id, and an unknown audience are all rejected in the service layer.
func TestMessageServiceRejectsMispairedAudience(t *testing.T) {
	svc := NewMessageService(newFakeMessageRepo(1))
	ctx := context.Background()

	_, err := svc.List(ctx, domain.MessageAudienceParent, 7, 0, 20, false)
	require.ErrorIs(t, err, domain.ErrInvalidInput, "家长通知不得带 kid_id")

	_, err = svc.List(ctx, domain.MessageAudienceKid, 0, 0, 20, false)
	require.ErrorIs(t, err, domain.ErrInvalidInput, "孩子通知必须带 kid_id")

	_, err = svc.List(ctx, 3, 1, 0, 20, false)
	require.ErrorIs(t, err, domain.ErrInvalidInput, "未知 audience 必须被拒")

	_, err = svc.UnreadCount(ctx, domain.MessageAudienceKid, 0)
	require.ErrorIs(t, err, domain.ErrInvalidInput)

	err = svc.MarkRead(ctx, 1, domain.MessageAudienceKid, 0)
	require.ErrorIs(t, err, domain.ErrInvalidInput)

	_, err = svc.MarkAllRead(ctx, domain.MessageAudienceParent, 7)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

// TestMessageServiceForwardsKidBoundary asserts kid_id is truly forwarded to the repo:
// without this step the repo's WHERE would lack the recipient boundary, and the authorization leak would happen at the repository layer.
func TestMessageServiceForwardsKidBoundary(t *testing.T) {
	repo := &fakeMessageRepo{rows: []*domain.Message{
		{ID: 1, Audience: domain.MessageAudienceKid, KidID: int64Ptr(5), Title: "A 的"},
		{ID: 2, Audience: domain.MessageAudienceKid, KidID: int64Ptr(9), Title: "B 的"},
	}}
	svc := NewMessageService(repo)
	ctx := context.Background()

	page, err := svc.List(ctx, domain.MessageAudienceKid, 5, 0, 20, false)
	require.NoError(t, err)
	require.Equal(t, domain.MessageAudienceKid, repo.gotAudience)
	require.EqualValues(t, 5, repo.gotKidID)
	require.Len(t, page.Items, 1)
	require.Equal(t, "A 的", page.Items[0].Title)

	// Kid A marks kid B's message: the repo filters by kid_id, so nothing changes on A's side.
	require.NoError(t, svc.MarkRead(ctx, 2, domain.MessageAudienceKid, 5))
	require.True(t, repo.rows[1].IsRead == false, "孩子 A 不应能标记孩子 B 的消息")

	n, err := svc.UnreadCount(ctx, domain.MessageAudienceKid, 5)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestMessageServiceRejectsNegativeCursor(t *testing.T) {
	svc := NewMessageService(newFakeMessageRepo(1))
	_, err := svc.List(context.Background(), domain.MessageAudienceParent, 0, -1, 20, false)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestMessageServiceCreateParentNotice(t *testing.T) {
	repo := newFakeMessageRepo(0)
	svc := NewMessageService(repo)

	id, err := svc.CreateParentNotice(
		context.Background(),
		domain.MessageKindGradeAdvanced,
		"  已自动升级  ",
		"如因休学/跳级不符，请到孩子档案修改。",
		"/kids/1",
	)
	require.NoError(t, err)
	require.Positive(t, id)
	require.Len(t, repo.rows, 1)
	require.Equal(t, "已自动升级", repo.rows[0].Title, "标题首尾空白应被裁掉")
	require.Nil(t, repo.rows[0].KidID, "家长通知不得带 kid_id")
	require.Equal(t, "/kids/1", *repo.rows[0].Link)

	// An empty title is invalid input: a notification without a title cannot be rendered in the bell.
	_, err = svc.CreateParentNotice(context.Background(), domain.MessageKindOther, "   ", "", "")
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestMessageServiceCreateKidNotice(t *testing.T) {
	repo := newFakeMessageRepo(0)
	svc := NewMessageService(repo)

	id, err := svc.CreateKidNotice(
		context.Background(),
		7,
		domain.MessageKindGradeChanged,
		"  年级已更新  ",
		"你的年级已更新为 9 年级。",
		"/kid",
	)
	require.NoError(t, err)
	require.Positive(t, id)
	require.Len(t, repo.rows, 1)
	require.Equal(t, domain.MessageAudienceKid, repo.rows[0].Audience)
	require.NotNil(t, repo.rows[0].KidID, "孩子通知必须带 kid_id")
	require.EqualValues(t, 7, *repo.rows[0].KidID)
	require.Equal(t, "年级已更新", repo.rows[0].Title, "标题首尾空白应被裁掉")

	// A kid notice without a valid kid_id cannot satisfy the CHECK constraint.
	_, err = svc.CreateKidNotice(context.Background(), 0, domain.MessageKindGradeChanged, "标题", "", "")
	require.ErrorIs(t, err, domain.ErrInvalidInput)

	// Empty title is invalid input, same as the parent writer.
	_, err = svc.CreateKidNotice(context.Background(), 7, domain.MessageKindGradeChanged, "  ", "", "")
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func (m *fakeMessageRepo) WithTx(tx domain.Tx) domain.MessageRepository { return m }
