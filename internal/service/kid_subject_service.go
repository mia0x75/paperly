package service

import (
	"context"
	"errors"
	"fmt"

	"paperly/internal/domain"
)

// KidSubjectService manages the per-kid subject whitelist (kid_subjects) and expands
// it into dictionary rows for the kid subject editor / upload page.
type KidSubjectService struct {
	kidRepo     domain.KidRepository
	subjectRepo domain.SubjectRepository
	kidSubRepo  domain.KidSubjectRepository
}

// NewKidSubjectService constructs the kid×subject whitelist service.
func NewKidSubjectService(kidRepo domain.KidRepository, subjectRepo domain.SubjectRepository, kidSubRepo domain.KidSubjectRepository) *KidSubjectService {
	return &KidSubjectService{kidRepo: kidRepo, subjectRepo: subjectRepo, kidSubRepo: kidSubRepo}
}

// List returns the subjects currently whitelisted for the kid, ordered by sort_order
// (consistent with the dictionary). Returns ErrNotFound when the kid is missing.
func (s *KidSubjectService) List(ctx context.Context, kidID int64) ([]*domain.Subject, error) {
	if err := s.ensureKid(ctx, kidID); err != nil {
		return nil, err
	}
	ids, err := s.kidSubRepo.ListSubjectIDs(ctx, kidID)
	if err != nil {
		return nil, fmt.Errorf("list kid %d subjects: %w", kidID, err)
	}
	if len(ids) == 0 {
		// No kid_subjects rows: the kid has no subjects ("no records"). There is no
		// mask fallback anywhere, so an empty list is the truthful answer.
		return []*domain.Subject{}, nil
	}
	return s.subjectRepo.ListByIDs(ctx, ids)
}

// Set replaces the kid's whitelist with subjectIDs.
//
// Validation:
//   - the kid must exist (ErrNotFound);
//   - every id must be a real subject (ErrInvalidInput otherwise);
//   - every mandatory subject (mandatory=1, chinese/math) is force-included by the
//     server, so the parent can never drop a core subject.
//
// An empty subjectIDs is allowed: it is merged up to exactly the mandatory set.
func (s *KidSubjectService) Set(ctx context.Context, kidID int64, subjectIDs []int64) error {
	if err := s.ensureKid(ctx, kidID); err != nil {
		return err
	}
	unique, err := validateKidSubjects(ctx, s.subjectRepo, subjectIDs)
	if err != nil {
		return err
	}
	if err := s.kidSubRepo.ReplaceAll(ctx, kidID, unique); err != nil {
		return fmt.Errorf("replace kid %d subjects: %w", kidID, err)
	}
	return nil
}

// normalizeSubjectIDs deduplicates while preserving input order (stable error messages).
func normalizeSubjectIDs(subjectIDs []int64) []int64 {
	seen := make(map[int64]bool, len(subjectIDs))
	unique := make([]int64, 0, len(subjectIDs))
	for _, id := range subjectIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	return unique
}

// validateKidSubjects dedups subjectIDs, verifies every id is a real subject, and — as the
// final authority on the whitelist — force-includes every mandatory subject. Shared by
// KidSubjectService.Set and the atomic kid+subjects write path in KidService, so both
// enforce identical rules.
//
// Mandatory subjects are merged in rather than rejected: a stale client or a UI bug that
// omits chinese/math must never be able to drop a core subject, yet the kid create/update has
// to keep working. The stored whitelist therefore always contains every mandatory subject.
func validateKidSubjects(ctx context.Context, subjectRepo domain.SubjectRepository, subjectIDs []int64) ([]int64, error) {
	unique := normalizeSubjectIDs(subjectIDs)

	// Load the full dictionary once (it is small): a single query answers both "does this
	// id exist" and "which subjects are mandatory", avoiding a second round trip / N+1.
	all, err := subjectRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("validate kid subjects: %w", err)
	}
	known := make(map[int64]bool, len(all))
	for _, sub := range all {
		known[sub.ID] = true
	}

	// A dangling id is a genuine bad request (unlike a missing mandatory subject): reject it
	// rather than silently storing a reference to a non-existent subject.
	for _, id := range unique {
		if !known[id] {
			return nil, fmt.Errorf("%w: subject id %d does not exist", domain.ErrInvalidInput, id)
		}
	}

	// Force-include every mandatory subject (a dictionary property, not a per-kid one).
	seen := make(map[int64]bool, len(unique))
	for _, id := range unique {
		seen[id] = true
	}
	for _, sub := range all {
		if sub.Mandatory == 0 || seen[sub.ID] {
			continue
		}
		unique = append(unique, sub.ID)
		seen[sub.ID] = true
	}
	return unique, nil
}

// ensureKid returns ErrNotFound when the kid does not exist.
func (s *KidSubjectService) ensureKid(ctx context.Context, kidID int64) error {
	if _, err := s.kidRepo.GetByID(ctx, kidID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("get kid %d: %w", kidID, err)
	}
	return nil
}
