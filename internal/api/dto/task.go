package dto

import (
	"strconv"
	"time"

	"paperly/internal/domain"
)

// formatTaskTime unifies the task time format (local timezone, second precision).
//
// tasks.created_at is written by SQLite CURRENT_TIMESTAMP (a UTC string); stuffing
// it straight into the response would give the frontend a bare string with no
// timezone label, which the browser would re-parse in local time and skew.
func formatTaskTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04:05")
}

// TaskSummary is a GET /api/tasks list item (Summary view).
//
// Business identifier = kind (tasks has no name/title column).
// LastError / Detail are returned only in single queries; carrying them in the list would bloat the
// response with stack strings for "40 failed tasks".
type TaskSummary struct {
	ID         int64  `json:"id"`
	Kind       int    `json:"kind"`
	Status     int    `json:"status"`
	Priority   int    `json:"priority"`
	RetryCount int    `json:"retry_count"`
	CreatedAt  string `json:"created_at"`
}

// TaskDetail is the Detail view of a single task: all Summary fields + errors and payload.
type TaskDetail struct {
	TaskSummary
	LastError string `json:"last_error"`
	// Detail is the raw JSON string of the payload-only associated target (e.g. {"kid_id":1}).
	//
	// Passed through raw rather than parsed into a struct: fields differ entirely by kind,
	// and building one big "all-nullable" struct in the DTO would leave half its fields
	// null under every kind.
	Detail     string `json:"detail"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

// TaskListResponse is the response body for GET /api/tasks.
//
// Keyset cursor pagination (ADR 0003): no total returned.
// Counts feeds the status-filter labels, saving the frontend from a count request per tab.
type TaskListResponse struct {
	Items      []TaskSummary  `json:"items"`
	NextCursor int64          `json:"next_cursor"`
	HasMore    bool           `json:"has_more"`
	Counts     map[string]int `json:"counts"`
}

// TaskCounts is the count per status (keys are decimal strings of the status enum,
// convenient to use directly as object keys).
type TaskCounts map[string]int

// TaskRetryResponse is the response body for POST /api/tasks/:id/retry.
type TaskRetryResponse struct {
	TaskDetail
}

// TaskClearFinishedResponse is the response body for POST /api/tasks/clear-finished.
type TaskClearFinishedResponse struct {
	Deleted int64 `json:"deleted"`
}

func NewTaskSummaries(rows []*domain.Task) []TaskSummary {
	items := make([]TaskSummary, 0, len(rows))
	for _, t := range rows {
		items = append(items, NewTaskSummary(t))
	}
	return items
}

func NewTaskSummary(t *domain.Task) TaskSummary {
	return TaskSummary{
		ID:         t.ID,
		Kind:       t.Kind,
		Status:     t.Status,
		Priority:   t.Priority,
		RetryCount: t.RetryCount,
		CreatedAt:  formatTaskTime(t.CreatedAt),
	}
}

func NewTaskDetail(t *domain.Task) TaskDetail {
	d := TaskDetail{
		TaskSummary: NewTaskSummary(t),
		Detail:      t.Detail,
	}
	if t.LastError != nil {
		d.LastError = *t.LastError
	}
	if t.StartedAt != nil {
		d.StartedAt = formatTaskTime(*t.StartedAt)
	}
	if t.FinishedAt != nil {
		d.FinishedAt = formatTaskTime(*t.FinishedAt)
	}
	return d
}

// NewTaskCounts converts a status-enum map into a string-keyed map.
func NewTaskCounts(counts map[int]int) map[string]int {
	out := make(map[string]int, len(counts))
	for status, n := range counts {
		out[strconv.Itoa(status)] = n
	}
	return out
}

func NewTaskListResponse(items []*domain.Task, nextCursor int64, hasMore bool, counts map[int]int) TaskListResponse {
	return TaskListResponse{
		Items:      NewTaskSummaries(items),
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Counts:     NewTaskCounts(counts),
	}
}
