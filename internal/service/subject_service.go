package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"paperly/internal/domain"
)

// SubjectService holds the business logic for the subject dictionary.
type SubjectService struct {
	repo domain.SubjectRepository
	// kidRepo validates a kid's existence; kidSubRepo reads the per-kid kid_subjects.
	// Both are optional: the dictionary-only endpoints (settings page) construct the
	// service without them, and kid-scoped methods error clearly if absent.
	kidRepo    domain.KidRepository
	kidSubRepo domain.KidSubjectRepository
}

// NewSubjectService constructs the subject service without kid dependencies
// (dictionary management endpoints only).
func NewSubjectService(repo domain.SubjectRepository) *SubjectService {
	return &SubjectService{repo: repo}
}

// NewSubjectServiceWithKids constructs the subject service with the kid + whitelist
// repositories needed by the kid-scoped listing (upload page / radar chart / kid editor).
func NewSubjectServiceWithKids(repo domain.SubjectRepository, kidRepo domain.KidRepository, kidSubRepo domain.KidSubjectRepository) *SubjectService {
	return &SubjectService{repo: repo, kidRepo: kidRepo, kidSubRepo: kidSubRepo}
}

// Get returns a single subject, or domain.ErrNotFound if absent.
func (s *SubjectService) Get(ctx context.Context, id int64) (*domain.Subject, error) {
	sub, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get subject %d: %w", id, err)
	}
	return sub, nil
}

// List returns all subjects, in sort_order.
func (s *SubjectService) List(ctx context.Context) ([]*domain.Subject, error) {
	return s.repo.List(ctx)
}

// ListByGrade returns the subjects visible to the given grade. grade <= 0 (or an
// out-of-range grade) returns the full dictionary, matching the "no filter" contract
// of GET /api/subjects.
func (s *SubjectService) ListByGrade(ctx context.Context, grade int) ([]*domain.Subject, error) {
	bit := domain.StageBitForGrade(grade)
	if bit == 0 {
		return s.repo.List(ctx)
	}
	return s.repo.ListByStageBit(ctx, bit)
}

// ListByGradeTrack returns subjects filtered by both grade stage and kid track.
//
//   - grade <= 0 (or out of range) → no stage filter.
//   - track 0/unknown → no track filter (an un-split kid sees every track).
//   - track 1 → subjects carrying the arts bit; track 2 → carrying the science bit.
//
// The arts/science split only applies from senior high (grade >= 10): a grade below
// 10 forces the track filter off, so a track set early (or by mistake) never hides
// subjects for a junior-high kid.
//
// When neither filter applies it returns the full dictionary (matches the plain
// GET /api/subjects contract).
func (s *SubjectService) ListByGradeTrack(ctx context.Context, grade, track int) ([]*domain.Subject, error) {
	stageBit := domain.StageBitForGrade(grade)
	trackBit := domain.TrackBitFor(track)
	if grade > 0 && grade < domain.TrackMinGrade {
		trackBit = 0
	}
	if stageBit == 0 && trackBit == 0 {
		return s.repo.List(ctx)
	}
	return s.repo.ListFiltered(ctx, stageBit, trackBit)
}

// ListForKid returns the subjects assigned to a specific kid via kid_subjects.
//
// No mask fallback: a kid with no kid_subjects rows yields an empty list ("no
// records").
//
// Returns domain.ErrNotFound when the kid does not exist, so the handler can 404.
func (s *SubjectService) ListForKid(ctx context.Context, kidID int64) ([]*domain.Subject, error) {
	if s.kidRepo == nil || s.kidSubRepo == nil {
		return nil, fmt.Errorf("subject service missing kid dependencies")
	}
	if _, err := s.kidRepo.GetByID(ctx, kidID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get kid %d for subject filter: %w", kidID, err)
	}
	ids, err := s.kidSubRepo.ListSubjectIDs(ctx, kidID)
	if err != nil {
		return nil, fmt.Errorf("list kid %d subjects: %w", kidID, err)
	}
	if len(ids) == 0 {
		return []*domain.Subject{}, nil
	}
	return s.repo.ListByIDs(ctx, ids)
}

// Create adds a parent-created subject and returns the persisted entity.
//
// Returns the entity rather than just the id: name is trimmed here (leading/trailing
// whitespace removed), and returning only the id would make the handler echo the
// untrimmed request text, showing the parent a name with a trailing space.
func (s *SubjectService) Create(ctx context.Context, name string) (*domain.Subject, error) {
	trimmed, err := validateSubjectName(name)
	if err != nil {
		return nil, err
	}

	code, err := deriveSubjectCode(trimmed)
	if err != nil {
		return nil, err
	}

	// Duplicate names are rejected in the service layer: code and name could both
	// collide in the DB, and showing parents a raw "UNIQUE constraint failed" is useless.
	existing, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subjects before insert: %w", err)
	}
	for _, sub := range existing {
		if sub.Name == trimmed {
			return nil, fmt.Errorf("%w: subject %q already exists", domain.ErrInvalidInput, trimmed)
		}
	}

	// sort_order is the current max +1 so new subjects land at the end instead of
	// disrupting the order parents are used to (the frontend lists "my subjects" in order).
	order := 0
	for _, sub := range existing {
		if sub.SortOrder > order {
			order = sub.SortOrder
		}
	}

	created := &domain.Subject{
		Code: code, Name: trimmed, SortOrder: order + 1, GradeMask: domain.GradeMaskAll,
	}
	id, err := s.repo.Insert(ctx, created)
	if err != nil {
		return nil, fmt.Errorf("create subject: %w", err)
	}
	created.ID = id
	created.Source = domain.SubjectSourceUser
	return created, nil
}

