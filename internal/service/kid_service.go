package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"paperly/internal/domain"
)

// KidService depends only on the repository interfaces defined in domain.
type KidService struct {
	repo domain.KidRepository
	// txm + subjectRepo + kidSubRepo serve the atomic kid+subjects write path
	// (CreateWithSubjects / UpdateWithSubjects). They stay nil for plain profile use.
	txm         domain.TxManager
	subjectRepo domain.SubjectRepository
	kidSubRepo  domain.KidSubjectRepository
}

// NewKidService constructs the kid service. The txm/subjectRepo/kidSubRepo
// triple wires the atomic "profile + subjects" write path; pass nils for
// plain profile use (Create/Update/Delete never touch subjects).
func NewKidService(repo domain.KidRepository, txm domain.TxManager, subjectRepo domain.SubjectRepository, kidSubRepo domain.KidSubjectRepository) *KidService {
	return &KidService{repo: repo, txm: txm, subjectRepo: subjectRepo, kidSubRepo: kidSubRepo}
}

type CreateKidReq struct {
	Name   string
	Grade  int
	Gender *int    // optional: 1=male / 0=female / nil=unset
	Pin    *string // optional, 4-6 digits
}

// CreateKidResponse is the create response (exposed after the api layer converts to DTO).
type CreateKidResponse struct {
	ID         int64
	Name       string
	AccessCode string
	KidURL     string
	CreatedAt  time.Time
}

type UpdateKidReq struct {
	Name   string
	Grade  int
	Gender *int // optional: 1=male / 0=female / nil=unset
	Track  int  // 0 undecided / 1 arts / 2 science (see domain.KidTrack*)
}

// Create creates a student: validate input → generate a unique access_code → hash
// the PIN (placeholder if unset) → persist → return the invite URL.
func (s *KidService) Create(ctx context.Context, req CreateKidReq) (*CreateKidResponse, error) {
	st, err := s.buildNewKid(ctx, req)
	if err != nil {
		return nil, err
	}

	id, err := s.repo.Insert(ctx, st)
	if err != nil {
		// Pre-checks catch most duplicate names; the unique index backstops concurrent
		// or bypassing writes: a hit maps to ErrDuplicateName instead of a 500.
		if isUniqueNameErr(err) {
			return nil, fmt.Errorf("%w: name already exists", domain.ErrDuplicateName)
		}
		return nil, fmt.Errorf("create kid: %w", err)
	}

	return s.buildCreateResponse(ctx, id)
}

type CreateKidWithSubjectsReq struct {
	CreateKidReq
	SubjectIDs []int64
}

// CreateWithSubjects creates a student and replaces its kid_subjects whitelist in one
// transaction: either both the profile row and its subjects are written, or neither is.
// Input is validated before the transaction opens (no partial writes on bad input).
func (s *KidService) CreateWithSubjects(ctx context.Context, req CreateKidWithSubjectsReq) (*CreateKidResponse, error) {
	if s.txm == nil || s.kidSubRepo == nil || s.subjectRepo == nil {
		return nil, fmt.Errorf("create kid with subjects: subject write path not configured")
	}

	st, err := s.buildNewKid(ctx, req.CreateKidReq)
	if err != nil {
		return nil, err
	}
	subjectIDs, err := validateKidSubjects(ctx, s.subjectRepo, req.SubjectIDs)
	if err != nil {
		return nil, err
	}

	var id int64
	err = s.txm.WithinTx(ctx, func(tx domain.Tx) error {
		kidRepo := s.repo.WithTx(tx)
		newID, err := kidRepo.Insert(ctx, st)
		if err != nil {
			return err
		}
		id = newID
		return s.kidSubRepo.WithTx(tx).ReplaceAll(ctx, id, subjectIDs)
	})
	if err != nil {
		if isUniqueNameErr(err) {
			return nil, fmt.Errorf("%w: name already exists", domain.ErrDuplicateName)
		}
		return nil, fmt.Errorf("create kid with subjects: %w", err)
	}

	return s.buildCreateResponse(ctx, id)
}

// buildNewKid validates a create request and assembles the domain.Kid,
// including access_code and pin_hash.
func (s *KidService) buildNewKid(ctx context.Context, req CreateKidReq) (*domain.Kid, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name cannot be empty", domain.ErrInvalidInput)
	}
	if req.Grade < domain.GradeMin || req.Grade > domain.GradeMax {
		return nil, fmt.Errorf("%w: grade must be %d-%d", domain.ErrInvalidInput, domain.GradeMin, domain.GradeMax)
	}
	if err := validateGender(req.Gender); err != nil {
		return nil, err
	}
	if err := s.ensureNameAvailable(ctx, name, 0); err != nil {
		return nil, err
	}

	pin := ""
	if req.Pin != nil {
		pin = *req.Pin
	}
	if err := validatePin(pin); err != nil {
		return nil, err
	}

	code, err := s.uniqueAccessCode(ctx)
	if err != nil {
		return nil, err
	}

	pinHash, err := hashPin(pin)
	if err != nil {
		return nil, err
	}
	if pinHash == "" {
		pinHash = PinPlaceholder
	}

	return &domain.Kid{
		Name:       name,
		Grade:      req.Grade,
		Gender:     req.Gender,
		AccessCode: code,
		PinHash:    pinHash,
		PinVersion: 1,
		Status:     domain.KidStatusActive,
	}, nil
}

