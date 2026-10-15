package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

// TestWithTxRollsBackWrites proves every repository's WithTx routes its writes into the
// caller's transaction: the fixture writes a row, then returns a sentinel error so
// WithinTx rolls back — the row must be absent afterwards.
func TestWithTxRollsBackWrites(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	sentinel := errors.New("rollback sentinel")

	subjectRepo := NewSubjectRepoSQLite(db)
	baseSubjectID, err := subjectRepo.Insert(ctx, &domain.Subject{Code: "base", Name: "基础", SortOrder: 1})
	require.NoError(t, err)
	kidID, err := NewKidRepoSQLite(db).Insert(ctx, newTestKid("小明", "a7k2m9p4"))
	require.NoError(t, err)

	grade := 9
	examID, err := NewExamRepoSQLite(db).Insert(ctx, &domain.Exam{
		Grade:      &grade,
		SubjectID:  &baseSubjectID,
		KidID:      &kidID,
		UploadedBy: domain.UploadedByParent,
		Status:     domain.ExamStatusActive,
	})
	require.NoError(t, err)

	cases := []struct {
		name      string
		apply     func(tx domain.Tx) error
		countSQL  string
		countArgs []any
	}{
		{
			name: "kid",
			apply: func(tx domain.Tx) error {
				_, err := NewKidRepoSQLite(db).WithTx(tx).Insert(ctx, newTestKid("小红", "b8k2m9p4"))
				return err
			},
			countSQL: `SELECT COUNT(*) FROM kids WHERE name = '小红'`,
		},
		{
			name: "subject",
			apply: func(tx domain.Tx) error {
				_, err := subjectRepo.WithTx(tx).Insert(ctx, &domain.Subject{Code: "tx-sub", Name: "事务科目", SortOrder: 2})
				return err
			},
			countSQL: `SELECT COUNT(*) FROM subjects WHERE code = 'tx-sub'`,
		},
		{
			name: "knowledge_point",
			apply: func(tx domain.Tx) error {
				_, err := NewKnowledgePointRepoSQLite(db).WithTx(tx).Insert(ctx, &domain.KnowledgePoint{
					SubjectID: baseSubjectID, Name: "事务考点", SortOrder: 1,
				})
				return err
			},
			countSQL: `SELECT COUNT(*) FROM knowledge_points WHERE name = '事务考点'`,
		},
		{
			name: "exam",
			apply: func(tx domain.Tx) error {
				_, err := NewExamRepoSQLite(db).WithTx(tx).Insert(ctx, &domain.Exam{
					Title: nil, Grade: &grade, SubjectID: &baseSubjectID, KidID: &kidID,
					UploadedBy: domain.UploadedByParent, Status: domain.ExamStatusActive,
				})
				return err
			},
			countSQL:  `SELECT COUNT(*) FROM exams WHERE id > ?`,
			countArgs: []any{examID},
		},
		{
			name: "page",
			apply: func(tx domain.Tx) error {
				_, err := NewPageRepoSQLite(db).WithTx(tx).Insert(ctx, &domain.Page{
					ExamID: examID, PageIndex: 1, FilePath: "assets/exams/tx/p1.jpg", SHA256: "deadbeef",
				})
				return err
			},
			countSQL: `SELECT COUNT(*) FROM pages WHERE file_path = 'assets/exams/tx/p1.jpg'`,
		},
		{
			name: "task",
			apply: func(tx domain.Tx) error {
				_, err := NewTaskRepoSQLite(db).WithTx(tx).Insert(ctx, &domain.Task{
					Kind: domain.TaskKindOther, Priority: 1, Status: domain.TaskStatusPending, Detail: "{}",
				})
				return err
			},
			countSQL: `SELECT COUNT(*) FROM tasks WHERE detail = '{}'`,
		},
		{
			name: "message",
			apply: func(tx domain.Tx) error {
				_, err := NewMessageRepoSQLite(db).WithTx(tx).Insert(ctx, &domain.Message{
					Audience: domain.MessageAudienceParent, Kind: domain.MessageKindBackupDone, Title: "事务通知",
				})
				return err
			},
			countSQL: `SELECT COUNT(*) FROM messages WHERE title = '事务通知'`,
		},
		{
			name: "user",
			apply: func(tx domain.Tx) error {
				_, err := NewUserRepoSQLite(db).WithTx(tx).Insert(ctx, &domain.User{
					DisplayName: "事务家长", PinHash: "!", PinVersion: 1, Status: domain.UserStatusActive,
				})
				return err
			},
			countSQL: `SELECT COUNT(*) FROM users WHERE display_name = '事务家长'`,
		},
		{
			name: "system_metric",
			apply: func(tx domain.Tx) error {
				return NewSystemMetricRepoSQLite(db).WithTx(tx).Inc(ctx, "tx_metric")
			},
			countSQL: `SELECT COUNT(*) FROM metrics WHERE metric_name = 'tx_metric'`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := db.WithinTx(ctx, func(tx domain.Tx) error {
				if err := tc.apply(tx); err != nil {
					return err
				}
				return sentinel
			})
			require.ErrorIs(t, err, sentinel)

			var n int
			require.NoError(t, db.Read().QueryRowContext(ctx, tc.countSQL, tc.countArgs...).Scan(&n))
			require.Zero(t, n, "rollback must leave no row")
		})
	}
}

// TestWithTxCommitPersists proves the bound handles write through the transaction when it commits.
func TestWithTxCommitPersists(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	err := db.WithinTx(ctx, func(tx domain.Tx) error {
		_, err := NewKidRepoSQLite(db).WithTx(tx).Insert(ctx, newTestKid("小花", "c9k2m9p4"))
		return err
	})
	require.NoError(t, err)

	exists, err := NewKidRepoSQLite(db).ExistsByName(ctx, "小花", 0)
	require.NoError(t, err)
	require.True(t, exists)
}

// TestKidSubjectReplaceWithinTxRollsBackKeepsPrevious: replacing a kid's
// whitelist inside a rolled-back transaction must keep the committed
// whitelist, so there is no clear-then-fail window.
func TestKidSubjectReplaceWithinTxRollsBackKeepsPrevious(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	kidID, chineseID, mathID := seedKidAndSubjects(t, db)
	repo := NewKidSubjectRepoSQLite(db)
	require.NoError(t, repo.ReplaceAll(ctx, kidID, []int64{chineseID, mathID}))

	sentinel := errors.New("boom")
	err := db.WithinTx(ctx, func(tx domain.Tx) error {
		if err := repo.WithTx(tx).ReplaceAll(ctx, kidID, []int64{mathID}); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	ids, err := repo.ListSubjectIDs(ctx, kidID)
	require.NoError(t, err)
	require.Equal(t, []int64{chineseID, mathID}, ids, "rollback keeps the previous whitelist intact")
}

// TestWithTxRejectsWrongTypeHandle proves a wrong-type transaction handle fails
// loudly instead of silently running outside the caller's transaction (which
// would lose atomicity without a trace).
func TestWithTxRejectsWrongTypeHandle(t *testing.T) {
	db := openTestDB(t)
	repo := NewKidRepoSQLite(db)
	require.Panics(t, func() { _ = repo.WithTx("not-a-transaction") })
	require.NotPanics(t, func() { _ = repo.WithTx(nil) })
}
