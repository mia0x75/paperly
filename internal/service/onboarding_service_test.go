package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// --- test doubles (self-contained, not depending on other test files) ---

type onbUserRepo struct {
	users []*domain.User
}

func (m *onbUserRepo) GetFirst(context.Context) (*domain.User, error) {
	if len(m.users) == 0 {
		return nil, domain.ErrNotFound
	}
	return m.users[0], nil
}

func (m *onbUserRepo) GetByID(_ context.Context, id int64) (*domain.User, error) {
	for _, u := range m.users {
		if u.ID == id {
			return u, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *onbUserRepo) List(context.Context) ([]*domain.User, error) { return m.users, nil }
func (m *onbUserRepo) Insert(_ context.Context, u *domain.User) (int64, error) {
	m.users = append(m.users, u)
	return int64(len(m.users)), nil
}
func (m *onbUserRepo) ExistsByDisplayName(_ context.Context, name string, _ int64) (bool, error) {
	for _, u := range m.users {
		if u.DisplayName == name {
			return true, nil
		}
	}
	return false, nil
}
func (m *onbUserRepo) UpdateDisplayName(context.Context, int64, string) (int64, error) { return 1, nil }
func (m *onbUserRepo) UpdatePinHash(context.Context, int64, string) (int64, error)     { return 1, nil }

type onbKidRepo struct {
	kids []*domain.Kid
}

func (m *onbKidRepo) GetByID(_ context.Context, _ int64) (*domain.Kid, error) {
	return nil, domain.ErrNotFound
}
func (m *onbKidRepo) Insert(ctx context.Context, k *domain.Kid) (int64, error) {
	m.kids = append(m.kids, k)
	return int64(len(m.kids)), nil
}
func (m *onbKidRepo) List(context.Context, domain.ListKidsFilter) ([]*domain.Kid, error) {
	return m.kids, nil
}
func (m *onbKidRepo) Update(context.Context, *domain.Kid) error                 { return nil }
func (m *onbKidRepo) Graduate(context.Context, int64) error                     { return nil }
func (m *onbKidRepo) UpdatePinHash(context.Context, int64, string) error        { return nil }
func (m *onbKidRepo) SoftDelete(context.Context, int64) error                   { return nil }
func (m *onbKidRepo) Restore(context.Context, int64) error                      { return nil }
func (m *onbKidRepo) UpdateAccessCode(context.Context, int64, string) error     { return nil }
func (m *onbKidRepo) ExistsByAccessCode(context.Context, string) (bool, error)  { return false, nil }
func (m *onbKidRepo) ExistsByName(context.Context, string, int64) (bool, error) { return false, nil }
func (m *onbKidRepo) GetByAccessCode(context.Context, string) (*domain.Kid, error) {
	return nil, domain.ErrNotFound
}

type onbCursorRepo struct {
	c *domain.MaintenanceCursor
}

func (m *onbCursorRepo) Get(_ context.Context, _ string) (*domain.MaintenanceCursor, error) {
	if m.c == nil {
		return nil, domain.ErrNotFound
	}
	return m.c, nil
}
func (m *onbCursorRepo) Upsert(_ context.Context, c *domain.MaintenanceCursor) error {
	m.c = c
	return nil
}
func (m *onbCursorRepo) List(context.Context) ([]*domain.MaintenanceCursor, error) {
	if m.c == nil {
		return nil, nil
	}
	return []*domain.MaintenanceCursor{m.c}, nil
}

func newOnbSvc(users []*domain.User, kids []*domain.Kid, c *domain.MaintenanceCursor) *OnboardingService {
	return NewOnboardingService(&onbUserRepo{users: users}, &onbKidRepo{kids: kids}, &onbCursorRepo{c: c})
}

// --- Status ---

func TestOnboardingStatusEmptyReturnsFalse(t *testing.T) {
	ctx := context.Background()
	svc := newOnbSvc(nil, nil, nil)

	done, err := svc.Status(ctx)
	require.NoError(t, err)
	require.False(t, done, "无标记、无实体 → 未完成")
}

func TestOnboardingStatusCursorButEmptyEntitiesIsFalse(t *testing.T) {
	ctx := context.Background()
	cursor := &domain.MaintenanceCursor{
		TaskName:   domain.CursorTaskOnboardingDone,
		LastStatus: domain.CursorStatusCompleted,
		LastRunAt:  &[]time.Time{time.Now()}[0],
	}
	// Marker set but entities all empty: the safety net after data clearing must pull the user back to onboarding.
	svc := newOnbSvc(nil, nil, cursor)

	done, err := svc.Status(ctx)
	require.NoError(t, err)
	require.False(t, done)
}

func TestOnboardingStatusOnlyUsersIsFalse(t *testing.T) {
	ctx := context.Background()
	cursor := &domain.MaintenanceCursor{
		TaskName:   domain.CursorTaskOnboardingDone,
		LastStatus: domain.CursorStatusCompleted,
	}
	svc := newOnbSvc([]*domain.User{{ID: 1, DisplayName: "妈妈"}}, nil, cursor)

	done, err := svc.Status(ctx)
	require.NoError(t, err)
	require.False(t, done, "只有家长没有孩子仍不算完成")
}

func TestOnboardingStatusOnlyKidsIsFalse(t *testing.T) {
	ctx := context.Background()
	cursor := &domain.MaintenanceCursor{
		TaskName:   domain.CursorTaskOnboardingDone,
		LastStatus: domain.CursorStatusCompleted,
	}
	svc := newOnbSvc(nil, []*domain.Kid{{ID: 1, Name: "小明"}}, cursor)

	done, err := svc.Status(ctx)
	require.NoError(t, err)
	require.False(t, done, "只有孩子没有家长仍不算完成")
}

func TestOnboardingStatusAllPresentIsTrue(t *testing.T) {
	ctx := context.Background()
	cursor := &domain.MaintenanceCursor{
		TaskName:   domain.CursorTaskOnboardingDone,
		LastStatus: domain.CursorStatusCompleted,
	}
	svc := newOnbSvc([]*domain.User{{ID: 1, DisplayName: "妈妈"}}, []*domain.Kid{{ID: 1, Name: "小明"}}, cursor)

	done, err := svc.Status(ctx)
	require.NoError(t, err)
	require.True(t, done)
}

func TestOnboardingStatusCursorPendingIsFalse(t *testing.T) {
	ctx := context.Background()
	// last_status != Completed: the marker exists but is in another state, always treated as incomplete.
	cursor := &domain.MaintenanceCursor{
		TaskName:   domain.CursorTaskOnboardingDone,
		LastStatus: domain.CursorStatusRunning,
	}
	svc := newOnbSvc([]*domain.User{{ID: 1}}, []*domain.Kid{{ID: 1}}, cursor)

	done, err := svc.Status(ctx)
	require.NoError(t, err)
	require.False(t, done, "非 Completed 状态不得算完成")
}

// --- Complete ---

func TestOnboardingCompleteRequiresBothEntities(t *testing.T) {
	ctx := context.Background()
	svc := newOnbSvc(nil, []*domain.Kid{{ID: 1}}, nil)

	_, err := svc.Complete(ctx)
	require.ErrorIs(t, err, domain.ErrInvalidState, "无家长不得硬打勾")
}

func TestOnboardingCompleteWritesCursor(t *testing.T) {
	ctx := context.Background()
	cursorRepo := &onbCursorRepo{}
	svc := NewOnboardingService(
		&onbUserRepo{users: []*domain.User{{ID: 1, DisplayName: "妈妈"}}},
		&onbKidRepo{kids: []*domain.Kid{{ID: 1, Name: "小明"}}},
		cursorRepo,
	)

	done, err := svc.Complete(ctx)
	require.NoError(t, err)
	require.True(t, done)
	require.NotNil(t, cursorRepo.c)
	require.Equal(t, domain.CursorTaskOnboardingDone, cursorRepo.c.TaskName)
	require.Equal(t, domain.CursorStatusCompleted, cursorRepo.c.LastStatus)
	require.NotNil(t, cursorRepo.c.LastRunAt, "完成标记要带时间戳（与 daily_mastery 对齐）")

	// After the write, Status should report complete.
	done, err = svc.Status(ctx)
	require.NoError(t, err)
	require.True(t, done)
}

func (m *onbUserRepo) WithTx(tx domain.Tx) domain.UserRepository { return m }

func (m *onbKidRepo) WithTx(tx domain.Tx) domain.KidRepository { return m }

func (m *onbCursorRepo) WithTx(tx domain.Tx) domain.MaintenanceCursorRepository { return m }
