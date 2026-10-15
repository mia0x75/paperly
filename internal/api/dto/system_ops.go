package dto

import "paperly/internal/domain"

// SchemaMigrationSummary is one applied migration record (the settings page "database schema version" list).
type SchemaMigrationSummary struct {
	Version    int    `json:"version"`
	Name       string `json:"name"`
	DisplayTag string `json:"display_tag"`
	AppliedAt  string `json:"applied_at"`
}

// CursorSummary is a task cursor (the settings page "task cursor persistence" list).
type CursorSummary struct {
	TaskName   string `json:"task_name"`
	LastID     int64  `json:"last_id"`
	LastRunAt  string `json:"last_run_at"`
	LastStatus int    `json:"last_status"`
}

// NewSchemaMigrationSummarys maps migration records in batch.
func NewSchemaMigrationSummarys(rows []*domain.SchemaMigration) []SchemaMigrationSummary {
	out := make([]SchemaMigrationSummary, 0, len(rows))
	for _, m := range rows {
		d := SchemaMigrationSummary{
			Version:   m.Version,
			Name:      m.Name,
			AppliedAt: m.AppliedAt.Local().Format("2006-01-02 15:04:05"),
		}
		if m.DisplayTag != nil {
			d.DisplayTag = *m.DisplayTag
		}
		out = append(out, d)
	}
	return out
}

// NewCursorSummarys maps cursors in batch.
func NewCursorSummarys(rows []*domain.MaintenanceCursor) []CursorSummary {
	out := make([]CursorSummary, 0, len(rows))
	for _, c := range rows {
		d := CursorSummary{
			TaskName:   c.TaskName,
			LastID:     c.LastID,
			LastStatus: c.LastStatus,
		}
		if c.LastRunAt != nil {
			d.LastRunAt = c.LastRunAt.Local().Format("2006-01-02 15:04:05")
		}
		out = append(out, d)
	}
	return out
}
