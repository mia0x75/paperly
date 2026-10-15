// Package health implements the data health check.
//
// Positioning: a single Box machine has no log aggregation and no monitoring
// dashboard, so the health page is the *only* observability entry for parents /
// operators. Therefore each check answers a concrete question rather than "run
// SELECT 1".
//
// There are 7 checks:
//
//	database / schema_version / last_backup /
//	filesystem / recent_window_fail / concurrency / maintenance_cursor
//
// maintenance_cursor answers the kind of silent failure where "everything else is
// green but the data is still wrong"—undetectable unless checked.
package health

import (
	"context"
	"fmt"
	"time"

	"paperly/internal/domain"
)

type CheckStatus string

// Three states rather than two: ok / warn / error. warn is "usable but worth a
// glance"; separating it from error keeps real failures from being drowned out
// by a pile of small warnings.
const (
	StatusOK    CheckStatus = "ok"
	StatusWarn  CheckStatus = "warn"
	StatusError CheckStatus = "error"
)

// Health-check thresholds (centralized here, for tuning per Box model).
const (
	// WarnDBSizeMB: a main DB larger than this prompts considering backup/cleanup.
	WarnDBSizeMB = 2048
	// WarnBackupAgeHours: warn if the latest backup is older than this many hours
	// (default 25h, i.e. one missed backup cycle).
	WarnBackupAgeHours = 25
	// WarnRecentWindowFail: warn if recent_window parse failures in the last 7
	// days exceed this count (the AC1 example says >20).
	WarnRecentWindowFail = 20
	// WarnWriteQueueWaitMS: warn if the write queue's longest wait exceeds this
	// many milliseconds.
	WarnWriteQueueWaitMS = 1000
	// RecentWindowDays is the metric statistics window (last 7 days).
	RecentWindowDays = 7
)

type Check struct {
	Key        string      `json:"key"`
	Title      string      `json:"title"`
	Status     CheckStatus `json:"status"`
	Value      string      `json:"value"`
	Detail     string      `json:"detail"`
	MeasuredAt time.Time   `json:"measured_at"`
}

// ok / warn / error are three constructor helpers, saving an if/else everywhere.
func ok(key, title, value, detail string) Check {
	return Check{Key: key, Title: title, Status: StatusOK, Value: value, Detail: detail}
}

func warn(key, title, value, detail string) Check {
	return Check{Key: key, Title: title, Status: StatusWarn, Value: value, Detail: detail}
}

func fail(key, title, value, detail string) Check {
	return Check{Key: key, Title: title, Status: StatusError, Value: value, Detail: detail}
}

type Report struct {
	Status    CheckStatus `json:"status"`
	Checks    []Check     `json:"checks"`
	Summary   string      `json:"summary"`
	Elapsed   string      `json:"elapsed"`
	CheckedAt time.Time   `json:"checked_at"`
}

type Service struct {
	opsRepo    domain.OpsRepository
	metrics    domain.SystemMetricsRepository
	cursorRepo domain.MaintenanceCursorRepository
	dataDir    string
	nowFn      func() time.Time
}

func NewService(
	opsRepo domain.OpsRepository,
	metrics domain.SystemMetricsRepository,
	cursorRepo domain.MaintenanceCursorRepository,
	dataDir string,
) *Service {
	return &Service{opsRepo: opsRepo, metrics: metrics, cursorRepo: cursorRepo, dataDir: dataDir, nowFn: time.Now}
}