// Update renames a subject.
func (s *SubjectService) Update(ctx context.Context, id int64, name string) error {
	sub, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	trimmed, err := validateSubjectName(name)
	if err != nil {
		return err
	}
	if sub.Name == trimmed {
		// Renaming to the same name is a no-op and still returns success: the frontend
		// just needs a 200 after saving, and an error would look like the rename failed.
		return nil
	}
	if _, err := s.repo.UpdateName(ctx, id, trimmed); err != nil {
		return fmt.Errorf("update subject: %w", err)
	}
	return nil
}

// UpdateMask sets a subject's visibility mask. mask must be within 0~31 (low 3 stage
// bits + high 2 track bits); 0 hides the subject from every stage/track.
func (s *SubjectService) UpdateMask(ctx context.Context, id int64, mask int) error {
	if mask < 0 || mask > domain.MaskMax {
		return fmt.Errorf("%w: mask must be between 0 and %d, got %d", domain.ErrInvalidInput, domain.MaskMax, mask)
	}
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}
	if _, err := s.repo.UpdateMask(ctx, id, mask); err != nil {
		return fmt.Errorf("update subject mask: %w", err)
	}
	return nil
}

// Delete removes a subject.
//
// System-preset subjects (source=1) cannot be deleted: they are the built-in dictionary,
// and deleting one would leave existing exams' subject_id dangling; the seed would
// re-import them on restart (its cursor flag only blocks once), so parents would see it
// "deleted then come back". Blocked in the service layer, not just by hiding the UI button,
// since calling the API directly would delete it too.
//
// User-created subjects are also blocked while still referenced: exams.subject_id is
// ON DELETE SET NULL (silently detaching existing exams from their subject) and
// knowledge_points.subject_id is ON DELETE CASCADE (silently destroying the knowledge-point
// tree). Both surface as ErrInvalidState with a count, so the parent knows what to handle first.
func (s *SubjectService) Delete(ctx context.Context, id int64) error {
	sub, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if sub.Source == domain.SubjectSourceSystem {
		return fmt.Errorf("%w: system-preset subject cannot be deleted (may be renamed)", domain.ErrInvalidInput)
	}

	exams, err := s.repo.CountExams(ctx, id)
	if err != nil {
		return fmt.Errorf("count exams before subject delete: %w", err)
	}
	if exams > 0 {
		return fmt.Errorf("%w: subject %q is still used by %d exam(s); delete or re-assign those exams first", domain.ErrInvalidState, sub.Name, exams)
	}
	kps, err := s.repo.CountKnowledgePoints(ctx, id)
	if err != nil {
		return fmt.Errorf("count knowledge points before subject delete: %w", err)
	}
	if kps > 0 {
		return fmt.Errorf("%w: subject %q is still used by %d knowledge point(s); delete or re-assign them first", domain.ErrInvalidState, sub.Name, kps)
	}

	if _, err := s.repo.Delete(ctx, id); err != nil {
		// kid_subjects.subject_id is ON DELETE RESTRICT: deleting a subject a kid
		// still whitelists raises a raw FK error. Surface it as invalid input
		// ("used by a kid") rather than a 500 — the parent can fix it by removing the
		// subject from the kid first.
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			return fmt.Errorf("%w: subject is in use by a student and cannot be deleted", domain.ErrInvalidInput)
		}
		return fmt.Errorf("delete subject: %w", err)
	}
	return nil
}

// validateSubjectName trims and validates a subject name.
func validateSubjectName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("%w: subject name cannot be empty", domain.ErrInvalidInput)
	}
	if n := len([]rune(trimmed)); n > domain.SubjectMaxNameLen {
		return "", fmt.Errorf("%w: subject name too long (max %d, got %d)", domain.ErrInvalidInput, domain.SubjectMaxNameLen, n)
	}
	if strings.IndexFunc(trimmed, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) == 0 {
		return "", fmt.Errorf("%w: subject name must contain letters or digits", domain.ErrInvalidInput)
	}
	return trimmed, nil
}

// deriveSubjectCode derives a stable ASCII code from the name (Chinese names produce a hash).
//
// subjects.code has a UNIQUE constraint but no business meaning (the UI only shows
// name), so user-created subjects generate their own. Strategy: ASCII letters are
// lowercased directly; a name containing Chinese falls back to "u" + a stable short
// hash. A random suffix is not used — it would change the code on every rebuild and
// make the association with existing exams/knowledge-points unreadable.
func deriveSubjectCode(name string) (string, error) {
	var ascii strings.Builder
	for _, r := range name {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			ascii.WriteRune(unicode.ToLower(r))
		}
	}
	if ascii.Len() > 0 && ascii.Len() <= 32 {
		return ascii.String(), nil
	}
	// FNV-1a 32-bit: short and stable.
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	h := uint32(offset32)
	for _, r := range name {
		h ^= uint32(r)
		h *= prime32
	}
	return fmt.Sprintf("u%08x", h), nil
}
