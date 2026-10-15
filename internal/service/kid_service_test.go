package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// memKidRepo is an in-memory fake implementation of KidRepo for service-layer tests.
type memKidRepo struct {
	byID   map[int64]*domain.Kid
	byCode map[string]int64
	nextID int64
	// existsCalls records ExistsByAccessCode invocations, to verify the retry behavior.
	existsCalls int
	// existsScript, if non-empty, returns values in order; after it is exhausted it returns false.
	existsScript []bool
	insertErr    error
}

func newMemRepo() *memKidRepo {
	return &memKidRepo{byID: map[int64]*domain.Kid{}, byCode: map[string]int64{}}
}

func (m *memKidRepo) Insert(_ context.Context, s *domain.Kid) (int64, error) {
	if m.insertErr != nil {
		return 0, m.insertErr
	}
	m.nextID++
	clone := *s
	clone.ID = m.nextID
	m.byID[m.nextID] = &clone
	m.byCode[s.AccessCode] = m.nextID
	return m.nextID, nil
}

func (m *memKidRepo) GetByID(_ context.Context, id int64) (*domain.Kid, error) {
	s, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return s, nil
}

func (m *memKidRepo) List(_ context.Context, f domain.ListKidsFilter) ([]*domain.Kid, error) {
	out := make([]*domain.Kid, 0)
	for _, s := range m.byID {
		if f.Status != 0 && s.Status != f.Status {
			continue
		}
		if f.Status == 0 && !f.IncludeDeleted && s.Status == domain.KidStatusDeleted {
			continue
		}
		if f.Grade > 0 && s.Grade != f.Grade {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (m *memKidRepo) Update(_ context.Context, s *domain.Kid) error {
	cur, ok := m.byID[s.ID]
	if !ok {
		return domain.ErrNotFound
	}
	cur.Name, cur.Grade, cur.Gender, cur.Avatar = s.Name, s.Grade, s.Gender, s.Avatar
	return nil
}

func (m *memKidRepo) Graduate(_ context.Context, id int64) error {
	cur, ok := m.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	cur.Status = domain.KidStatusGraduated
	return nil
}

func (m *memKidRepo) SoftDelete(_ context.Context, id int64) error {
	cur, ok := m.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	cur.Status = domain.KidStatusDeleted
	return nil
}

func (m *memKidRepo) ExistsByAccessCode(_ context.Context, code string) (bool, error) {
	m.existsCalls++
	if _, ok := m.byCode[code]; ok {
		return true, nil
	}
	if len(m.existsScript) > 0 {
		v := m.existsScript[0]
		m.existsScript = m.existsScript[1:]
		return v, nil
	}
	return false, nil
}

func (m *memKidRepo) Restore(_ context.Context, id int64) error {
	cur, ok := m.byID[id]
	if !ok || cur.Status != domain.KidStatusDeleted {
		return domain.ErrNotFound
	}
	cur.Status = domain.KidStatusActive
	return nil
}

func (m *memKidRepo) ExistsByName(_ context.Context, name string, excludeID int64) (bool, error) {
	for _, s := range m.byID {
		if s.ID != excludeID && s.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (m *memKidRepo) GetByAccessCode(_ context.Context, code string) (*domain.Kid, error) {
	id, ok := m.byCode[code]
	if !ok {
		return nil, domain.ErrNotFound
	}
	s, ok := m.byID[id]
	if !ok || s.Status != domain.KidStatusActive {
		return nil, domain.ErrNotFound
	}
	return s, nil
}

func (m *memKidRepo) UpdateAccessCode(_ context.Context, id int64, code string) error {
	cur, ok := m.byID[id]
	if !ok || cur.Status == domain.KidStatusDeleted {
		return domain.ErrNotFound
	}
	delete(m.byCode, cur.AccessCode)
	cur.AccessCode = code
	m.byCode[code] = id
	return nil
}

func (m *memKidRepo) UpdatePinHash(_ context.Context, id int64, hash string) error {
	cur, ok := m.byID[id]
	if !ok || cur.Status == domain.KidStatusDeleted {
		return domain.ErrNotFound
	}
	cur.PinHash = hash
	cur.PinVersion++
	return nil
}

func TestCreateKidSuccess(t *testing.T) {
	svc := NewKidService(newMemRepo(), nil, nil, nil)
	resp, err := svc.Create(context.Background(), CreateKidReq{
		Name: "小明", Grade: 9, Pin: strPtr("1234"),
	})
	require.NoError(t, err)
	require.Positive(t, resp.ID)
	require.Equal(t, "小明", resp.Name)
	require.Len(t, resp.AccessCode, domain.AccessCodeLength)
	require.Equal(t, "/invite/"+resp.AccessCode, resp.KidURL)
}

func TestCreateKidEmptyNameRejected(t *testing.T) {
	svc := NewKidService(newMemRepo(), nil, nil, nil)
	_, err := svc.Create(context.Background(), CreateKidReq{Name: "   "})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestCreateKidPinStates(t *testing.T) {
	ctx := context.Background()

	// No PIN → placeholder (NOT NULL semantics), and login against the placeholder always fails.
	repo1 := newMemRepo()
	_, err := NewKidService(repo1, nil, nil, nil).Create(ctx, CreateKidReq{Name: "A", Grade: 9})
	require.NoError(t, err)
	var stored *domain.Kid
	for _, s := range repo1.byID {
		stored = s
	}
	require.Equal(t, PinPlaceholder, stored.PinHash, "空 PIN 应写占位值而非 NULL")
	require.False(t, VerifyPin(stored.PinHash, "1234"), "占位值不可验证")

	// Non-empty PIN → bcrypt hash, not plaintext.
	repo2 := newMemRepo()
	_, err = NewKidService(repo2, nil, nil, nil).Create(ctx, CreateKidReq{Name: "B", Grade: 9, Pin: strPtr("1234")})
	require.NoError(t, err)
	var stored2 *domain.Kid
	for _, s := range repo2.byID {
		stored2 = s
	}
	require.Equal(t, "$2", stored2.PinHash[:2], "应为 bcrypt 占位值以外的真实哈希")
	require.NotEqual(t, "1234", stored2.PinHash, "PIN 不得明文入库")
	require.True(t, strings.HasPrefix(stored2.PinHash, "$2"), "应为 bcrypt 哈希")
	require.True(t, VerifyPin(stored2.PinHash, "1234"))
}

func TestCreateKidInvalidPinRejected(t *testing.T) {
	svc := NewKidService(newMemRepo(), nil, nil, nil)
	for _, bad := range []string{"12", "1234567", "abcd", "12a4"} {
		_, err := svc.Create(context.Background(), CreateKidReq{Name: "A", Grade: 9, Pin: &bad})
		require.ErrorIs(t, err, domain.ErrInvalidInput, "PIN %q 应被拒绝", bad)
	}
}

func TestCreateKidAccessCodeRetryOnCollision(t *testing.T) {
	repo := newMemRepo()
	// First call returns exists=true, triggering a retry; the second returns false, succeeding.
	repo.existsScript = []bool{true, false}
	svc := NewKidService(repo, nil, nil, nil)

	resp, err := svc.Create(context.Background(), CreateKidReq{Name: "A", Grade: 9})
	require.NoError(t, err)
	require.NotEmpty(t, resp.AccessCode)
	require.GreaterOrEqual(t, repo.existsCalls, 2, "应发生过重试")
}

func TestCreateKidAccessCodeCollisionExhausts(t *testing.T) {
	repo := newMemRepo()
	repo.existsScript = []bool{true, true, true, true, true, true, true, true, true, true, true}
	svc := NewKidService(repo, nil, nil, nil)

	_, err := svc.Create(context.Background(), CreateKidReq{Name: "A", Grade: 9})
	require.ErrorIs(t, err, domain.ErrAccessCodeCollision)
}

func TestCreateKidDuplicateNameRejected(t *testing.T) {
	ctx := context.Background()
	svc := NewKidService(newMemRepo(), nil, nil, nil)

	_, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9})
	require.NoError(t, err)

	_, err = svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 8})
	require.ErrorIs(t, err, domain.ErrDuplicateName, "重名必须被拒")
}

func TestCreateKidDuplicateNameIncludesDeleted(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewKidService(repo, nil, nil, nil)

	created, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9})
	require.NoError(t, err)
	require.NoError(t, repo.SoftDelete(ctx, created.ID))

	_, err = svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 3})
	require.ErrorIs(t, err, domain.ErrDuplicateName, "全表唯一：已软删的姓名也不可复用")
}

