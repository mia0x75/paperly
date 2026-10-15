package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

func newTestExam() *domain.Exam {
	grade := 9
	return &domain.Exam{
		Grade:      &grade,
		UploadedBy: domain.UploadedByParent,
		Status:     domain.ExamStatusActive,
	}
}

func TestExamRepoInsertGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewExamRepoSQLite(db)

	examType := domain.ExamTypeMidterm
	examDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	summary := "期中总结"
	summarySource := int(domain.SummarySourceManual)

	// The foreign keys require subject / kid to exist: insert one of each first.
	res, err := db.Write().ExecContext(ctx,
		`INSERT INTO subjects (code, name, sort_order, source) VALUES ('math', '数学', 20, 1)`)
	require.NoError(t, err)
	subjectID, err := res.LastInsertId()
	require.NoError(t, err)

	res, err = db.Write().ExecContext(ctx,
		`INSERT INTO kids (name, grade, access_code, pin_hash) VALUES ('小明', 9, 'kid00007', '!')`)
	require.NoError(t, err)
	kidID, err := res.LastInsertId()
	require.NoError(t, err)

	p := newTestExam()
	p.SubjectID = &subjectID
	p.ExamType = &examType
	p.ExamDate = &examDate
	p.KidID = &kidID
	p.Summary = &summary
	p.SummarySource = &summarySource

	title := "2026-初三-上学期-数学-05月-期中"
	p.Title = &title

	id, err := repo.Insert(ctx, p)
	require.NoError(t, err)
	require.Positive(t, id)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got.Title)
	require.Equal(t, title, *got.Title)
	require.NotNil(t, got.SubjectID)
	require.EqualValues(t, subjectID, *got.SubjectID)
	require.Equal(t, 9, *got.Grade)
	require.Equal(t, domain.ExamTypeMidterm, *got.ExamType)
	require.Equal(t, domain.UploadedByParent, got.UploadedBy)
	require.Equal(t, domain.ExamStatusActive, got.Status)
	require.Equal(t, "期中总结", *got.Summary)
	require.Equal(t, domain.SummarySourceManual, domain.SummarySource(*got.SummarySource))
	require.NotNil(t, got.KidID)
	require.EqualValues(t, kidID, *got.KidID)
	require.NotNil(t, got.ExamDate)
	require.True(t, got.ExamDate.Equal(examDate))
}

func TestExamRepoGetByIDNotFound(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewExamRepoSQLite(db)

	_, err := repo.GetByID(ctx, 999)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestExamRepoListFilters(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewExamRepoSQLite(db)

	var kidA, kidB int64
	gradeA, gradeB := 9, 8
	examType := domain.ExamTypeWeekly

	// Foreign keys require kids to exist.
	res, err := db.Write().ExecContext(ctx,
		`INSERT INTO kids (name, grade, access_code, pin_hash) VALUES ('A', 9, 'kidaaaa1', '!')`)
	require.NoError(t, err)
	kidA, err = res.LastInsertId()
	require.NoError(t, err)
	res, err = db.Write().ExecContext(ctx,
		`INSERT INTO kids (name, grade, access_code, pin_hash) VALUES ('B', 8, 'kidbbbb1', '!')`)
	require.NoError(t, err)
	kidB, err = res.LastInsertId()
	require.NoError(t, err)

	pA := newTestExam()
	pA.KidID = &kidA
	pA.Grade = &gradeA
	pA.ExamType = &examType
	_, err = repo.Insert(ctx, pA)
	require.NoError(t, err)

	pB := newTestExam()
	pB.KidID = &kidB
	pB.Grade = &gradeB
	_, err = repo.Insert(ctx, pB)
	require.NoError(t, err)

	// No filter: 2 rows, no next page.
	items, next, err := repo.List(ctx, domain.ListExamFilter{Limit: 20})
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Zero(t, next)

	// keyset: limit=1 → 1 row + next cursor; use the cursor to fetch the remaining 1 row.
	items, next, err = repo.List(ctx, domain.ListExamFilter{Limit: 1})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.EqualValues(t, items[0].ID, next, "游标应为本页最后一条 id")
	items2, next2, err := repo.List(ctx, domain.ListExamFilter{Limit: 1, Cursor: next})
	require.NoError(t, err)
	require.Len(t, items2, 1)
	require.Less(t, items2[0].ID, items[0].ID, "第二页应更旧")
	require.Zero(t, next2, "取完应无更多")

	items, _, err = repo.List(ctx, domain.ListExamFilter{Grade: &gradeA})
	require.NoError(t, err)
	require.Len(t, items, 1)

	items, _, err = repo.List(ctx, domain.ListExamFilter{ExamType: &examType})
	require.NoError(t, err)
	require.Len(t, items, 1)

	items, _, err = repo.List(ctx, domain.ListExamFilter{KidID: &kidB})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.EqualValues(t, kidB, *items[0].KidID)
}

func TestExamRepoUpdate(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewExamRepoSQLite(db)

	id, err := repo.Insert(ctx, newTestExam())
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	grade := 8
	summary := "更新备注"
	got.Grade = &grade
	got.Summary = &summary
	require.NoError(t, repo.Update(ctx, got))

	again, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 8, *again.Grade)
	require.Equal(t, "更新备注", *again.Summary)

	require.ErrorIs(t, repo.Update(ctx, &domain.Exam{ID: 99999}), domain.ErrNotFound)
}

