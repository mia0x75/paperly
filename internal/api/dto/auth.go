package dto

import (
	"paperly/internal/domain"
	"paperly/internal/service"
)

// AccountSummary is the response item for GET /api/auth/parent-accounts (parent login-page selection).
// Exposes only id + display_name + pin_set: status is filtered by the service layer (active only),
// so it is never leaked; pin_set only reports whether a PIN is set, and pin_hash does not exist at the serialization layer.
// Sentinel values for an unset PIN (empty string / placeholder "!") always map to false.
type AccountSummary struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
	PinSet      bool   `json:"pin_set"`
}

// NewAccountSummaries maps the parent-account list (active only; the service already filters out disabled accounts).
func NewAccountSummaries(users []*domain.User) []AccountSummary {
	out := make([]AccountSummary, 0, len(users))
	for _, u := range users {
		out = append(out, AccountSummary{
			ID:          u.ID,
			DisplayName: u.DisplayName,
			PinSet:      u.PinHash != "" && u.PinHash != service.PinPlaceholder,
		})
	}
	return out
}

// LoginListKid is the response item for GET /api/auth/kid-accounts (kid login-page selection).
type LoginListKid struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// NewLoginListKids maps the kid list.
func NewLoginListKids(kids []*domain.Kid) []LoginListKid {
	out := make([]LoginListKid, 0, len(kids))
	for _, k := range kids {
		out = append(out, LoginListKid{ID: k.ID, Name: k.Name})
	}
	return out
}

// MeParent is the GET /api/me response body (parent-audience branch).
type MeParent struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"display_name"`
}

// NewMeParent maps the current parent.
func NewMeParent(u *domain.User) MeParent {
	return MeParent{ID: u.ID, DisplayName: u.DisplayName}
}

// MeKid is the GET /api/me response body (kid-audience branch).
type MeKid struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Grade int    `json:"grade"`
	Track int    `json:"track"`
}

// NewMeKid maps the current kid.
func NewMeKid(k *domain.Kid) MeKid {
	return MeKid{ID: k.ID, Name: k.Name, Grade: k.Grade, Track: k.Track}
}

// InviteVerifyResponse is the response body for POST /api/kid/invite/verify (anonymous).
// An invalid invite code still returns HTTP 200 + {valid:false} — it does not distinguish "code absent"
// from "code valid but kid unavailable"; kid_id is not returned (prevents code-to-id enumeration).
type InviteVerifyResponse struct {
	Valid bool   `json:"valid"`
	Name  string `json:"name,omitempty"`
}

// InviteSetupResponse is the response body for POST /api/kid/invite/setup (anonymous).
// Setting the PIN successfully issues the kid JWT immediately, saving the frontend from another login page.
type InviteSetupResponse struct {
	Token string `json:"token"`
	Kid   MeKid  `json:"kid"`
}
