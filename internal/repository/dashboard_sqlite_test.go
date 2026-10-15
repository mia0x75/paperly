package repository

import (
	"context"
	"testing"
	"time"

	"paperly/internal/domain"
)

// win is the fixed window used in tests (baseline 2026-09-15).
func win() domain.DashboardWindow {
	return domain.DashboardWindow{
		From:          time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
		To:            time.Date(2026, 9, 15, 23, 59, 59, 0, time.UTC),
		ThisMonthFrom: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		LastMonthFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		LastMonthTo:   time.Date(2026, 8, 31, 23, 59, 59, 0, time.UTC),
	}
}

// seedDashboard builds two kids: A has 2 math + 1 chinese (all archived),
// plus 1 soft-deleted math; B has 1 math.
func seedDashboard(t *testing.T) *DashboardRepoSQLite {
	t.Helper()
	db := openTestDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Write().ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}

	exec(`INSERT INTO subjects (id, code, name, sort_order) VALUES (1, 'math', '数学', 1)`)
	exec(`INSERT INTO subjects (id, code, name, sort_order) VALUES (2, 'chinese', '语文', 2)`)
	exec(`INSERT INTO subjects (id, code, name, sort_order) VALUES (3, 'english', '英语', 3)`)

	const insertKid = `INSERT INTO kids (name, grade, gender, access_code, pin_hash, status, created_at, updated_at)
		VALUES (?, ?, 1, ?, '', ?, ?, ?)`
	exec(insertKid, "甲", 8, "code-a", domain.KidStatusActive, "2026-08-01 10:00:00", "2026-08-01 10:00:00")
	exec(insertKid, "乙", 9, "code-b", domain.KidStatusActive, "2026-08-01 10:00:00", "2026-08-01 10:00:00")

	const insertExam = `INSERT INTO exams (kid_id, subject_id, exam_type, exam_date, status, uploaded_by,
		created_at, updated_at) VALUES (?, ?, 1, '2026-09-01', ?, ?, ?, ?)`
	type row struct {
		kid, subj int64
		status    int
		created   string
	}
	rows := []row{
		// A this month (September): 2 math + 1 chinese
		{1, 1, domain.ExamStatusActive, "2026-09-01 09:00:00"},
		{1, 1, domain.ExamStatusActive, "2026-09-10 09:00:00"},
		{1, 2, domain.ExamStatusActive, "2026-09-12 09:00:00"},
		// A last month (August): 1, for month-over-month
		{1, 1, domain.ExamStatusActive, "2026-08-20 09:00:00"},
		// A's soft-deleted: every lens must ignore it
		{1, 1, domain.ExamStatusDeleted, "2026-09-13 09:00:00"},
		// B this month: 1 math (only counted in All, not in A)
		{2, 1, domain.ExamStatusActive, "2026-09-11 09:00:00"},
	}
	for _, r := range rows {
		exec(insertExam, r.kid, r.subj, r.status, domain.UploadedByParent, r.created, r.created)
	}

	// A's 4 active exams (exam_id 1-4) have 1 page each; B's (exam_id 6) has 1,
	// for 5 pages globally. exam_id 5 is A's soft-deleted exam, so it has no page.
	const insertPage = `INSERT INTO pages (exam_id, page_index, file_path, sha256) VALUES (?, 1, '/tmp/x.png', ?)`
	for _, id := range []int{1, 2, 3, 4, 6} {
		exec(insertPage, id, string(rune('a'+id)))
	}

	// Kid A's subject set (kid_subjects): all three subjects. The kid-scoped subject
	// queries (archives / distribution) read this set directly — there is no mask fallback.
	const insertKidSubject = `INSERT INTO kid_subjects (kid_id, subject_id) VALUES (?, ?)`
	for _, subj := range []int{1, 2, 3} {
		exec(insertKidSubject, 1, subj)
	}

	return NewDashboardRepoSQLite(db)
}