// buildCreateResponse reloads the freshly created kid and assembles the response.
func (s *KidService) buildCreateResponse(ctx context.Context, id int64) (*CreateKidResponse, error) {
	created, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("reload created kid: %w", err)
	}
	return &CreateKidResponse{
		ID:         created.ID,
		Name:       created.Name,
		AccessCode: created.AccessCode,
		KidURL:     "/invite/" + created.AccessCode,
		CreatedAt:  created.CreatedAt,
	}, nil
}

func (s *KidService) Get(ctx context.Context, id int64) (*domain.Kid, error) {
	st, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (s *KidService) List(ctx context.Context, filter domain.ListKidsFilter) ([]*domain.Kid, error) {
	return s.repo.List(ctx, filter)
}

func (s *KidService) Update(ctx context.Context, id int64, req UpdateKidReq) (*domain.Kid, error) {
	st, err := s.buildUpdatedKid(ctx, id, req)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, st); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		if isUniqueNameErr(err) {
			return nil, fmt.Errorf("%w: name already exists", domain.ErrDuplicateName)
		}
		return nil, fmt.Errorf("update kid: %w", err)
	}
	return s.Get(ctx, id)
}

type UpdateKidWithSubjectsReq struct {
	UpdateKidReq
	SubjectIDs []int64
}

// UpdateWithSubjects updates the student profile and replaces its kid_subjects whitelist
// in one transaction: either both changes land, or neither does. Validation happens before
// the transaction opens.
func (s *KidService) UpdateWithSubjects(ctx context.Context, id int64, req UpdateKidWithSubjectsReq) (*domain.Kid, error) {
	if s.txm == nil || s.kidSubRepo == nil || s.subjectRepo == nil {
		return nil, fmt.Errorf("update kid with subjects: subject write path not configured")
	}

	st, err := s.buildUpdatedKid(ctx, id, req.UpdateKidReq)
	if err != nil {
		return nil, err
	}
	subjectIDs, err := validateKidSubjects(ctx, s.subjectRepo, req.SubjectIDs)
	if err != nil {
		return nil, err
	}

	err = s.txm.WithinTx(ctx, func(tx domain.Tx) error {
		if err := s.repo.WithTx(tx).Update(ctx, st); err != nil {
			return err
		}
		return s.kidSubRepo.WithTx(tx).ReplaceAll(ctx, id, subjectIDs)
	})
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		if isUniqueNameErr(err) {
			return nil, fmt.Errorf("%w: name already exists", domain.ErrDuplicateName)
		}
		return nil, fmt.Errorf("update kid with subjects: %w", err)
	}
	return s.Get(ctx, id)
}

// buildUpdatedKid validates an update request and assembles the patched domain.Kid.
func (s *KidService) buildUpdatedKid(ctx context.Context, id int64, req UpdateKidReq) (*domain.Kid, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name cannot be empty", domain.ErrInvalidInput)
	}
	if req.Grade < domain.GradeMin || req.Grade > domain.GradeMax {
		return nil, fmt.Errorf("%w: grade must be %d-%d", domain.ErrInvalidInput, domain.GradeMin, domain.GradeMax)
	}
	if err := validateGender(req.Gender); err != nil {
		return nil, err
	}
	if err := validateTrack(req.Track); err != nil {
		return nil, err
	}
	// Exclude self: renaming to the same name is a valid no-op.
	if err := s.ensureNameAvailable(ctx, name, id); err != nil {
		return nil, err
	}
	return &domain.Kid{
		ID:     id,
		Name:   name,
		Grade:  req.Grade,
		Gender: req.Gender,
		Track:  req.Track,
	}, nil
}

// validateGender validates the gender value: nil=unset, otherwise must be 1 (male) or 0 (female).
func validateGender(g *int) error {
	if g == nil {
		return nil
	}
	if *g != domain.KidGenderMale && *g != domain.KidGenderFemale {
		return fmt.Errorf("%w: gender must be 1 (male) / 0 (female) / empty", domain.ErrInvalidInput)
	}
	return nil
}

