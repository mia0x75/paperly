package service

import (
	"context"
	"errors"
	"fmt"

	"paperly/internal/domain"
)

// TaskPage is one page of the task list (keyset cursor).
type TaskPage struct {
	Items      []*domain.Task
	NextCursor int64
	HasMore    bool
	// Counts holds the per-status counts, used by the list page's status filter labels, saving one request.
	Counts map[int]int
}

// TaskService holds the ops logic for background tasks (tasks table).
//
// Note: this service only does ops inspection and retry; it does not execute tasks.
// Execution is the Worker's job (internal/worker); retry simply flips the status back
// to pending for the Worker to pick up — it does not run tasks synchronously here.
type TaskService struct {
	repo domain.TaskRepository
}

// NewTaskService constructs the task service.
func NewTaskService(repo domain.TaskRepository) *TaskService {
	return &TaskService{repo: repo}
}

// List returns one page of the task list, clamping limit to [1, domain.TaskListLimit].
func (s *TaskService) List(ctx context.Context, status int, hasStatus bool, cursor int64, limit int) (*TaskPage, error) {
	if hasStatus {
		if err := validateTaskStatus(status); err != nil {
			return nil, err
		}
	}
	if cursor < 0 {
		return nil, fmt.Errorf("%w: cursor cannot be negative", domain.ErrInvalidInput)
	}
	if limit <= 0 {
		limit = domain.TaskListLimit
	}
	if limit > domain.TaskListLimit {
		limit = domain.TaskListLimit
	}

	// Fetch one extra row to detect has_more: next_cursor=0 ⟺ has_more=false.
	rows, err := s.repo.List(ctx, domain.ListTasksFilter{
		Status: status, HasStatus: hasStatus, Cursor: cursor, Limit: limit + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	page := &TaskPage{Items: rows, NextCursor: 0, HasMore: false}
	if len(rows) > limit {
		page.HasMore = true
		page.Items = rows[:limit]
	}
	if len(page.Items) > 0 {
		page.NextCursor = page.Items[len(page.Items)-1].ID
	}
	if !page.HasMore {
		page.NextCursor = 0
	}

	counts, err := s.repo.CountByStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("count tasks by status: %w", err)
	}
	page.Counts = counts
	return page, nil
}

// Retry retries a failed task and returns the updated task.
//
// Only failed→pending is allowed: pending/running/done/skipped are all rejected,
// otherwise "retry" could re-run a completed task (re-running a backup task = one more snapshot).
func (s *TaskService) Retry(ctx context.Context, id int64) (*domain.Task, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.TaskStatusFailed {
		return nil, fmt.Errorf("%w: only failed tasks can be retried (current status %d)", domain.ErrInvalidInput, t.Status)
	}
	if t.RetryCount >= domain.TaskMaxRetryCount {
		return nil, fmt.Errorf("%w: retry count reached the limit of %d", domain.ErrInvalidInput, domain.TaskMaxRetryCount)
	}
	if _, err := s.repo.Retry(ctx, id); err != nil {
		return nil, fmt.Errorf("retry task: %w", err)
	}
	return s.repo.GetByID(ctx, id)
}

// ClearFinished removes done/skipped tasks and returns the number deleted.
func (s *TaskService) ClearFinished(ctx context.Context) (int64, error) {
	n, err := s.repo.ClearFinished(ctx)
	if err != nil {
		return 0, fmt.Errorf("clear finished tasks: %w", err)
	}
	return n, nil
}

// validateTaskStatus validates the task status enum.
func validateTaskStatus(status int) error {
	switch status {
	case domain.TaskStatusPending, domain.TaskStatusRunning, domain.TaskStatusDone,
		domain.TaskStatusFailed, domain.TaskStatusSkipped, domain.TaskStatusCancelled:
		return nil
	default:
		return fmt.Errorf("%w: unknown task status %d", domain.ErrInvalidInput, status)
	}
}

// GetTask returns a single task for detail/log viewing.
func (s *TaskService) GetTask(ctx context.Context, id int64) (*domain.Task, error) {
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get task %d: %w", id, err)
	}
	return t, nil
}
