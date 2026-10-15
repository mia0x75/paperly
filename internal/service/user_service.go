package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"paperly/internal/domain"
)

// UserService holds the business logic for parent accounts (users table).
//
// Multi-parent support: onboarding registers the first parent at first startup, hence Create/List here.
// Renaming and changing the PIN go through AuthService (addressed by the user_id in the token); this service
// does not provide "default parent" read/write methods (Current/UpdateDisplayName/UpdatePin were removed).
type UserService struct {
	repo domain.UserRepository
}

// NewUserService constructs the parent account service.
func NewUserService(repo domain.UserRepository) *UserService {
	return &UserService{repo: repo}
}

// CreateUserReq is the input for creating a parent account.
type CreateUserReq struct {
	DisplayName string  // required, ≤24 runes, unique across the table
	Pin         *string // optional, 4-6 digits
}

// validateDisplayName validates a parent nickname: trims whitespace, non-empty, ≤24 runes (counting runes rather than bytes —
// 12 Chinese characters are 36 bytes, so counting bytes would wrongly reject valid input). Returns the normalized nickname.
// UserService.Create and AuthService.RenameParent share this same validation.
func validateDisplayName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("%w: parent display name cannot be empty", domain.ErrInvalidInput)
	}
	if n := len([]rune(trimmed)); n > domain.UserMaxDisplayNameLen {
		return "", fmt.Errorf("%w: parent display name too long (max %d, got %d)", domain.ErrInvalidInput, domain.UserMaxDisplayNameLen, n)
	}
	return trimmed, nil
}

// Create creates a parent account: validate nickname and PIN → hash → persist → return the new account.
func (s *UserService) Create(ctx context.Context, req CreateUserReq) (*domain.User, error) {
	name, err := validateDisplayName(req.DisplayName)
	if err != nil {
		return nil, err
	}
	exists, err := s.repo.ExistsByDisplayName(ctx, name, 0)
	if err != nil {
		return nil, fmt.Errorf("check user display name: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("%w: parent display name already exists", domain.ErrDuplicateDisplayName)
	}

	pin := ""
	if req.Pin != nil {
		pin = *req.Pin
	}
	if err := validatePin(pin); err != nil {
		return nil, err
	}
	hash, err := hashPin(pin)
	if err != nil {
		return nil, err
	}

	st := &domain.User{
		DisplayName: name,
		PinHash:     hash,
		PinVersion:  1,
		Status:      domain.UserStatusActive,
	}
	id, err := s.repo.Insert(ctx, st)
	if err != nil {
		if errors.Is(err, domain.ErrDuplicateDisplayName) {
			return nil, fmt.Errorf("%w: parent display name already exists", domain.ErrDuplicateDisplayName)
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	st.ID = id
	return st, nil
}

// List returns all parent accounts (by id ASC).
func (s *UserService) List(ctx context.Context) ([]*domain.User, error) {
	return s.repo.List(ctx)
}