// validateTrack validates kids.track: only 0 (undecided) / 1 (arts) / 2 (science) are allowed.
func validateTrack(track int) error {
	switch track {
	case domain.KidTrackUndecided, domain.KidTrackArts, domain.KidTrackScience:
		return nil
	default:
		return fmt.Errorf("%w: track must be 0 (undecided) / 1 (arts) / 2 (science)", domain.ErrInvalidInput)
	}
}

// ensureNameAvailable checks that the name is not used by another student (unique
// across the table, including deleted/graduated).
func (s *KidService) ensureNameAvailable(ctx context.Context, name string, excludeID int64) error {
	exists, err := s.repo.ExistsByName(ctx, name, excludeID)
	if err != nil {
		return fmt.Errorf("check kid name: %w", err)
	}
	if exists {
		return fmt.Errorf("%w: name already exists", domain.ErrDuplicateName)
	}
	return nil
}

// isUniqueNameErr reports whether the error comes from a kids.name unique-index
// conflict. SQLite's constraint error text always contains
// "UNIQUE constraint failed: kids.name"; the modernc driver exposes no assertable
// type, so this is text matching — a last line of defense.
func isUniqueNameErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: kids.name")
}

// Delete soft-deletes a student (status → deleted).
//
// Graduated kids cannot be deleted: graduation is terminal and an accidental delete
// could not be undone through the normal UI. Deleted kids are treated as absent, and
// deleting again returns ErrNotFound.
func (s *KidService) Delete(ctx context.Context, id int64) error {
	st, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	switch st.Status {
	case domain.KidStatusGraduated:
		return fmt.Errorf("%w: graduated kid cannot be deleted", domain.ErrInvalidState)
	case domain.KidStatusDeleted:
		return domain.ErrNotFound
	}
	if err := s.repo.SoftDelete(ctx, id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("delete kid: %w", err)
	}
	return nil
}

// Restore brings a soft-deleted student back (status: deleted → active).
//
// Graduated and non-deleted kids cannot be restored: restore targets only soft-deleted
// profiles. The name uniqueness constraint is table-wide, so restore cannot clash with
// other kids (the name stays occupied even while deleted).
func (s *KidService) Restore(ctx context.Context, id int64) (*domain.Kid, error) {
	st, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	switch st.Status {
	case domain.KidStatusDeleted:
		// Allowed.
	case domain.KidStatusGraduated:
		return nil, fmt.Errorf("%w: graduated kid cannot be restored", domain.ErrInvalidState)
	default:
		return nil, fmt.Errorf("%w: kid is not deleted, nothing to restore", domain.ErrInvalidState)
	}
	if err := s.repo.Restore(ctx, id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("restore kid: %w", err)
	}
	return s.Get(ctx, id)
}

// ResetAccessCode generates a new access_code and overwrites the old one (parent reset).
// The previous code is invalidated immediately (its URL stops working). Soft-deleted kids
// cannot be reset, consistent with list visibility; graduated kids may be reset since
// they may still need to retain access.
func (s *KidService) ResetAccessCode(ctx context.Context, id int64) (*domain.Kid, error) {
	st, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if st.Status == domain.KidStatusDeleted {
		return nil, domain.ErrNotFound
	}

	code, err := s.uniqueAccessCode(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateAccessCode(ctx, id, code); err != nil {
		return nil, fmt.Errorf("reset access_code: %w", err)
	}
	return s.Get(ctx, id)
}

// ResetPin sets or clears a student's PIN.
//
// An empty newPin clears it (writes a placeholder; kids without a PIN are set via the
// /invite flow); a non-empty value is validated as 4-6 digits and bcrypt-hashed slowly.
// Soft-deleted kids cannot be reset.
func (s *KidService) ResetPin(ctx context.Context, id int64, newPin string) (*domain.Kid, error) {
	st, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if st.Status == domain.KidStatusDeleted {
		return nil, domain.ErrNotFound
	}

	newPin = strings.TrimSpace(newPin)
	if err := validatePin(newPin); err != nil {
		return nil, err
	}
	hash := PinPlaceholder
	if newPin != "" {
		hash, err = hashPin(newPin)
		if err != nil {
			return nil, err
		}
	}
	if err := s.repo.UpdatePinHash(ctx, id, hash); err != nil {
		return nil, fmt.Errorf("reset pin: %w", err)
	}
	return s.Get(ctx, id)
}

// uniqueAccessCode generates an access_code not present in the DB, retrying up to
// maxAccessCodeRetries times.
func (s *KidService) uniqueAccessCode(ctx context.Context) (string, error) {
	for i := 0; i < maxAccessCodeRetries; i++ {
		code, err := generateAccessCode()
		if err != nil {
			return "", err
		}
		exists, err := s.repo.ExistsByAccessCode(ctx, code)
		if err != nil {
			return "", fmt.Errorf("check access_code: %w", err)
		}
		if !exists {
			return code, nil
		}
	}
	return "", domain.ErrAccessCodeCollision
}
