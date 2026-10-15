package service

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"paperly/internal/domain"
)

// KnowledgePointMaxNameLen is the maximum length of a knowledge-point name.
const KnowledgePointMaxNameLen = 64

// KnowledgePointService holds the business logic for knowledge points (knowledge_points table).
type KnowledgePointService struct {
	kp   domain.KnowledgePointRepository
	subj domain.SubjectRepository
}

// NewKnowledgePointService constructs the knowledge-point service.
//
// SubjectRepository is injected to verify the subject exists before writing a knowledge point:
// the DB has a foreign-key constraint as backstop, but its error is "FOREIGN KEY constraint failed",
// which parents cannot understand; the service intercepts it to return "subject does not exist".
func NewKnowledgePointService(kp domain.KnowledgePointRepository, subj domain.SubjectRepository) *KnowledgePointService {
	return &KnowledgePointService{kp: kp, subj: subj}
}

// List returns the knowledge points under a subject.
func (s *KnowledgePointService) List(ctx context.Context, subjectID int64) ([]*domain.KnowledgePoint, error) {
	if _, err := s.subj.GetByID(ctx, subjectID); err != nil {
		return nil, err
	}
	return s.kp.ListBySubject(ctx, subjectID)
}

// Create adds a knowledge point under the given subject and returns the persisted entity (rationale as in SubjectService.Create).
func (s *KnowledgePointService) Create(ctx context.Context, subjectID int64, name string) (*domain.KnowledgePoint, error) {
	if _, err := s.subj.GetByID(ctx, subjectID); err != nil {
		return nil, err
	}
	trimmed, err := validateKnowledgePointName(name)
	if err != nil {
		return nil, err
	}
	created := &domain.KnowledgePoint{SubjectID: subjectID, Name: trimmed}
	id, err := s.kp.Insert(ctx, created)
	if err != nil {
		return nil, fmt.Errorf("create knowledge point: %w", err)
	}
	created.ID = id
	return created, nil
}

// Update renames a knowledge point.
//
// subjectID must be passed explicitly by the caller (from the path) rather than looked up from id:
// the route is /subjects/:subject_id/knowledge_points/:id,
// without this parameter one could edit subject B's knowledge point via subject A's path,
// and a misattributed ownership parameter often goes unnoticed in self-tests.
func (s *KnowledgePointService) Update(ctx context.Context, subjectID, id int64, name string) error {
	if err := s.assertOwned(ctx, subjectID, id); err != nil {
		return err
	}
	trimmed, err := validateKnowledgePointName(name)
	if err != nil {
		return err
	}
	if _, err := s.kp.UpdateName(ctx, id, trimmed); err != nil {
		return fmt.Errorf("update knowledge point: %w", err)
	}
	return nil
}

// Delete removes a knowledge point. Ownership check same as Update: subjectID comes from the path.
func (s *KnowledgePointService) Delete(ctx context.Context, subjectID, id int64) error {
	if err := s.assertOwned(ctx, subjectID, id); err != nil {
		return err
	}
	if _, err := s.kp.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete knowledge point: %w", err)
	}
	return nil
}

// assertOwned verifies the knowledge point actually belongs to the subject given in the path.
//
// An ownership mismatch always returns ErrNotFound rather than 403: a 404 does not reveal that the id exists,
// and to a parent "this knowledge point is not under this subject" and "this knowledge point does not exist"
// are the same thing, so there is no need to distinguish them.
func (s *KnowledgePointService) assertOwned(ctx context.Context, subjectID, id int64) error {
	kp, err := s.kp.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if kp.SubjectID != subjectID {
		return domain.ErrNotFound
	}
	return nil
}

// validateKnowledgePointName trims and validates a knowledge-point name.
func validateKnowledgePointName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("%w: knowledge point name cannot be empty", domain.ErrInvalidInput)
	}
	if n := len([]rune(trimmed)); n > KnowledgePointMaxNameLen {
		return "", fmt.Errorf("%w: knowledge point name too long (max %d, got %d)", domain.ErrInvalidInput, KnowledgePointMaxNameLen, n)
	}
	// UNIQUE(subject_id, name) compares values as-is, so dirty data would collide with the unique constraint.
	// Symbol-only names (?!, ***) are rejected early; they carry no information in the knowledge-point tree.
	if strings.IndexFunc(trimmed, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) == 0 {
		return "", fmt.Errorf("%w: knowledge point name must contain letters or digits", domain.ErrInvalidInput)
	}
	return trimmed, nil
}
