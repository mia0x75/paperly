package worker

import (
	"context"

	"paperly/internal/domain"
)

// aggregate_gaps placeholder: gaps aggregation needs the levels/gaps/questions repositories and
// algorithm layers that do not exist yet. Skipping on consumption keeps the task out of pending;
// once the domain lands, drop this and swap the real handler into dispatch.
func handleAggregateGapsUnavailable(_ context.Context, _ Deps, _ *domain.Task) error {
	return skip(skipAggregateGapsReason)
}