// Check runs all health-check items and returns the report.
//
// A single item's failure does not interrupt the whole—the value of the health
// page is that "when one thing is down, the rest is still visible". Only when the
// database is completely unreadable do all items degrade together.
func (s *Service) Check(ctx context.Context) *Report {
	started := s.nowFn()
	report := &Report{Status: StatusOK, CheckedAt: started}

	report.Checks = []Check{
		s.checkDatabase(ctx),
		s.checkSchemaVersion(ctx),
		s.checkLastBackup(ctx),
		s.checkFilesystem(),
		s.checkConcurrency(ctx),
		s.checkMaintenanceCursor(ctx),
	}

	// Aggregation: any error makes it error; otherwise any warn makes it warn.
	var warns, errs int
	for _, c := range report.Checks {
		switch c.Status {
		case StatusWarn:
			warns++
		case StatusError:
			errs++
		}
	}
	switch {
	case errs > 0:
		report.Status = StatusError
	case warns > 0:
		report.Status = StatusWarn
	}
	report.Summary = fmt.Sprintf("共 %d 项，告警 %d 项，故障 %d 项", len(report.Checks), warns, errs)
	report.Elapsed = s.nowFn().Sub(started).String()
	return report
}

// checkDatabase check 1: main DB size + quick_check.
func (s *Service) checkDatabase(ctx context.Context) Check {
	const key, title = "database", "数据库"
	size, err := s.opsRepo.DBSize(ctx)
	if err != nil {
		return fail(key, title, "unknown", "failed to read database file size: "+err.Error())
	}
	integrity, err := s.opsRepo.IntegrityCheck(ctx)
	if err != nil {
		return fail(key, title, HumanBytes(size), "integrity check failed: "+err.Error())
	}
	value := HumanBytes(size)
	if integrity != "ok" {
		return fail(key, title, value, "integrity check failed: "+integrity)
	}
	if size > WarnDBSizeMB*1024*1024 {
		return warn(key, title, value, "库体积偏大，建议检查是否有可清理的历史数据")
	}
	return ok(key, title, value, "完整性校验通过")
}

// checkSchemaVersion check 2: dynamically read display_tag (with a fallback).
//
// Never hardcode the version number—"changing the version but forgetting the
// health page" is exactly the bug class this is meant to eliminate.
func (s *Service) checkSchemaVersion(ctx context.Context) Check {
	const key, title = "schema_version", "数据库结构版本"
	tag, err := s.opsRepo.SchemaDisplayTag(ctx)
	if err != nil {
		return fail(key, title, "unknown", "failed to read schema_version: "+err.Error())
	}
	if tag == "unknown" {
		// Fallback rather than crash: both an empty and an old DB must open the
		// health page.
		return warn(key, title, "unknown", "版本记录缺失，建议执行迁移/校验")
	}
	return ok(key, title, tag, "动态读取版本记录")
}

// checkLastBackup check 3: latest backup (the backup_last_at metric).
func (s *Service) checkLastBackup(ctx context.Context) Check {
	const key, title = "last_backup", "最近备份"
	ts, found, err := s.metrics.LatestAt(ctx, domain.MetricBackupLastAt)
	if err != nil {
		return fail(key, title, "unknown", "failed to read backup metrics: "+err.Error())
	}
	if !found {
		return warn(key, title, "从未备份", "尚无成功备份记录，建议立即执行一次")
	}
	value := ts.Local().Format("2006-01-02 15:04")
	age := s.nowFn().Sub(ts)
	if age > WarnBackupAgeHours*time.Hour {
		return warn(key, title, value, fmt.Sprintf("距上次备份已 %s，超过 %d 小时周期", age.Round(time.Hour), WarnBackupAgeHours))
	}
	return ok(key, title, value, fmt.Sprintf("距今 %s", age.Round(time.Hour)))
}

// checkFilesystem check 6: data directory usage and free disk space.
func (s *Service) checkFilesystem() Check {
	const key, title = "filesystem", "文件系统"
	used, err := dirSize(s.dataDir)
	if err != nil {
		return warn(key, title, "unknown", "统计数据目录失败："+err.Error())
	}
	usage, supported := filesystemUsage(s.dataDir)
	if !supported {
		// Platform does not support statfs (e.g. Windows)—degrade to "report used
		// only", without fabricating a total.
		return ok(key, title, HumanBytes(used)+"（本平台不提供剩余空间）", "数据目录统计")
	}
	if usage.Free == 0 || usage.Total == 0 {
		return warn(key, title, HumanBytes(used), "无法读取磁盘容量")
	}
	pct := float64(used) / float64(usage.Total) * 100
	value := fmt.Sprintf("%s / %s（%.1f%%）", HumanBytes(used), HumanBytes(int64(usage.Total)), pct)
	if pct > 90 {
		return warn(key, title, value, "磁盘占用超过 90%，请清理或扩容")
	}
	return ok(key, title, value, fmt.Sprintf("剩余 %s", HumanBytes(int64(usage.Free))))
}