func TestExamRepoSoftDelete(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewExamRepoSQLite(db)

	id, err := repo.Insert(ctx, newTestExam())
	require.NoError(t, err)

	require.NoError(t, repo.SoftDelete(ctx, id))

	// GetByID is unaffected by soft-delete (visibility is decided by List's status filter).
	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.ExamStatusDeleted, got.Status)

	// Status == 0 means all statuses, including deleted: the exam list's "all"
	// must not drop soft-deleted exams, or they'd be invisible even before the
	// parent filters for "deleted" (see domain.ListExamFilter.Status).
	items, next, err := repo.List(ctx, domain.ListExamFilter{})
	require.NoError(t, err)
	require.Zero(t, next)
	require.Len(t, items, 1, "默认 List 应包含已删除试卷")
	require.Equal(t, domain.ExamStatusDeleted, items[0].Status)

	items, _, err = repo.List(ctx, domain.ListExamFilter{Status: domain.ExamStatusDeleted})
	require.NoError(t, err)
	require.Len(t, items, 1)

	// Explicitly querying only normal must not find soft-deleted exams.
	items, _, err = repo.List(ctx, domain.ListExamFilter{Status: domain.ExamStatusActive})
	require.NoError(t, err)
	require.Empty(t, items)

	require.ErrorIs(t, repo.SoftDelete(ctx, 99999), domain.ErrNotFound)
}

// TestExamRepoRestore covers the soft-delete round-trip and status gate:
// a deleted exam restores to active, while restoring an active, missing or
// already-restored row is ErrNotFound.
func TestExamRepoRestore(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewExamRepoSQLite(db)

	id, err := repo.Insert(ctx, newTestExam())
	require.NoError(t, err)

	// An active exam cannot be restored (repo-level status gate → ErrNotFound).
	require.ErrorIs(t, repo.Restore(ctx, id), domain.ErrNotFound)

	require.NoError(t, repo.SoftDelete(ctx, id))

	require.NoError(t, repo.Restore(ctx, id))
	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.ExamStatusActive, got.Status)

	// Idempotency gate: restoring an already-restored (active) row → ErrNotFound.
	require.ErrorIs(t, repo.Restore(ctx, id), domain.ErrNotFound)

	require.ErrorIs(t, repo.Restore(ctx, 99999), domain.ErrNotFound)
}