func TestUpdateKidDuplicateNameRejected(t *testing.T) {
	ctx := context.Background()
	svc := NewKidService(newMemRepo(), nil, nil, nil)

	_, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9})
	require.NoError(t, err)
	b, err := svc.Create(ctx, CreateKidReq{Name: "小红", Grade: 8})
	require.NoError(t, err)

	_, err = svc.Update(ctx, b.ID, UpdateKidReq{Name: "小明", Grade: 8})
	require.ErrorIs(t, err, domain.ErrDuplicateName, "改成他人姓名必须被拒")
}

func TestUpdateKidSameNameAllowed(t *testing.T) {
	ctx := context.Background()
	svc := NewKidService(newMemRepo(), nil, nil, nil)

	created, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9})
	require.NoError(t, err)

	got, err := svc.Update(ctx, created.ID, UpdateKidReq{Name: "小明", Grade: 8})
	require.NoError(t, err, "保持原名只改年级应放行")
	require.Equal(t, 8, got.Grade)
}

func TestGetKidNotFound(t *testing.T) {
	svc := NewKidService(newMemRepo(), nil, nil, nil)
	_, err := svc.Get(context.Background(), 42)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestDeleteKid(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewKidService(repo, nil, nil, nil)

	resp, err := svc.Create(ctx, CreateKidReq{Name: "A", Grade: 9})
	require.NoError(t, err)

	require.NoError(t, svc.Delete(ctx, resp.ID))
	got, err := svc.Get(ctx, resp.ID)
	require.NoError(t, err)
	require.Equal(t, domain.KidStatusDeleted, got.Status)

	require.ErrorIs(t, svc.Delete(ctx, 9999), domain.ErrNotFound)
}

func TestDeleteKidGraduatedRejected(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewKidService(repo, nil, nil, nil)

	created, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9})
	require.NoError(t, err)
	repo.byID[created.ID].Status = domain.KidStatusGraduated

	require.ErrorIs(t, svc.Delete(ctx, created.ID), domain.ErrInvalidState,
		"已毕业学生不允许删除")
	require.Equal(t, domain.KidStatusGraduated, repo.byID[created.ID].Status, "状态不应改变")
}

