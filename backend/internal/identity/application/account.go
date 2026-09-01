package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"muse-backend/internal/identity/domain"
)

type AccountService struct {
	repo AccountRepository
}

func NewAccountService(repo AccountRepository) *AccountService {
	return &AccountService{repo: repo}
}

func (s *AccountService) ResolveOrCreateAccount(ctx context.Context, identity domain.ExternalIdentity) (domain.AccountID, bool, error) {
	account, err := s.repo.FindByLinkedIdentity(ctx, identity.Provider, identity.Subject)
	switch {
	case err == nil:
		if account.IsDeleted() {
			return "", false, domain.ErrAccountDeactivated
		}
		return account.ID, false, nil

	case errors.Is(err, domain.ErrAccountNotFound):
		id, createErr := s.createAccount(ctx, identity)
		if createErr != nil {
			return "", false, createErr
		}
		return id, true, nil

	default:
		return "", false, fmt.Errorf("account: resolve: %w", err)
	}
}

func (s *AccountService) createAccount(ctx context.Context, identity domain.ExternalIdentity) (domain.AccountID, error) {
	now := time.Now()
	newAccount := domain.Account{
		DisplayName: "",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	newIdentity := domain.LinkedIdentity{
		Provider:  identity.Provider,
		Subject:   identity.Subject,
		CreatedAt: now,
	}

	created, err := s.repo.CreateWithLinkedIdentity(ctx, newAccount, newIdentity)
	if err != nil {
		if errors.Is(err, domain.ErrLinkedIdentityAlreadyExists) {
			existing, findErr := s.repo.FindByLinkedIdentity(ctx, identity.Provider, identity.Subject)
			if findErr != nil {
				return "", fmt.Errorf("account: re-resolve after concurrent creation: %w", findErr)
			}
			return existing.ID, nil
		}
		return "", fmt.Errorf("account: create: %w", err)
	}
	return created.ID, nil
}

func (s *AccountService) FindByID(ctx context.Context, id domain.AccountID) (domain.Account, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *AccountService) UpdateDisplayName(ctx context.Context, id domain.AccountID, displayName string) error {
	return s.repo.UpdateDisplayName(ctx, id, displayName)
}

func (s *AccountService) UpdateAvatar(ctx context.Context, id domain.AccountID, avatarID domain.AvatarID) error {
	if !domain.IsValidAvatarID(avatarID) {
		return domain.ErrInvalidAvatarID
	}
	return s.repo.UpdateAvatar(ctx, id, avatarID)
}

func (s *AccountService) Deactivate(ctx context.Context, id domain.AccountID) error {
	return s.repo.Deactivate(ctx, id)
}