// checkRecentWindow check 8: recent_window parse failure count in the last 7
// days (AC1).
//
// This item is why AC1 exists: "data pending repair" is an in-memory variable
// that resets to zero on restart, so system_metrics — its only persistence
// location — is what lets the health page report the real count.
//
//nolint:unused
func (s *Service) checkRecentWindow(ctx context.Context) Check {
	const key, title = "recent_window_fail", "近期窗口解析失败"
	n, err := s.metrics.SumSince(ctx, domain.MetricRecentWindowParseFail, RecentWindowDays)
	if err != nil {
		return fail(key, title, "unknown", "failed to read metrics: "+err.Error())
	}
	value := fmt.Sprintf("近 %d 天 %d 次", RecentWindowDays, n)
	if n > WarnRecentWindowFail {
		return warn(key, title, value, fmt.Sprintf("超过 %d 次，近期表现权重可能失真，建议执行全量重算", WarnRecentWindowFail))
	}
	return ok(key, title, value, "无异常")
}

// checkConcurrency check 9: three-chain concurrency conflict.
func (s *Service) checkConcurrency(ctx context.Context) Check {
	const key, title = "concurrency", "并发冲突"
	rollbacks, err := s.metrics.SumSince(ctx, domain.MetricWriteConflictRollback, RecentWindowDays)
	if err != nil {
		return fail(key, title, "unknown", "failed to read concurrency metrics: "+err.Error())
	}
	waitMS, err := s.metrics.MaxSince(ctx, domain.MetricWriteQueueMaxWaitMS, RecentWindowDays)
	if err != nil {
		return fail(key, title, "unknown", "failed to read concurrency metrics: "+err.Error())
	}
	interrupted, err := s.metrics.SumSince(ctx, domain.MetricMaintenanceInterrupted, RecentWindowDays)
	if err != nil {
		return fail(key, title, "unknown", "failed to read concurrency metrics: "+err.Error())
	}
	value := fmt.Sprintf("回滚 %d 次 / 最长排队 %dms / 维护被打断 %d 次", rollbacks, waitMS, interrupted)
	detail := fmt.Sprintf("近 %d 天统计", RecentWindowDays)
	if waitMS > WarnWriteQueueWaitMS {
		return warn(key, title, value, detail+fmt.Sprintf("；排队超过 %dms，写队列可能成为瓶颈", WarnWriteQueueWaitMS))
	}
	if rollbacks > 0 {
		return warn(key, title, value, detail+"；存在写版本冲突，建议检查是否有多进程同时写入")
	}
	return ok(key, title, value, detail)
}

// checkMaintenanceCursor check 10: daily maintenance cursor.
func (s *Service) checkMaintenanceCursor(ctx context.Context) Check {
	const key, title = "maintenance_cursor", "每日维护游标"
	c, err := s.cursorRepo.Get(ctx, "daily_mastery")
	if err != nil {
		return warn(key, title, "未运行", "尚无维护记录，维护将在首日调度时创建游标")
	}
	value := fmt.Sprintf("id=%d / %d", c.LastID, c.LastStatus)
	if c.LastStatus == domain.CursorStatusRunning {
		return warn(key, title, value, "上次维护未跑完（可续跑），建议检查是否频繁超时")
	}
	return ok(key, title, value, "游标正常")
}

// HumanBytes converts a byte count to a human-readable string.
//
// Exported for reuse by the DTO layer: DB size and assets size are the same
// quantity, and two separate formatters would eventually produce different
// strings at the GB→TB boundary.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 3; v /= unit {
		div *= unit
		exp++
	}
	suffix := [...]string{"KB", "MB", "GB", "TB"}[exp]
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), suffix)
}