func TestScopeCountsKid(t *testing.T) {
	repo := seedDashboard(t)
	ctx := context.Background()

	kid := int64(1)
	got, err := repo.ScopeCounts(ctx, &kid, win())
	if err != nil {
		t.Fatalf("ScopeCounts: %v", err)
	}

	// A: 3 active (2+1 in September, 1 in August) plus 1 soft-deleted (ignored)
	// = 4; this month 3, last month 1.
	if got.ExamTotal != 4 {
		t.Errorf("ExamTotal = %d, want 4", got.ExamTotal)
	}
	if got.ExamThisMonth != 3 {
		t.Errorf("ExamThisMonth = %d, want 3", got.ExamThisMonth)
	}
	if got.ExamLastMonth != 1 {
		t.Errorf("ExamLastMonth = %d, want 1", got.ExamLastMonth)
	}
	if got.ExamPages != 4 {
		t.Errorf("ExamPages = %d, want 4", got.ExamPages)
	}
}

func TestScopeCountsAllAggregatesEveryKid(t *testing.T) {
	repo := seedDashboard(t)
	ctx := context.Background()

	got, err := repo.ScopeCounts(ctx, nil, win())
	if err != nil {
		t.Fatalf("ScopeCounts(nil): %v", err)
	}

	// All includes A and B: A 4 + B 1 = 5; this month 4; last month 1; pages 5
	if got.ExamTotal != 5 {
		t.Errorf("ExamTotal = %d, want 5", got.ExamTotal)
	}
	if got.ExamThisMonth != 4 {
		t.Errorf("ExamThisMonth = %d, want 4", got.ExamThisMonth)
	}
	if got.ExamLastMonth != 1 {
		t.Errorf("ExamLastMonth = %d, want 1", got.ExamLastMonth)
	}
	if got.ExamPages != 5 {
		t.Errorf("ExamPages = %d, want 5", got.ExamPages)
	}
}

func TestScopeCountsKidNeverExceedsAll(t *testing.T) {
	repo := seedDashboard(t)
	ctx := context.Background()

	all, err := repo.ScopeCounts(ctx, nil, win())
	if err != nil {
		t.Fatalf("ScopeCounts(nil): %v", err)
	}

	// The all scope is "everyone" rather than "other kids", so any kid side ≤ all side. This property
	// catches a common bug class: after missing the kid_id filter on the all side, the denominator
	// shrinks and the card prints contradictory numbers like "5 of 3".
	for _, kid := range []int64{1, 2} {
		got, err := repo.ScopeCounts(ctx, &kid, win())
		if err != nil {
			t.Fatalf("ScopeCounts(kid=%d): %v", kid, err)
		}
		if got.ExamTotal > all.ExamTotal {
			t.Errorf("kid=%d: ExamTotal %d > all %d", kid, got.ExamTotal, all.ExamTotal)
		}
		if got.ExamPages > all.ExamPages {
			t.Errorf("kid=%d: ExamPages %d > all %d", kid, got.ExamPages, all.ExamPages)
		}
	}
}

func TestUploadSeriesForKidGroupsByDay(t *testing.T) {
	repo := seedDashboard(t)
	ctx := context.Background()

	got, err := repo.UploadSeriesForKid(ctx, 1, win())
	if err != nil {
		t.Fatalf("UploadSeriesForKid: %v", err)
	}

	// A's dates with data in window 9/09~9/15: 9-10, 9-12.
	// B's 9-11 must be absent — upload streak is "how long this kid has persisted"; mixing in
	// someone else's exams would strip the line chart of meaning.
	want := map[string]int{"2026-09-10": 1, "2026-09-12": 1}
	if len(got) != len(want) {
		t.Fatalf("序列长度 = %d, want %d", len(got), len(want))
	}
	for _, p := range got {
		if want[p.Date.Format(dateLayout)] != p.Count {
			t.Errorf("桶 %s = %d，与期望不符", p.Date.Format(dateLayout), p.Count)
		}
	}
}

