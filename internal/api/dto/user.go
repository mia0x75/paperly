package dto

import "paperly/internal/domain"

// UserSummary is a GET /api/users list item and the POST /api/users response body (Summary view).
// The old GET/PUT /api/users/me have been removed; reading and writing the logged-in account now goes through /api/me*.
//
// Business identifier = display_name. **No pin_hash**: credentials do not exist at the serialization layer,
// the same rule as kids / exams credential handling (ADR 0002).
// PinSet is derived from whether pin_hash is empty, only reporting whether a PIN is set; the hash is never echoed.
type UserSummary struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
	PinSet      bool   `json:"pin_set"`
	Status      int    `json:"status"`
}

// NewUserSummary maps a parent account.
func NewUserSummary(u *domain.User) UserSummary {
	return UserSummary{
		ID:          u.ID,
		DisplayName: u.DisplayName,
		PinSet:      u.PinHash != "",
		Status:      u.Status,
	}
}

// NewUserSummaries maps the parent-account list.
func NewUserSummaries(list []*domain.User) []UserSummary {
	out := make([]UserSummary, 0, len(list))
	for _, u := range list {
		out = append(out, NewUserSummary(u))
	}
	return out
}