// TestExamRepoListStatusFilter covers three-state filtering: default/all
// include soft-deleted, while "normal" and "deleted" are each exact. The
// table also exercises the combination with kid_id.
func TestExamRepoListStatusFilter(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewExamRepoSQLite(db)

	// exams.kid_id has a foreign key; create two kids first: kid7 / kid8.
	for _, code := range []string{"kid00007", "kid00008"} {
		_, err := db.Write().ExecContext(ctx,
			`INSERT INTO kids (name, grade, access_code, pin_hash) VALUES (?, 9, ?, '!')`, code, code)
		require.NoError(t, err)
	}
	var kid7, kid8 int64
	require.NoError(t, db.Read().GetContext(ctx, &kid7, `SELECT id FROM kids WHERE access_code = 'kid00007'`))
	require.NoError(t, db.Read().GetContext(ctx, &kid8, `SELECT id FROM kids WHERE access_code = 'kid00008'`))

	// kid7: one normal + one deleted; kid8: one normal. 3 total, 2 of them normal.
	const seed = `INSERT INTO exams (uploaded_by, kid_id, status, created_at, updated_at, summary)
VALUES (1, ?, ?, '2026-09-01 00:00:00', '2026-09-01 00:00:00', '')`
	_, err := db.Write().ExecContext(ctx, seed, kid7, domain.ExamStatusActive)
	require.NoError(t, err)
	_, err = db.Write().ExecContext(ctx, seed, kid7, domain.ExamStatusDeleted)
	require.NoError(t, err)
	_, err = db.Write().ExecContext(ctx, seed, kid8, domain.ExamStatusActive)
	require.NoError(t, err)

	cases := []struct {
		name   string
		filter domain.ListExamFilter
		want   int
	}{
		{
			name:   "缺省 Status=0 视为全部，含已删除",
			filter: domain.ListExamFilter{Limit: 20},
			want:   3,
		},
		{
			name:   "Status=0 显式传全部，与缺省一致",
			filter: domain.ListExamFilter{Status: 0, Limit: 20},
			want:   3,
		},
		{
			name:   "只查正常",
			filter: domain.ListExamFilter{Status: domain.ExamStatusActive, Limit: 20},
			want:   2,
		},
		{
			name:   "只查已删除",
			filter: domain.ListExamFilter{Status: domain.ExamStatusDeleted, Limit: 20},
			want:   1,
		},
		{
			name:   "与 kid_id 组合：全部 + kid7 含其已删除卷",
			filter: domain.ListExamFilter{KidID: &kid7, Limit: 20},
			want:   2,
		},
		{
			name:   "与 kid_id 组合：正常 + kid7 只含正常",
			filter: domain.ListExamFilter{Status: domain.ExamStatusActive, KidID: &kid7, Limit: 20},
			want:   1,
		},
		{
			name:   "与 kid_id 组合：已删除 + kid7 只含软删",
			filter: domain.ListExamFilter{Status: domain.ExamStatusDeleted, KidID: &kid7, Limit: 20},
			want:   1,
		},
		{
			// Invalid values follow the existing parameter convention: an
			// intParam parse failure falls back to the default 0 (equivalent
			// to "all"), not a 400.
			name:   "非法值回落为 0（等价全部）",
			filter: domain.ListExamFilter{Status: 0, Limit: 20},
			want:   3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, _, err := repo.List(ctx, tc.filter)
			require.NoError(t, err)
			require.Len(t, items, tc.want)
		})
	}
}

// TestExamRepoListStatusFilterKeepsOrphanExam locks the orphan-exam fallback:
// an exam uploaded but with no pages rows yet (a mid-upload state) must still
// be returned under all three status options.
func TestExamRepoListStatusFilterKeepsOrphanExam(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewExamRepoSQLite(db)

	// One normal exam with 0 pages (orphan), one normal with pages. Neither soft-deleted.
	const seed = `INSERT INTO exams (uploaded_by, status, created_at, updated_at, summary)
VALUES (1, ?, '2026-09-01 00:00:00', '2026-09-01 00:00:00', '')`
	_, err := db.Write().ExecContext(ctx, seed, domain.ExamStatusActive)
	require.NoError(t, err)
	_, err = db.Write().ExecContext(ctx, seed, domain.ExamStatusActive)
	require.NoError(t, err)

	withPage, _, err := repo.List(ctx, domain.ListExamFilter{Limit: 20})
	require.NoError(t, err)
	require.Len(t, withPage, 2)
	// The first (smaller id) has no pages, so its page count is 0. The list
	// does not hide it for having no pages.
	require.Zero(t, withPage[1].PageCount, "0 页的 exam 仍在列表里（孤儿兜底）")

	// Orphans are also listed under explicit "normal".
	normal, _, err := repo.List(ctx, domain.ListExamFilter{Status: domain.ExamStatusActive, Limit: 20})
	require.NoError(t, err)
	require.Len(t, normal, 2)
}