func TestRestoreDeletedKid(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewKidService(repo, nil, nil, nil)

	created, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9})
	require.NoError(t, err)
	require.NoError(t, svc.Delete(ctx, created.ID))

	got, err := svc.Restore(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.KidStatusActive, got.Status)
	require.Equal(t, domain.KidStatusActive, repo.byID[created.ID].Status)
}

func TestRestoreRejectsNonDeleted(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewKidService(repo, nil, nil, nil)

	created, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9})
	require.NoError(t, err)

	// Not deleted.
	_, err = svc.Restore(ctx, created.ID)
	require.ErrorIs(t, err, domain.ErrInvalidState)

	// Graduated.
	repo.byID[created.ID].Status = domain.KidStatusGraduated
	_, err = svc.Restore(ctx, created.ID)
	require.ErrorIs(t, err, domain.ErrInvalidState, "已毕业学生不允许恢复")
}

func TestGenerateAccessCodeCharsetAndUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		code, err := generateAccessCode()
		require.NoError(t, err)
		require.Len(t, code, 8)
		for _, ch := range code {
			require.False(t, strings.ContainsRune("0o1li", ch), "字符集不得含易混字符: %c", ch)
			require.True(t, strings.ContainsRune(domain.AccessCodeCharset, ch))
		}
		require.False(t, seen[code], "1000 次生成出现重复: %s", code)
		seen[code] = true
	}
}

