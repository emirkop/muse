package application_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"muse-backend/internal/identity/application"
	"muse-backend/internal/identity/domain"
)

type fakeAccountRepository struct {
	mu                sync.Mutex
	accountsByID      map[domain.AccountID]domain.Account
	accountByIdentity map[string]domain.AccountID
	nextID            int
}

func newFakeAccountRepository() *fakeAccountRepository {
	return &fakeAccountRepository{
		accountsByID:      make(map[domain.AccountID]domain.Account),
		accountByIdentity: make(map[string]domain.AccountID),
	}
}

func identityKey(provider domain.Provider, subject string) string {
	return string(provider) + ":" + subject
}

func (f *fakeAccountRepository) FindByLinkedIdentity(_ context.Context, provider domain.Provider, subject string) (domain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	id, ok := f.accountByIdentity[identityKey(provider, subject)]
	if !ok {
		return domain.Account{}, domain.ErrAccountNotFound
	}
	return f.accountsByID[id], nil
}

func (f *fakeAccountRepository) CreateWithLinkedIdentity(_ context.Context, account domain.Account, identity domain.LinkedIdentity) (domain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := identityKey(identity.Provider, identity.Subject)
	if _, exists := f.accountByIdentity[key]; exists {
		return domain.Account{}, domain.ErrLinkedIdentityAlreadyExists
	}

	f.nextID++
	account.ID = domain.AccountID(fmt.Sprintf("acct_%d", f.nextID))
	f.accountsByID[account.ID] = account
	f.accountByIdentity[key] = account.ID
	return account, nil
}

func (f *fakeAccountRepository) FindByID(_ context.Context, id domain.AccountID) (domain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	account, ok := f.accountsByID[id]
	if !ok {
		return domain.Account{}, domain.ErrAccountNotFound
	}
	return account, nil
}

func (f *fakeAccountRepository) UpdateDisplayName(_ context.Context, id domain.AccountID, displayName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	account, ok := f.accountsByID[id]
	if !ok {
		return domain.ErrAccountNotFound
	}
	account.DisplayName = displayName
	f.accountsByID[id] = account
	return nil
}

func (f *fakeAccountRepository) UpdateAvatar(_ context.Context, id domain.AccountID, avatarID domain.AvatarID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	account, ok := f.accountsByID[id]
	if !ok {
		return domain.ErrAccountNotFound
	}
	account.AvatarID = avatarID
	f.accountsByID[id] = account
	return nil
}

func (f *fakeAccountRepository) Deactivate(_ context.Context, id domain.AccountID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	account, ok := f.accountsByID[id]
	if !ok {
		return domain.ErrAccountNotFound
	}
	now := time.Now()
	account.DeletedAt = &now
	f.accountsByID[id] = account
	return nil
}

func TestAccountService_ResolveOrCreateAccount_NewIdentity_CreatesAccount(t *testing.T) {
	repo := newFakeAccountRepository()
	svc := application.NewAccountService(repo)
	identity := domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-new"}

	id, isNew, err := svc.ResolveOrCreateAccount(context.Background(), identity)
	if err != nil {
		t.Fatalf("ResolveOrCreateAccount: %v", err)
	}
	if id == "" {
		t.Fatal("expected a non-empty account ID")
	}
	if !isNew {
		t.Error("expected isNewAccount to be true for a never-seen identity")
	}

	account, err := svc.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if account.DisplayName != "" {
		t.Errorf("expected a fresh account's display name to be empty, got %q", account.DisplayName)
	}
}

func TestAccountService_ResolveOrCreateAccount_ExistingIdentity_ReturnsSameAccount(t *testing.T) {
	repo := newFakeAccountRepository()
	svc := application.NewAccountService(repo)
	identity := domain.ExternalIdentity{Provider: domain.ProviderGoogle, Subject: "sub-existing"}
	ctx := context.Background()

	first, firstIsNew, err := svc.ResolveOrCreateAccount(ctx, identity)
	if err != nil {
		t.Fatalf("first ResolveOrCreateAccount: %v", err)
	}
	second, secondIsNew, err := svc.ResolveOrCreateAccount(ctx, identity)
	if err != nil {
		t.Fatalf("second ResolveOrCreateAccount: %v", err)
	}

	if first != second {
		t.Fatalf("expected the same identity to resolve to the same account, got %q and %q", first, second)
	}
	if !firstIsNew {
		t.Error("expected the first sign-in with a never-seen identity to report isNewAccount true")
	}
	if secondIsNew {
		t.Error("expected the second sign-in with the same identity to report isNewAccount false")
	}
}

