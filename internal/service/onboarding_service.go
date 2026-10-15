package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"paperly/internal/domain"
)

// OnboardingService handles first-startup (/welcome) status determination and the completion marker.
//
// It shares the cursors table with the existing seed-import mechanism for "one-time markers": seed uses idempotent dedup
// to guard against re-import, while onboarding uses the CursorTaskOnboardingDone cursor to guard against "has this startup
// already run onboarding". The two have no shared prerequisite; see Status for the combined rules.
type OnboardingService struct {
	userRepo   domain.UserRepository
	kidRepo    domain.KidRepository
	cursorRepo domain.MaintenanceCursorRepository
}

// NewOnboardingService constructs the onboarding service.
func NewOnboardingService(
	userRepo domain.UserRepository,
	kidRepo domain.KidRepository,
	cursorRepo domain.MaintenanceCursorRepository,
) *OnboardingService {
	return &OnboardingService{userRepo: userRepo, kidRepo: kidRepo, cursorRepo: cursorRepo}
}

// Status reports whether onboarding is complete.
//
// Rules (combined; any missing counts as incomplete):
//   - cursors has task_name='onboarding_done' with last_status=Completed
//   - the users table has at least 1 parent account
//   - the kids table has at least 1 kid
//
// Points three and four matter because after a user manually clears the data the marker remains but the content is gone;
// the user must be pulled back to onboarding to rebuild, rather than running an empty shell.
func (s *OnboardingService) Status(ctx context.Context) (bool, error) {
	done, err := s.cursorDone(ctx)
	if err != nil {
		return false, err
	}
	if !done {
		return false, nil
	}

	users, err := s.userRepo.List(ctx)
	if err != nil {
		return false, fmt.Errorf("list users for onboarding check: %w", err)
	}
	if len(users) == 0 {
		return false, nil
	}

	kids, err := s.kidRepo.List(ctx, domain.ListKidsFilter{})
	if err != nil {
		return false, fmt.Errorf("list kids for onboarding check: %w", err)
	}
	return len(kids) > 0, nil
}

// cursorDone checks only the cursor side: exists and is Completed.
func (s *OnboardingService) cursorDone(ctx context.Context) (bool, error) {
	c, err := s.cursorRepo.Get(ctx, domain.CursorTaskOnboardingDone)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("get onboarding cursor: %w", err)
	}
	return c.LastStatus == domain.CursorStatusCompleted, nil
}

// Complete marks onboarding done, writing the one-time cursor and returning the final Status.
//
// Prerequisite: users≥1 and kids≥1. Otherwise it returns ErrInvalidState — the completion marker is
// a terminal state, triggered only by a real two-entity onboarding, never checked off against an empty DB.
//
// Security gate (prevents anonymous replaying to issue a parent JWT):
//   - cursor marker missing → allow (first onboarding);
//   - marker exists but users or kids empty → allow (reset scenario: data cleared, state incomplete,
//     so onboarding still needs to run to completion after rebuilding);
//   - marker exists and both users/kids non-empty → reject, returning domain.ErrOnboardingCompleted
//     (the api layer maps to 409 — onboarding is already a complete terminal state, so doing it again is a replay).
func (s *OnboardingService) Complete(ctx context.Context) (bool, error) {
	done, err := s.cursorDone(ctx)
	if err != nil {
		return false, err
	}
	users, err := s.userRepo.List(ctx)
	if err != nil {
		return false, fmt.Errorf("list users before completing onboarding: %w", err)
	}
	kids, err := s.kidRepo.List(ctx, domain.ListKidsFilter{})
	if err != nil {
		return false, fmt.Errorf("list kids before completing onboarding: %w", err)
	}
	if done && len(users) > 0 && len(kids) > 0 {
		return false, fmt.Errorf("%w: onboarding already completed", domain.ErrOnboardingCompleted)
	}
	if len(users) == 0 {
		return false, fmt.Errorf("%w: at least one parent account is required to complete onboarding", domain.ErrInvalidState)
	}
	if len(kids) == 0 {
		return false, fmt.Errorf("%w: at least one kid is required to complete onboarding", domain.ErrInvalidState)
	}

	now := time.Now()
	if err := s.cursorRepo.Upsert(ctx, &domain.MaintenanceCursor{
		TaskName:   domain.CursorTaskOnboardingDone,
		LastID:     0,
		LastRunAt:  &now,
		LastStatus: domain.CursorStatusCompleted,
	}); err != nil {
		return false, fmt.Errorf("write onboarding cursor: %w", err)
	}
	return true, nil
}
