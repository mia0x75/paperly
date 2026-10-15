package domain

import "context"

// kid_subjects: per-kid whitelist of subjects the kid actually studies.
//
// Semantics (authoritative, no fallback): rows present = that is the kid's set; no rows = the kid
// has no subjects ("no records").
//
// This is the kid × subject dimension subjects.mask cannot express: a province that drops a subject,
// or a family hiding subjects, sets it here without touching the global dictionary. Consumers (radar
// axes, exam distribution, subject archives, the upload-page picker) read this set directly.

// KidSubjectRepository is the kid_subjects data-access contract.
type KidSubjectRepository interface {
	// WithTx returns a new instance bound to tx.
	WithTx(tx Tx) KidSubjectRepository
	// ListSubjectIDs returns the kid's whitelisted subject ids, empty slice when none configured
	// (the caller treats "none" as "no subjects"). Order is not significant.
	ListSubjectIDs(ctx context.Context, kidID int64) ([]int64, error)
	// ReplaceAll sets the whitelist to exactly subjectIDs (delete-then-insert in one transaction).
	// An empty list clears it.
	ReplaceAll(ctx context.Context, kidID int64, subjectIDs []int64) error
}
