package domain

import (
	"context"
	"time"
)

// SubjectSource (two states).
const (
	SubjectSourceSystem = 1 // system-built
	SubjectSourceUser   = 2 // user-defined
)

// Stage visibility bits for subjects.mask (low 3 bits). A subject shows for a grade when
// (mask & StageBitForGrade(grade)) != 0.
const (
	StagePrimary = 1 // grades 1~6
	StageJunior  = 2 // grades 7~9
	StageSenior  = 4 // grades 10~12

	// GradeMaskAll is the default stage mask (visible in all stages).
	GradeMaskAll = StagePrimary | StageJunior | StageSenior // 7
)

// Track visibility bits for subjects.mask (bits 3~4), sharing the mask column with the stage bits.
// A subject shows for a kid's track when (mask & TrackBitFor(track)) != 0.
//
// bit5 ("comprehensive") was retired: core subjects now carry both arts and science bits (mask 31),
// so they match any track without a general bit.
const (
	TrackArts    = 1 << 3 // arts
	TrackScience = 1 << 4 // science

	// TrackMaskAll is both tracks on (arts | science).
	TrackMaskAll = TrackArts | TrackScience // 24

	// MaskAll is the fully-open mask (all stages + both tracks): the default for new subjects.
	MaskAll = GradeMaskAll | TrackMaskAll // 31

	// MaskMax is the largest valid mask (bits 0~4); bit5 and above are reserved.
	MaskMax = MaskAll // 31
)

// TrackMinGrade is the lowest grade at which a kid's track takes effect: the arts/science split
// applies from senior high (grade 10); below it the track is stored but never filters subjects.
const TrackMinGrade = 10

// StageBitForGrade maps a grade (1~12) to its stage bit; out-of-range yields 0 (matches nothing).
func StageBitForGrade(grade int) int {
	switch {
	case grade >= 1 && grade <= 6:
		return StagePrimary
	case grade >= 7 && grade <= 9:
		return StageJunior
	case grade >= 10 && grade <= 12:
		return StageSenior
	default:
		return 0
	}
}

// TrackBitFor maps a kid track (0/1/2) to its track bit. 0 (undecided) or invalid -> 0, meaning
// "do not filter by track": a kid who has not split should still see every track, matching the
// stage no-filter convention. Core subjects carry both track bits, so they match either.
func TrackBitFor(track int) int {
	switch track {
	case KidTrackArts:
		return TrackArts
	case KidTrackScience:
		return TrackScience
	default:
		return 0
	}
}

// Subject corresponds to the subjects table.
type Subject struct {
	ID        int64  `db:"id"`
	Code      string `db:"code"`
	Name      string `db:"name"`
	SortOrder int    `db:"sort_order"`
	Source    int    `db:"source"`
	GradeMask int    `db:"mask"`
	// Mandatory marks a subject a kid cannot drop (chinese / math = 1); the kid-subject editor
	// force-checks them and the server force-includes them in the whitelist.
	Mandatory int       `db:"mandatory"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// SubjectRepository is the subject-dictionary data-access contract.
type SubjectRepository interface {
	// ImportSeed idempotently imports seeds (unique by code, existing skipped), returning the count inserted.
	ImportSeed(ctx context.Context, rows []*Subject) (int64, error)
	// List returns all subjects, sort_order ASC, id ASC.
	List(ctx context.Context) ([]*Subject, error)
	// ListByStageBit returns subjects visible in the stage (mask & bit != 0), sort_order ASC, id ASC.
	ListByStageBit(ctx context.Context, bit int) ([]*Subject, error)
	// ListFiltered returns subjects matching a stage bit AND a track bit. A zero bit means "do not
	// filter that dimension", so ListFiltered(ctx, 0, 0) == List.
	ListFiltered(ctx context.Context, stageBit, trackBit int) ([]*Subject, error)
	// ListByIDs returns the subjects whose id is in ids, sort_order ASC, id ASC (empty ids -> empty
	// slice). Expands a kid's kid_subjects set into dictionary rows.
	ListByIDs(ctx context.Context, ids []int64) ([]*Subject, error)
	// GetByID queries by id; ErrNotFound when missing.
	GetByID(ctx context.Context, id int64) (*Subject, error)
	// Insert adds a parent-created subject (source=SubjectSourceUser), returning the new id.
	Insert(ctx context.Context, s *Subject) (int64, error)
	// UpdateName renames, returning the affected count.
	UpdateName(ctx context.Context, id int64, name string) (int64, error)
	// UpdateMask sets the visibility mask (0~31), returning the affected count.
	UpdateMask(ctx context.Context, id int64, mask int) (int64, error)
	// CountExams returns how many exams reference the subject. The service blocks deletion while any
	// do — otherwise the schema would silently SET NULL and detach exams from their subject.
	CountExams(ctx context.Context, id int64) (int64, error)
	// CountKnowledgePoints returns how many knowledge points reference the subject. The service
	// blocks deletion while any do: subject_id is ON DELETE CASCADE, so deleting would silently
	// destroy them.
	CountKnowledgePoints(ctx context.Context, id int64) (int64, error)
	// Delete removes a subject, returning the affected count.
	//
	// Deleting a system-built subject (source=1) is intercepted by the service: hiding the button
	// is not enough — a direct API call would delete it and leave exams' subject_id dangling.
	Delete(ctx context.Context, id int64) (int64, error)
	// WithTx returns a new instance bound to tx.
	WithTx(tx Tx) SubjectRepository
}

// SubjectMaxNameLen is the subject-name length limit.
const SubjectMaxNameLen = 32