func TestUploadSeriesForKidsReturnsPerKidBucketsInRequestOrder(t *testing.T) {
	repo := seedDashboard(t)
	ctx := context.Background()

	// Fetch Jia, Yi and a non-existent kid 99 at once: results come back in
	// request order, and kids with no data are absent.
	got, err := repo.UploadSeriesForKids(ctx, []int64{1, 2, 99}, win())
	if err != nil {
		t.Fatalf("UploadSeriesForKids: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("返回孩子数 = %d, want 2（99 无数据不出现）", len(got))
	}
	requireKidOrder(t, got, 1, 2)

	// Jia: 9-10, 9-12 (9-13 soft-deleted must be ignored)
	requirePoints(t, got[0], map[string]int{"2026-09-10": 1, "2026-09-12": 1})
	requirePoints(t, got[1], map[string]int{"2026-09-11": 1})
}

func TestUploadSeriesForKidsEmptyInputIsNoop(t *testing.T) {
	repo := seedDashboard(t)
	got, err := repo.UploadSeriesForKids(context.Background(), nil, win())
	if err != nil {
		t.Fatalf("空输入不应报错: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("空输入 = %+v, want 空切片无元素", got)
	}
}

func TestUploadSeriesForKidsOmitsActiveKidWithNoExams(t *testing.T) {
	repo := seedDashboard(t)
	ctx := context.Background()

	// Add a third active kid Bing with no exams: GROUP BY only returns kids
	// with archives, so Bing is absent. That is the service's input contract —
	// PersistenceForKids completes active kids with zero archives into all-zero
	// series against the full kids table; the repo does not fabricate rows.
	const insertKid = `INSERT INTO kids (name, grade, gender, access_code, pin_hash, status, created_at, updated_at)
		VALUES (?, ?, 1, ?, '', ?, ?, ?)`
	if _, err := repo.db.Write().ExecContext(ctx, insertKid, "丙", 7, "code-c", domain.KidStatusActive, "2026-08-01 10:00:00", "2026-08-01 10:00:00"); err != nil {
		t.Fatalf("insert 丙: %v", err)
	}

	got, err := repo.UploadSeriesForKids(ctx, []int64{1, 3}, win())
	if err != nil {
		t.Fatalf("UploadSeriesForKids: %v", err)
	}
	if len(got) != 1 || got[0].KidID != 1 {
		t.Fatalf("返回 = %+v, want 只有甲（零归档的丙缺席，补零归 service）", got)
	}
	requirePoints(t, got[0], map[string]int{"2026-09-10": 1, "2026-09-12": 1})
}

// requireKidOrder asserts results appear in KidID order, stable across runs.
func requireKidOrder(t *testing.T, got []domain.KidUploadSeries, ids ...int64) {
	t.Helper()
	for i, id := range ids {
		if got[i].KidID != id {
			t.Fatalf("第 %d 个 = kid %d, want kid %d", i, got[i].KidID, id)
		}
	}
}

func requirePoints(t *testing.T, s domain.KidUploadSeries, want map[string]int) {
	t.Helper()
	if len(s.Points) != len(want) {
		t.Fatalf("kid %d 序列长度 = %d, want %d", s.KidID, len(s.Points), len(want))
	}
	for _, p := range s.Points {
		if want[p.Date.Format(dateLayout)] != p.Count {
			t.Errorf("kid %d 桶 %s = %d，与期望不符", s.KidID, p.Date.Format(dateLayout), p.Count)
		}
	}
}

func TestSubjectDistributionForKidOnlyArchivedSubjects(t *testing.T) {
	repo := seedDashboard(t)
	ctx := context.Background()

	got, err := repo.SubjectDistributionForKid(ctx, 1)
	if err != nil {
		t.Fatalf("SubjectDistributionForKid: %v", err)
	}
	// A: math 3 / chinese 1, by count descending; english 0 doesn't go into the donut
	if len(got) != 2 {
		t.Fatalf("分布项数 = %d, want 2", len(got))
	}
	if got[0].Name != "数学" || got[0].Count != 3 {
		t.Errorf("第 1 项 = %+v，want 数学/3", got[0])
	}
	if got[1].Name != "语文" || got[1].Count != 1 {
		t.Errorf("第 2 项 = %+v，want 语文/1", got[1])
	}
}

// TestSubjectDistributionForKidRestrictsToWhitelist: the distribution counts only the
// subjects in the kid's kid_subjects (no mask fallback). No rows → empty ("no records").
func TestSubjectDistributionForKidRestrictsToWhitelist(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	repo := NewDashboardRepoSQLite(db)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Write().ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	exec(`INSERT INTO subjects (id, code, name, sort_order) VALUES (1, 'math', '数学', 1)`)
	exec(`INSERT INTO subjects (id, code, name, sort_order) VALUES (2, 'chinese', '语文', 2)`)
	exec(`INSERT INTO kids (name, grade, gender, access_code, pin_hash, status, created_at, updated_at)
		VALUES ('甲', 8, 1, 'code-a', '', ?, '2026-08-01 10:00:00', '2026-08-01 10:00:00')`, domain.KidStatusActive)
	const insertExam = `INSERT INTO exams (kid_id, subject_id, exam_type, exam_date, status, uploaded_by,
		created_at, updated_at) VALUES (1, ?, 1, '2026-09-01', ?, ?, ?, ?)`
	exec(insertExam, 1, domain.ExamStatusActive, domain.UploadedByParent, "2026-09-01 09:00:00", "2026-09-01 09:00:00")
	exec(insertExam, 2, domain.ExamStatusActive, domain.UploadedByParent, "2026-09-01 09:00:00", "2026-09-01 09:00:00")

	// No kid_subjects rows: the kid has no subjects → empty distribution (no fallback).
	got, err := repo.SubjectDistributionForKid(ctx, 1)
	if err != nil {
		t.Fatalf("SubjectDistributionForKid (no whitelist): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("无 kid_subjects 分布项数 = %d, want 0", len(got))
	}

	// Configure the set to math only → chinese is dropped.
	exec(`INSERT INTO kid_subjects (kid_id, subject_id) VALUES (1, 1)`)
	got, err = repo.SubjectDistributionForKid(ctx, 1)
	if err != nil {
		t.Fatalf("SubjectDistributionForKid (whitelist): %v", err)
	}
	if len(got) != 1 || got[0].Name != "数学" {
		t.Fatalf("kid_subjects 分布 = %+v, want 仅 数学", got)
	}
}

func TestSubjectDistributionForKidEmptyIsNotNil(t *testing.T) {
	db := openTestDB(t)
	repo := NewDashboardRepoSQLite(db)
	mustExec(t, db, `INSERT INTO kids (name, grade, gender, access_code, pin_hash, status, created_at)
		VALUES ('乙', 8, 1, 'code-z', '', 1, '2026-08-01 10:00:00')`)

	got, err := repo.SubjectDistributionForKid(context.Background(), 1)
	if err != nil {
		t.Fatalf("SubjectDistributionForKid: %v", err)
	}
	// Must be non-nil: a nil slice serializes to null and breaks the
	// frontend's forEach.
	if got == nil {
		t.Fatal("分布 = nil，应为非 nil 空切片（DTO 会把 nil 序列化成 null）")
	}
}

func TestSubjectDistributionAllAggregatesAcrossKids(t *testing.T) {
	repo := seedDashboard(t)
	got, err := repo.SubjectDistributionAll(context.Background())
	if err != nil {
		t.Fatalf("SubjectDistributionAll: %v", err)
	}
	// A: math 3 + chinese 1; B: math 1 → all math 4, chinese 1, count
	// descending; soft-deleted rows are not counted.
	if len(got) != 2 {
		t.Fatalf("全部聚合分布项数 = %d, want 2", len(got))
	}
	if got[0].Name != "数学" || got[0].Count != 4 {
		t.Errorf("第 1 项 = %+v，want 数学/4（跨孩子聚合）", got[0])
	}
	if got[1].Name != "语文" || got[1].Count != 1 {
		t.Errorf("第 2 项 = %+v，want 语文/1", got[1])
	}
}

func TestScopeCountsOnUnknownKidIsZeroNotError(t *testing.T) {
	repo := seedDashboard(t)
	ctx := context.Background()

	// kid 99 doesn't exist: the aggregate query must not error, all counts 0
	kid := int64(99)
	got, err := repo.ScopeCounts(ctx, &kid, win())
	if err != nil {
		t.Fatalf("ScopeCounts 对不存在的 kid 不应报错: %v", err)
	}
	if got.ExamTotal != 0 || got.ExamPages != 0 {
		t.Errorf("kid 99 = %+v, want 全 0", got)
	}
}

func mustExec(t *testing.T, db *DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Write().ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}
