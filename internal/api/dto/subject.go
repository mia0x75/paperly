package dto

import "paperly/internal/domain"

// SubjectSummary is a GET /api/subjects list item.
//
// **Source must be sent**: the settings page uses it to distinguish "system preset (🔒 not deletable)"
// from "self-created (deletable)". Without providing this fact to the frontend, it would have to hardcode
// a code whitelist — and once that whitelist diverges from seed, the delete button would appear on the wrong subjects.
type SubjectSummary struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	Source    int    `json:"source"`
	// Mask is the visibility bitmask: low 3 bits=stage (bit0=primary, bit1=junior,
	// bit2=senior), high 2 bits=track (bit3=arts, bit4=science).
	// The frontend renders/edits it as five checkboxes in the subject manager.
	Mask int `json:"mask"`
	// Mandatory marks a subject a kid cannot drop (语文 / 数学 = 1): the kid subject
	// editor force-checks it and the server force-includes it, so a set that omits it
	// is repaired (not rejected).
	Mandatory int `json:"mandatory"`
}

// SubjectCreated is the POST /api/subjects response (Created view).
type SubjectCreated struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// SubjectUpdateRequest is the PUT /api/subjects/:id body. Both fields are optional
// hooks so the same route serves rename (name) and stage-visibility (mask) edits.
type SubjectUpdateRequest struct {
	Name *string `json:"name"`
	Mask *int    `json:"mask"`
}

// KnowledgePointSummary is a knowledge-point list item.
//
// Detail only carries sort_order, no source: the knowledge-point tree does not distinguish
// preset/self-created (the design groups by subject, not by knowledge-point source).
type KnowledgePointSummary struct {
	ID        int64  `json:"id"`
	SubjectID int64  `json:"subject_id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

// KnowledgePointCreated is the POST knowledge-point response (Created view).
type KnowledgePointCreated struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// NewSubjectSummaries builds the subject list response in batch (guaranteeing an empty list serializes to []).
func NewSubjectSummaries(rows []*domain.Subject) []SubjectSummary {
	out := make([]SubjectSummary, 0, len(rows))
	for _, s := range rows {
		out = append(out, NewSubjectSummary(s))
	}
	return out
}

// NewSubjectSummary builds a single subject.
func NewSubjectSummary(s *domain.Subject) SubjectSummary {
	return SubjectSummary{
		ID: s.ID, Code: s.Code, Name: s.Name, SortOrder: s.SortOrder, Source: s.Source, Mask: s.GradeMask, Mandatory: s.Mandatory,
	}
}

// NewSubjectCreated builds the new-subject response.
func NewSubjectCreated(s *domain.Subject) SubjectCreated {
	return SubjectCreated{ID: s.ID, Name: s.Name}
}

// NewKnowledgePointSummaries builds the knowledge-point list in batch (guaranteeing an empty list serializes to []).
func NewKnowledgePointSummaries(rows []*domain.KnowledgePoint) []KnowledgePointSummary {
	out := make([]KnowledgePointSummary, 0, len(rows))
	for _, kp := range rows {
		out = append(out, NewKnowledgePointSummary(kp))
	}
	return out
}

// NewKnowledgePointSummary builds a single knowledge point.
func NewKnowledgePointSummary(kp *domain.KnowledgePoint) KnowledgePointSummary {
	return KnowledgePointSummary{
		ID: kp.ID, SubjectID: kp.SubjectID, Name: kp.Name, SortOrder: kp.SortOrder,
	}
}

// NewKnowledgePointCreated builds the new-knowledge-point response.
func NewKnowledgePointCreated(kp *domain.KnowledgePoint) KnowledgePointCreated {
	return KnowledgePointCreated{ID: kp.ID, Name: kp.Name}
}
