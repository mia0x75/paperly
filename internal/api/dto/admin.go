package dto

import (
	"time"

	"paperly/internal/backup"
	"paperly/internal/health"
)

// DTOs for the operations API.
//
// Principle: expose only information a parent can understand. Absolute backup paths, seed directory
// locations, and database file paths are all kept out of the DTO — the frontend only needs the file name and size.
//
// BackupCreated is the receipt for POST /api/admin/backup.
type BackupCreated struct {
	Timestamp   string `json:"timestamp"`
	Tier        string `json:"tier"`
	FileName    string `json:"file_name"`
	Bytes       int64  `json:"bytes"`
	Integrity   string `json:"integrity"`
	AssetsBytes int64  `json:"assets_bytes"`
	CreatedAt   string `json:"created_at"`
	Pruned      int    `json:"pruned"`
}

// BackupListResponse is the data part of GET /api/admin/backups.
type BackupListResponse struct {
	Items []backup.Info `json:"items"`
	Total int           `json:"total"`
	// LastAt only has a value when HasBackup is true (no backup means there is no "most recent backup time").
	LastAt    string `json:"last_backup_at"`
	HasBackup bool   `json:"has_backup"`
}

// NewBackupCreated assembles the backup receipt DTO.
func NewBackupCreated(m *backup.Manifest, pruned int) BackupCreated {
	return BackupCreated{
		Timestamp:   m.Timestamp,
		Tier:        m.Tier,
		FileName:    m.DBFile,
		Bytes:       m.DBBytes,
		Integrity:   m.DBIntegrity,
		AssetsBytes: m.AssetsBytes,
		CreatedAt:   m.CreatedAt,
		Pruned:      pruned,
	}
}

// NewBackupListResponse assembles the backup list DTO.
func NewBackupListResponse(items []backup.Info, lastAt time.Time, hasBackup bool) BackupListResponse {
	list := items
	if list == nil {
		list = []backup.Info{}
	}
	last := ""
	if hasBackup {
		last = lastAt.Local().Format("2006-01-02 15:04")
	}
	return BackupListResponse{Items: list, Total: len(list), LastAt: last, HasBackup: hasBackup}
}

// HealthResponse is the receipt for GET /api/admin/health.
type HealthResponse struct {
	Status    string         `json:"status"`
	Summary   string         `json:"summary"`
	Elapsed   string         `json:"elapsed"`
	CheckedAt string         `json:"checked_at"`
	Checks    []health.Check `json:"checks"`
}

// NewHealthResponse assembles the health report DTO.
func NewHealthResponse(r *health.Report) HealthResponse {
	checks := r.Checks
	if checks == nil {
		checks = []health.Check{}
	}
	return HealthResponse{
		Status:    string(r.Status),
		Summary:   r.Summary,
		Elapsed:   r.Elapsed,
		CheckedAt: r.CheckedAt.Local().Format("2006-01-02 15:04:05"),
		Checks:    checks,
	}
}
