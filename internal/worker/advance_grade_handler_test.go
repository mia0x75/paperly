package worker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

const (
	det7Grad7Target8 = `{"kid_id":7,"enrolled_year":2025,"enrolled_grade":7,"target_grade":8}`
	det12Grad12T13   = `{"kid_id":7,"enrolled_year":2025,"enrolled_grade":12,"target_grade":13}`
)

func mkActiveKid(grade int) map[int64]*domain.Kid {
	return map[int64]*domain.Kid{7: {ID: 7, Name: "小明", Grade: grade, Status: domain.KidStatusActive}}
}

func TestAdvanceGradePromotesGrade(t *testing.T) {
	kids := &fakeKidStore{kids: mkActiveKid(7)}
	notifier := &fakeNotifier{}
	deps := testDeps()
	deps.Kids = kids
	deps.Notify = notifier

	err := handleAdvanceGrade(context.Background(), deps,
		&domain.Task{ID: 1, Kind: domain.TaskKindAdvanceGrade, Detail: det7Grad7Target8})
	require.NoError(t, err, "grade=7 的学生应正常推进")

	require.Len(t, kids.updates, 1, "改 grade")
	// current school year 2026 - enrolled_year 2025 = 1; target 8 + 1 = 9.
	require.Equal(t, 9, kids.updates[0].Grade)
	require.Empty(t, kids.graduated, "≤12 不毕业")
	require.Len(t, notifier.calls, 2, "推进成功通知 + 初三科目提醒")
	require.Equal(t, domain.MessageKindGradeAdvanced, notifier.calls[0].kind)
	require.Equal(t, "/kids/7", notifier.calls[0].link)
	require.Equal(t, "已自动升级", notifier.calls[0].title)
	// rawTarget==9 fires the "grade 9 subjects wrapped up" reminder.
	require.Equal(t, domain.MessageKindGrade9Reminder, notifier.calls[1].kind)
	require.Equal(t, "/settings/subjects", notifier.calls[1].link)
	require.Contains(t, notifier.calls[1].body, "生物 / 地理")

	// The student also gets their own grade-change notice, bound to their kid_id.
	require.Len(t, notifier.kidCalls, 1, "改年级还应通知孩子本人")
	require.Equal(t, domain.MessageKindGradeChanged, notifier.kidCalls[0].kind)
	require.EqualValues(t, 7, notifier.kidCalls[0].kidID)
	require.Equal(t, "/kid", notifier.kidCalls[0].link)
	require.Contains(t, notifier.kidCalls[0].body, "9 年级")
}

// Promoting into senior high (rawTarget 10) fires the "set arts/science track" reminder, linked to
// the kid's own page. enrolled_year=2026 keeps the year delta at 0, so rawTarget lands on 10.
func TestAdvanceGradeGrade10Reminder(t *testing.T) {
	const det = `{"kid_id":7,"enrolled_year":2026,"enrolled_grade":9,"target_grade":10}`
	notifier := &fakeNotifier{}
	deps := testDeps()
	deps.Kids = &fakeKidStore{kids: mkActiveKid(9)}
	deps.Notify = notifier

	err := handleAdvanceGrade(context.Background(), deps,
		&domain.Task{ID: 11, Kind: domain.TaskKindAdvanceGrade, Detail: det})
	require.NoError(t, err)

	require.Len(t, notifier.calls, 2, "推进成功通知 + 高一文理提醒")
	require.Equal(t, domain.MessageKindGradeAdvanced, notifier.calls[0].kind)
	// rawTarget==10 fires the senior-high arts/science reminder.
	require.Equal(t, domain.MessageKindGrade10Reminder, notifier.calls[1].kind)
	require.Equal(t, "/kids/7", notifier.calls[1].link)
	require.Contains(t, notifier.calls[1].body, "高一")
	require.Contains(t, notifier.calls[1].body, "文理科")
}

func TestAdvanceGradeNoGrade9ReminderAtOtherGrades(t *testing.T) {
	// enrolled_grade=6, target=7 → rawTarget 8: promoted but not into grade 9.
	const det = `{"kid_id":7,"enrolled_year":2025,"enrolled_grade":6,"target_grade":7}`
	notifier := &fakeNotifier{}
	deps := testDeps()
	deps.Kids = &fakeKidStore{kids: mkActiveKid(6)}
	deps.Notify = notifier

	err := handleAdvanceGrade(context.Background(), deps,
		&domain.Task{ID: 9, Kind: domain.TaskKindAdvanceGrade, Detail: det})
	require.NoError(t, err)
	require.Len(t, notifier.calls, 1, "非初三不额外提醒")
	require.Equal(t, domain.MessageKindGradeAdvanced, notifier.calls[0].kind)
	require.Len(t, notifier.kidCalls, 1, "普通升级也通知孩子本人（与年级提醒无关）")
	require.EqualValues(t, 7, notifier.kidCalls[0].kidID)
}

