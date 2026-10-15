// Package dto defines the API-layer public contract (JSON serialization structs).
//
// Why it is separated from domain (ADR 0002):
//   - Security: sensitive fields (pin_hash) do not exist in any view, so they cannot leak at the serialization layer;
//   - Contract specialization: access_code is returned only once in the create response; list/detail views have different field sets.
package dto

import (
	"time"

	"paperly/internal/domain"
	"paperly/internal/service"
)

// KidCreated is the response body for POST /api/kids.
// AccessCode appears only once in this view (after creation it is never returned).
type KidCreated struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	AccessCode string    `json:"access_code"`
	KidURL     string    `json:"kid_url"`
	CreatedAt  time.Time `json:"created_at"`
}

// KidDetail is the response body for GET/PUT /api/kids/:id.
// Excludes access_code (to prevent re-sharing) and pin_hash.
type KidDetail struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Grade     int       `json:"grade"`
	Gender    *int      `json:"gender"`
	Avatar    *string   `json:"avatar"`
	Status    int       `json:"status"`
	Track     int       `json:"track"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KidSummary is a GET /api/kids list item.
// More minimal than Detail: no timestamps or note fields. Track is kept: the
// exam create page / radar chart filter the subject list by a kid's grade+track
// straight from the store, so the list view must carry it.
type KidSummary struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Grade  int    `json:"grade"`
	Status int    `json:"status"`
	Track  int    `json:"track"`
}

// ResetAccessCodeResponse is the response body for POST /api/kids/:id/access_code/reset.
// As with the create response, access_code appears only in this view — after a reset it is handed to the parent immediately and never returned again.
type ResetAccessCodeResponse struct {
	KidID      int64  `json:"kid_id"`
	AccessCode string `json:"access_code"`
	KidURL     string `json:"kid_url"`
}

// ResetPinResponse is the response body for POST /api/kids/:id/pin/reset.
// Only reports whether a PIN is set; never echoes pin_hash.
type ResetPinResponse struct {
	KidID  int64 `json:"kid_id"`
	PinSet bool  `json:"pin_set"`
}

// NewResetAccessCodeResponseonse builds the reset-access-code response from the domain model.
func NewResetAccessCodeResponseonse(s *domain.Kid) ResetAccessCodeResponse {
	return ResetAccessCodeResponse{
		KidID:      s.ID,
		AccessCode: s.AccessCode,
		KidURL:     "/invite/" + s.AccessCode,
	}
}

// NewResetPinResponseonse builds the reset-PIN response (PinSet is derived from whether pin_hash is the placeholder).
// Any hash other than the placeholder is treated as "PIN set" — the internal placeholder must not leak into the API semantics.
func NewResetPinResponseonse(s *domain.Kid) ResetPinResponse {
	return ResetPinResponse{
		KidID:  s.ID,
		PinSet: s.PinHash != service.PinPlaceholder,
	}
}

// NewKidDetail builds the detail response from the domain model.
func NewKidDetail(s *domain.Kid) KidDetail {
	return KidDetail{
		ID:        s.ID,
		Name:      s.Name,
		Grade:     s.Grade,
		Gender:    s.Gender,
		Avatar:    s.Avatar,
		Status:    s.Status,
		Track:     s.Track,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

// NewKidSummaries builds a batch of list responses (guaranteeing an empty list serializes to []).
func NewKidSummaries(list []*domain.Kid) []KidSummary {
	out := make([]KidSummary, 0, len(list))
	for _, s := range list {
		out = append(out, KidSummary{
			ID:     s.ID,
			Name:   s.Name,
			Grade:  s.Grade,
			Status: s.Status,
			Track:  s.Track,
		})
	}
	return out
}