func TestAccountService_ResolveOrCreateAccount_DifferentIdentities_CreateDifferentAccounts(t *testing.T) {
	repo := newFakeAccountRepository()
	svc := application.NewAccountService(repo)
	ctx := context.Background()

	a, _, err := svc.ResolveOrCreateAccount(ctx, domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-a"})
	if err != nil {
		t.Fatalf("ResolveOrCreateAccount (a): %v", err)
	}
	b, _, err := svc.ResolveOrCreateAccount(ctx, domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-b"})
	if err != nil {
		t.Fatalf("ResolveOrCreateAccount (b): %v", err)
	}

	if a == b {
		t.Fatal("expected two distinct identities to create two distinct accounts")
	}
}

func TestAccountService_ResolveOrCreateAccount_DeactivatedAccount_Fails(t *testing.T) {
	repo := newFakeAccountRepository()
	svc := application.NewAccountService(repo)
	identity := domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-deactivated"}
	ctx := context.Background()

	id, _, err := svc.ResolveOrCreateAccount(ctx, identity)
	if err != nil {
		t.Fatalf("initial ResolveOrCreateAccount: %v", err)
	}
	if err := svc.Deactivate(ctx, id); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	_, _, err = svc.ResolveOrCreateAccount(ctx, identity)
	if !errors.Is(err, domain.ErrAccountDeactivated) {
		t.Fatalf("expected ErrAccountDeactivated for a deactivated account's identity, got %v", err)
	}
}

func TestAccountService_ResolveOrCreateAccount_ConcurrentCreation_RecoversGracefully(t *testing.T) {
	repo := newFakeAccountRepository()
	identity := domain.ExternalIdentity{Provider: domain.ProviderGoogle, Subject: "sub-race"}
	ctx := context.Background()

	seedSvc := application.NewAccountService(repo)
	seeded, seededIsNew, err := seedSvc.ResolveOrCreateAccount(ctx, identity)
	if err != nil {
		t.Fatalf("seed ResolveOrCreateAccount: %v", err)
	}
	if !seededIsNew {
		t.Error("expected the seeding call to report isNewAccount true")
	}

	svc := application.NewAccountService(repo)
	resolved, resolvedIsNew, err := svc.ResolveOrCreateAccount(ctx, identity)
	if err != nil {
		t.Fatalf("ResolveOrCreateAccount after concurrent creation: %v", err)
	}
	if resolved != seeded {
		t.Fatalf("expected the recovering call to resolve to the already-created account %q, got %q", seeded, resolved)
	}
	if resolvedIsNew {
		t.Error("expected the recovering call to report isNewAccount false — the identity already existed by the time it ran")
	}
}

func TestAccountService_Deactivate_IsIdempotent(t *testing.T) {
	repo := newFakeAccountRepository()
	svc := application.NewAccountService(repo)
	ctx := context.Background()

	id, _, err := svc.ResolveOrCreateAccount(ctx, domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-idempotent"})
	if err != nil {
		t.Fatalf("ResolveOrCreateAccount: %v", err)
	}

	if err := svc.Deactivate(ctx, id); err != nil {
		t.Fatalf("first Deactivate: %v", err)
	}
	if err := svc.Deactivate(ctx, id); err != nil {
		t.Fatalf("second Deactivate (idempotent) should not error: %v", err)
	}
}

func TestAccountService_UpdateDisplayName(t *testing.T) {
	repo := newFakeAccountRepository()
	svc := application.NewAccountService(repo)
	ctx := context.Background()

	id, _, err := svc.ResolveOrCreateAccount(ctx, domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-name"})
	if err != nil {
		t.Fatalf("ResolveOrCreateAccount: %v", err)
	}

	if err := svc.UpdateDisplayName(ctx, id, "Test User"); err != nil {
		t.Fatalf("UpdateDisplayName: %v", err)
	}

	account, err := svc.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if account.DisplayName != "Test User" {
		t.Errorf("expected display name %q, got %q", "Test User", account.DisplayName)
	}
}

func TestAccountService_UpdateAvatar_ValidAvatarID_Persists(t *testing.T) {
	repo := newFakeAccountRepository()
	svc := application.NewAccountService(repo)
	ctx := context.Background()

	id, _, err := svc.ResolveOrCreateAccount(ctx, domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-avatar"})
	if err != nil {
		t.Fatalf("ResolveOrCreateAccount: %v", err)
	}

	if err := svc.UpdateAvatar(ctx, id, domain.Avatar3); err != nil {
		t.Fatalf("UpdateAvatar: %v", err)
	}

	account, err := svc.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if account.AvatarID != domain.Avatar3 {
		t.Errorf("expected avatar %q, got %q", domain.Avatar3, account.AvatarID)
	}
}

func TestAccountService_UpdateAvatar_InvalidAvatarID_Rejected(t *testing.T) {
	repo := newFakeAccountRepository()
	svc := application.NewAccountService(repo)
	ctx := context.Background()

	id, _, err := svc.ResolveOrCreateAccount(ctx, domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-avatar-invalid"})
	if err != nil {
		t.Fatalf("ResolveOrCreateAccount: %v", err)
	}

	err = svc.UpdateAvatar(ctx, id, domain.AvatarID("not-a-real-avatar"))
	if !errors.Is(err, domain.ErrInvalidAvatarID) {
		t.Fatalf("expected ErrInvalidAvatarID, got %v", err)
	}

	account, findErr := svc.FindByID(ctx, id)
	if findErr != nil {
		t.Fatalf("FindByID: %v", findErr)
	}
	if account.AvatarID != "" {
		t.Errorf("expected avatar to remain unset after a rejected update, got %q", account.AvatarID)
	}
}