// Promoting into junior high (rawTarget 7) fires the "verify subjects are complete" reminder,
// linked to the kid's own page. enrolled_year=2026 keeps the year delta at 0, so rawTarget lands
// on 7.
func TestAdvanceGradeGrade7Reminder(t *testing.T) {
	const det = `{"kid_id":7,"enrolled_year":2026,"enrolled_grade":6,"target_grade":7}`
	notifier := &fakeNotifier{}
	deps := testDeps()
	deps.Kids = &fakeKidStore{kids: mkActiveKid(6)}
	deps.Notify = notifier

	err := handleAdvanceGrade(context.Background(), deps,
		&domain.Task{ID: 12, Kind: domain.TaskKindAdvanceGrade, Detail: det})
	require.NoError(t, err)

	require.Len(t, notifier.calls, 2, "推进成功通知 + 初一科目提醒")
	require.Equal(t, domain.MessageKindGradeAdvanced, notifier.calls[0].kind)
	require.Equal(t, domain.MessageKindGrade7Reminder, notifier.calls[1].kind)
	require.Equal(t, "/kids/7", notifier.calls[1].link)
	require.Contains(t, notifier.calls[1].body, "初一")
}

func TestAdvanceGradeGraduatesWhenRawTargetOver12(t *testing.T) {
	kids := &fakeKidStore{kids: mkActiveKid(12)}
	notifier := &fakeNotifier{}
	deps := testDeps()
	deps.Kids = kids
	deps.Notify = notifier

	err := handleAdvanceGrade(context.Background(), deps,
		&domain.Task{ID: 2, Kind: domain.TaskKindAdvanceGrade, Detail: det12Grad12T13})
	require.NoError(t, err)

	require.Equal(t, []int64{7}, kids.graduated, "raw_target=14>12 应毕业")
	require.Empty(t, kids.updates, "毕业不改 grade")
	require.Len(t, notifier.calls, 1, "毕业同样通知家长")
	require.Equal(t, domain.MessageKindGradeAdvanced, notifier.calls[0].kind)
	require.Empty(t, notifier.kidCalls, "毕业不是年级更新，不通知孩子")
}

func TestAdvanceGradeSkipsWhenParentChangedGrade(t *testing.T) {
	kids := &fakeKidStore{kids: mkActiveKid(9)}
	deps := testDeps()
	deps.Kids = kids
	deps.Notify = &fakeNotifier{}

	err := handleAdvanceGrade(context.Background(), deps,
		&domain.Task{ID: 3, Kind: domain.TaskKindAdvanceGrade, Detail: det7Grad7Target8})
	requireSkipped(t, err, "grade manually overridden by parent")
	require.Empty(t, kids.updates, "幂等门不成立不改")
	require.Empty(t, kids.graduated)
}

func TestAdvanceGradeSkipsNonActive(t *testing.T) {
	kids := &fakeKidStore{kids: map[int64]*domain.Kid{7: {ID: 7, Grade: 7, Status: domain.KidStatusGraduated}}}
	deps := testDeps()
	deps.Kids = kids
	deps.Notify = &fakeNotifier{}

	err := handleAdvanceGrade(context.Background(), deps,
		&domain.Task{ID: 4, Kind: domain.TaskKindAdvanceGrade, Detail: det7Grad7Target8})
	requireSkipped(t, err, "kid is not in active status")
	require.Empty(t, kids.updates)
}

func TestAdvanceGradeSkipsKidNotFound(t *testing.T) {
	deps := testDeps()
	deps.Kids = &fakeKidStore{}
	deps.Notify = &fakeNotifier{}

	err := handleAdvanceGrade(context.Background(), deps,
		&domain.Task{ID: 5, Kind: domain.TaskKindAdvanceGrade, Detail: det7Grad7Target8})
	requireSkipped(t, err, "kid not found (possibly soft-deleted)")
}

func TestAdvanceGradeMalformedDetail(t *testing.T) {
	deps := testDeps()
	deps.Kids = &fakeKidStore{kids: mkActiveKid(7)}

	err := handleAdvanceGrade(context.Background(), deps,
		&domain.Task{ID: 6, Kind: domain.TaskKindAdvanceGrade, Detail: "not-json"})
	requireNonRetryable(t, err)
}

func TestAdvanceGradeMissingField(t *testing.T) {
	deps := testDeps()

	err := handleAdvanceGrade(context.Background(), deps,
		&domain.Task{ID: 7, Kind: domain.TaskKindAdvanceGrade, Detail: `{"kid_id":7,"enrolled_year":2025,"target_grade":8}`})
	requireNonRetryable(t, err)
}

// A notice write failure is only logged; the advance result is unaffected.
func TestAdvanceGradeNotifyFailureDoesNotFailTask(t *testing.T) {
	kids := &fakeKidStore{kids: mkActiveKid(7)}
	deps := testDeps()
	deps.Kids = kids
	deps.Notify = &fakeNotifier{err: errNotifyFailed}

	err := handleAdvanceGrade(context.Background(), deps,
		&domain.Task{ID: 8, Kind: domain.TaskKindAdvanceGrade, Detail: det7Grad7Target8})
	require.NoError(t, err, "通知失败不应把成功推进标 failed")
	require.Len(t, kids.updates, 1)
}