func strPtr(s string) *string { return &s }

// --- parent reset of access_code / PIN ---

func TestResetAccessCodeChangesCodeAndInvalidatesOld(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewKidService(repo, nil, nil, nil)

	created, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9})
	require.NoError(t, err)
	old := created.AccessCode

	got, err := svc.ResetAccessCode(ctx, created.ID)
	require.NoError(t, err)
	require.NotEqual(t, old, got.AccessCode, "重置应产生新码")
	require.Len(t, got.AccessCode, domain.AccessCodeLength)

	// Old code is invalidated.
	_, err = repo.GetByAccessCode(ctx, old)
	require.ErrorIs(t, err, domain.ErrNotFound, "旧 access_code 必须失效")
	// New code is usable.
	byNew, err := repo.GetByAccessCode(ctx, got.AccessCode)
	require.NoError(t, err)
	require.EqualValues(t, created.ID, byNew.ID)
}

func TestResetAccessCodeNotFound(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewKidService(repo, nil, nil, nil)

	_, err := svc.ResetAccessCode(ctx, 999)
	require.ErrorIs(t, err, domain.ErrNotFound)

	created, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9})
	require.NoError(t, err)
	require.NoError(t, repo.SoftDelete(ctx, created.ID))
	_, err = svc.ResetAccessCode(ctx, created.ID)
	require.ErrorIs(t, err, domain.ErrNotFound, "已删学生不可重置")
}

func TestResetAccessCodeAllowsGraduated(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewKidService(repo, nil, nil, nil)

	created, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9})
	require.NoError(t, err)
	repo.byID[created.ID].Status = domain.KidStatusGraduated

	got, err := svc.ResetAccessCode(ctx, created.ID)
	require.NoError(t, err, "毕业生也允许重置访问码")
	require.NotEmpty(t, got.AccessCode)
}

func TestResetPinClears(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewKidService(repo, nil, nil, nil)

	created, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9, Pin: strPtr("1234")})
	require.NoError(t, err)
	require.Equal(t, "$2", repo.byID[created.ID].PinHash[:2], "创建后应为真实哈希")

	got, err := svc.ResetPin(ctx, created.ID, "")
	require.NoError(t, err)
	require.Equal(t, PinPlaceholder, got.PinHash, "空 new_pin = 清除 PIN（写占位值）")
	require.False(t, VerifyPin(got.PinHash, "1234"), "清除后旧 PIN 不可再登录")
}

func TestResetPinSetsHashed(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewKidService(repo, nil, nil, nil)

	created, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9})
	require.NoError(t, err)
	require.Equal(t, PinPlaceholder, repo.byID[created.ID].PinHash, "初始未设 PIN 为占位值")

	got, err := svc.ResetPin(ctx, created.ID, "5678")
	require.NoError(t, err)
	require.NotEqual(t, "5678", got.PinHash, "PIN 不得明文入库")
	require.NotEqual(t, PinPlaceholder, got.PinHash, "设 PIN 后不再是占位值")
	require.True(t, VerifyPin(got.PinHash, "5678"), "bcrypt 哈希应可验")
	require.False(t, VerifyPin(got.PinHash, "0000"))
}

func TestResetPinRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewKidService(repo, nil, nil, nil)

	created, err := svc.Create(ctx, CreateKidReq{Name: "小明", Grade: 9})
	require.NoError(t, err)

	for _, bad := range []string{"123", "1234567", "abcd", "12a4"} {
		_, err := svc.ResetPin(ctx, created.ID, bad)
		require.ErrorIs(t, err, domain.ErrInvalidInput, "非法 PIN %q 必须被拒", bad)
	}
}

func TestResetPinNotFound(t *testing.T) {
	ctx := context.Background()
	svc := NewKidService(newMemRepo(), nil, nil, nil)

	_, err := svc.ResetPin(ctx, 999, "1234")
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func (m *memKidRepo) WithTx(tx domain.Tx) domain.KidRepository { return m }
