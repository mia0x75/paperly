package worker

import (
	"context"

	"paperly/internal/domain"
)

// kind=3 backup: run it, then notify the parent.
//
// The notice is informational, not the result: the backup has already succeeded or failed, so a
// notice-write failure is only logged—it must not flip a successful backup to failed.
func handleBackup(ctx context.Context, deps Deps, task *domain.Task) error {
	if err := deps.Backup.CreateBackup(ctx); err != nil {
		if _, nerr := deps.Notify.CreateParentNotice(ctx, domain.MessageKindBackupFailed,
			"自动备份失败", err.Error(), ""); nerr != nil {
			deps.Log.Warn("backup-failure notice write failed", "task_id", task.ID, "err", nerr)
		}
		return err
	}
	if _, nerr := deps.Notify.CreateParentNotice(ctx, domain.MessageKindBackupDone,
		"自动备份完成", "数据库与试卷文件已自动备份。", ""); nerr != nil {
		deps.Log.Warn("backup-success notice write failed", "task_id", task.ID, "err", nerr)
	}
	return nil
}
