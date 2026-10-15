package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"paperly/internal/domain"
)

var errBackupFailed = errors.New("insufficient disk space")

func TestBackupSuccessNotifies(t *testing.T) {
	backup := &fakeBackup{}
	notifier := &fakeNotifier{}
	deps := testDeps()
	deps.Backup = backup
	deps.Notify = notifier

	err := handleBackup(context.Background(), deps,
		&domain.Task{ID: 1, Kind: domain.TaskKindBackup, Detail: "{}"})
	require.NoError(t, err)
	require.Equal(t, 1, backup.calls)
	require.Len(t, notifier.calls, 1, "备份成功通知家长")
	require.Equal(t, domain.MessageKindBackupDone, notifier.calls[0].kind)
	require.Equal(t, "自动备份完成", notifier.calls[0].title)
}

func TestBackupFailureNotifiesAndFails(t *testing.T) {
	backup := &fakeBackup{err: errBackupFailed}
	notifier := &fakeNotifier{}
	deps := testDeps()
	deps.Backup = backup
	deps.Notify = notifier

	err := handleBackup(context.Background(), deps,
		&domain.Task{ID: 2, Kind: domain.TaskKindBackup, Detail: "{}"})
	require.ErrorIs(t, err, errBackupFailed)
	require.Len(t, notifier.calls, 1)
	require.Equal(t, domain.MessageKindBackupFailed, notifier.calls[0].kind)
	require.Equal(t, errBackupFailed.Error(), notifier.calls[0].body)
}

// A notice-write failure is only logged; the backup result is unchanged.
func TestBackupNotifyFailureDoesNotChangeResult(t *testing.T) {
	backup := &fakeBackup{}
	notifier := &fakeNotifier{err: errNotifyFailed}
	deps := testDeps()
	deps.Backup = backup
	deps.Notify = notifier

	err := handleBackup(context.Background(), deps,
		&domain.Task{ID: 3, Kind: domain.TaskKindBackup, Detail: "{}"})
	require.NoError(t, err, "成功 + 通知失败 → 任务仍成功")
	require.Len(t, notifier.calls, 1)
}