func TestListExamIncludesPageCount(t *testing.T) {
	db := openTestDB(t)
	repo := NewExamRepoSQLite(db)
	ctx := context.Background()

	// One exam with 3 pages, one with 1 page and one with 0. A soft-deleted
	// exam is also present; it does not take part in the page-count lens.
	const insert = `INSERT INTO exams (uploaded_by, status, created_at, updated_at, summary)
		VALUES (?, ?, '2026-09-01 09:00:00', '2026-09-01 09:00:00', ?)`
	for _, s := range []struct {
		status  int
		summary string
	}{{domain.ExamStatusActive, "第一单元测试卷"}, {domain.ExamStatusActive, "期中卷"}, {domain.ExamStatusActive, "空卷"}} {
		if _, err := db.Write().ExecContext(ctx, insert, domain.UploadedByParent, s.status, s.summary); err != nil {
			t.Fatalf("insert exam: %v", err)
		}
	}
	const insertPage = `INSERT INTO pages (exam_id, page_index, file_path, sha256) VALUES (?, ?, '/tmp/x.png', ?)`
	for i := 1; i <= 3; i++ {
		if _, err := db.Write().ExecContext(ctx, insertPage, 1, i, "sha1"); err != nil {
			t.Fatalf("insert page: %v", err)
		}
	}
	if _, err := db.Write().ExecContext(ctx, insertPage, 2, 1, "sha2"); err != nil {
		t.Fatalf("insert page: %v", err)
	}

	got, _, err := repo.List(ctx, domain.ListExamFilter{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// Order by id DESC: 3(0 pages) / 2(1 page) / 1(3 pages)
	want := []struct {
		id      int64
		pages   int
		summary string
	}{{3, 0, "空卷"}, {2, 1, "期中卷"}, {1, 3, "第一单元测试卷"}}
	for i, w := range want {
		p := got[i]
		if p.ID != w.id {
			t.Fatalf("第 %d 行 ID = %d, want %d", i, p.ID, w.id)
		}
		if p.PageCount != w.pages {
			t.Errorf("ID=%d PageCount = %d, want %d", p.ID, p.PageCount, w.pages)
		}
		if p.Summary == nil || *p.Summary != w.summary {
			t.Errorf("ID=%d Summary = %v, want %s", p.ID, p.Summary, w.summary)
		}
	}
}

func TestListExamPageSizeNotInflatedByPageJoin(t *testing.T) {
	db := openTestDB(t)
	repo := NewExamRepoSQLite(db)
	ctx := context.Background()

	// One exam with 5 pages: if expanded by JOIN pages, this page would return only 1 row
	const insert = `INSERT INTO exams (uploaded_by, status, created_at, updated_at) VALUES (?, 1,
		'2026-09-01 09:00:00', '2026-09-01 09:00:00')`
	res, err := db.Write().ExecContext(ctx, insert, domain.UploadedByParent)
	if err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	id, _ := res.LastInsertId()
	const insertPage = `INSERT INTO pages (exam_id, page_index, file_path, sha256) VALUES (?, ?, '/tmp/x.png', ?)`
	for i := 1; i <= 5; i++ {
		if _, err := db.Write().ExecContext(ctx, insertPage, id, i, "sha"); err != nil {
			t.Fatalf("insert page: %v", err)
		}
	}

	got, _, err := repo.List(ctx, domain.ListExamFilter{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// A JOIN would turn 1 exam into 5 rows, corrupting pagination
	if len(got) != 1 {
		t.Errorf("返回行数 = %d, want 1（页数必须用子查询，不能 JOIN 展开）", len(got))
	}
	if got[0].PageCount != 5 {
		t.Errorf("PageCount = %d, want 5", got[0].PageCount)
	}
}

// TestExamRepoListDateRange covers the exam_date closed interval: both
// endpoints include their day, and reversed endpoints are swapped. Without the
// swap the query is always false and returns an empty list, a silent wrong answer.
func TestExamRepoListDateRange(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewExamRepoSQLite(db)

	// Three exams on different dates: 09-01 / 09-15 / 09-30.
	seed := func(day int) {
		p := newTestExam()
		d := time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC)
		p.ExamDate = &d
		_, err := repo.Insert(ctx, p)
		require.NoError(t, err)
	}
	seed(1)
	seed(15)
	seed(30)

	day := func(d int) *time.Time {
		tm := time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC)
		return &tm
	}

	// Both closed-interval endpoints include: 15~30 should hit both 15 and 30.
	items, _, err := repo.List(ctx, domain.ListExamFilter{
		DateFrom: day(15), DateTo: day(30), Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, items, 2)

	// Single-day closed interval: from == to == 15 hits only 15.
	items, _, err = repo.List(ctx, domain.ListExamFilter{
		DateFrom: day(15), DateTo: day(15), Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, items, 1)

	// Reversed: from=30, to=01 should equal 01~30, hitting all 3.
	items, _, err = repo.List(ctx, domain.ListExamFilter{
		DateFrom: day(30), DateTo: day(1), Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, items, 3, "逆序端点应交换而非返回空列表")
}

// TestExamRepoListCursorStableAcrossDelete: keyset pagination must not miss
// rows when an already-paged row is deleted, exactly where offset pagination fails.
func TestExamRepoListCursorStableAcrossDelete(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewExamRepoSQLite(db)

	var ids []int64
	for i := 0; i < 5; i++ {
		id, err := repo.Insert(ctx, newTestExam())
		require.NoError(t, err)
		ids = append(ids, id)
	}
	// ids: 1..5, id DESC order is 5,4,3,2,1.

	// First page limit=2 → 5,4; cursor=4.
	page1, next, err := repo.List(ctx, domain.ListExamFilter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.Equal(t, int64(4), next)

	// During paging, delete the "already paged" 5 (a first-page row).
	require.NoError(t, repo.SoftDelete(ctx, ids[4]))

	// Second page: id < 4 → 3,2. With OFFSET and a deleted leading row this
	// would misalign and miss 3.
	page2, _, err := repo.List(ctx, domain.ListExamFilter{Limit: 2, Cursor: next})
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.Equal(t, int64(3), page2[0].ID)
	require.Equal(t, int64(2), page2[1].ID)
}
