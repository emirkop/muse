package infrastructure_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"muse-backend/internal/identity/domain"
	"muse-backend/internal/identity/infrastructure"
	"muse-backend/internal/platform/database"
)

func testPool(t *testing.T) *database.Pool {
	t.Helper()

	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping PostgreSQL integration tests")
	}

	ctx := context.Background()
	pool, err := database.Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("connect to TEST_DATABASE_URL: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	if _, err := pool.Pool().Exec(ctx, `TRUNCATE email_outbox, password_credentials, pending_signups, password_resets, auth_attempts, external_identities, accounts CASCADE`); err != nil {
		t.Fatalf("truncate test tables: %v", err)
	}

	return pool
}

func TestPostgresAccountRepository_CreateAndFindByLinkedIdentity(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresAccountRepository(pool.Pool())
	ctx := context.Background()

	created, err := repo.CreateWithLinkedIdentity(ctx,
		domain.Account{DisplayName: ""},
		domain.LinkedIdentity{Provider: domain.ProviderApple, Subject: "apple-sub-1"},
	)
	if err != nil {
		t.Fatalf("CreateWithLinkedIdentity: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected a non-empty generated account ID")
	}
	if created.IsDeleted() {
		t.Fatal("expected a freshly created account to not be deleted")
	}
	if created.AvatarID != "" {
		t.Errorf("expected a freshly created account's avatar_id to be empty ( not built yet), got %q", created.AvatarID)
	}

	found, err := repo.FindByLinkedIdentity(ctx, domain.ProviderApple, "apple-sub-1")
	if err != nil {
		t.Fatalf("FindByLinkedIdentity: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected FindByLinkedIdentity to return account %q, got %q", created.ID, found.ID)
	}
}

func TestPostgresAccountRepository_FindByLinkedIdentity_NotFound(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresAccountRepository(pool.Pool())

	_, err := repo.FindByLinkedIdentity(context.Background(), domain.ProviderGoogle, "does-not-exist")
	if !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestPostgresAccountRepository_CreateWithLinkedIdentity_DuplicateIdentity_ReturnsAlreadyExists(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresAccountRepository(pool.Pool())
	ctx := context.Background()

	identity := domain.LinkedIdentity{Provider: domain.ProviderGoogle, Subject: "google-sub-dup"}

	if _, err := repo.CreateWithLinkedIdentity(ctx, domain.Account{}, identity); err != nil {
		t.Fatalf("first CreateWithLinkedIdentity: %v", err)
	}

	_, err := repo.CreateWithLinkedIdentity(ctx, domain.Account{}, identity)
	if !errors.Is(err, domain.ErrLinkedIdentityAlreadyExists) {
		t.Fatalf("expected ErrLinkedIdentityAlreadyExists, got %v", err)
	}
}

func TestPostgresAccountRepository_FindByID(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresAccountRepository(pool.Pool())
	ctx := context.Background()

	created, err := repo.CreateWithLinkedIdentity(ctx,
		domain.Account{},
		domain.LinkedIdentity{Provider: domain.ProviderApple, Subject: "apple-sub-findbyid"},
	)
	if err != nil {
		t.Fatalf("CreateWithLinkedIdentity: %v", err)
	}

	found, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected account %q, got %q", created.ID, found.ID)
	}
}

func TestPostgresAccountRepository_FindByID_NotFound(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresAccountRepository(pool.Pool())

	_, err := repo.FindByID(context.Background(), domain.AccountID("00000000-0000-0000-0000-000000000000"))
	if !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestPostgresAccountRepository_UpdateDisplayName(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresAccountRepository(pool.Pool())
	ctx := context.Background()

	created, err := repo.CreateWithLinkedIdentity(ctx,
		domain.Account{},
		domain.LinkedIdentity{Provider: domain.ProviderApple, Subject: "apple-sub-name"},
	)
	if err != nil {
		t.Fatalf("CreateWithLinkedIdentity: %v", err)
	}

	if err := repo.UpdateDisplayName(ctx, created.ID, "Test Display Name"); err != nil {
		t.Fatalf("UpdateDisplayName: %v", err)
	}

	found, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.DisplayName != "Test Display Name" {
		t.Fatalf("expected display name %q, got %q", "Test Display Name", found.DisplayName)
	}
	if !found.UpdatedAt.After(created.UpdatedAt) && found.UpdatedAt != created.UpdatedAt {
		t.Error("expected UpdatedAt to advance after UpdateDisplayName")
	}
}

func TestPostgresAccountRepository_UpdateDisplayName_UnknownAccount(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresAccountRepository(pool.Pool())

	err := repo.UpdateDisplayName(context.Background(), domain.AccountID("00000000-0000-0000-0000-000000000000"), "x")
	if !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestPostgresAccountRepository_UpdateAvatar(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresAccountRepository(pool.Pool())
	ctx := context.Background()

	created, err := repo.CreateWithLinkedIdentity(ctx,
		domain.Account{},
		domain.LinkedIdentity{Provider: domain.ProviderApple, Subject: "apple-sub-avatar"},
	)
	if err != nil {
		t.Fatalf("CreateWithLinkedIdentity: %v", err)
	}

	if err := repo.UpdateAvatar(ctx, created.ID, domain.Avatar4); err != nil {
		t.Fatalf("UpdateAvatar: %v", err)
	}

	found, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found.AvatarID != domain.Avatar4 {
		t.Fatalf("expected avatar %q, got %q", domain.Avatar4, found.AvatarID)
	}
}

func TestPostgresAccountRepository_UpdateAvatar_UnknownAccount(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresAccountRepository(pool.Pool())

	err := repo.UpdateAvatar(context.Background(), domain.AccountID("00000000-0000-0000-0000-000000000000"), domain.Avatar1)
	if !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestPostgresAccountRepository_Deactivate_SoftDeletes(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresAccountRepository(pool.Pool())
	ctx := context.Background()

	created, err := repo.CreateWithLinkedIdentity(ctx,
		domain.Account{},
		domain.LinkedIdentity{Provider: domain.ProviderApple, Subject: "apple-sub-deactivate"},
	)
	if err != nil {
		t.Fatalf("CreateWithLinkedIdentity: %v", err)
	}

	if err := repo.Deactivate(ctx, created.ID); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	found, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("FindByID after deactivate: %v", err)
	}
	if !found.IsDeleted() {
		t.Fatal("expected account to be marked deleted after Deactivate")
	}

	stillLinked, err := repo.FindByLinkedIdentity(ctx, domain.ProviderApple, "apple-sub-deactivate")
	if err != nil {
		t.Fatalf("FindByLinkedIdentity after deactivate: %v", err)
	}
	if !stillLinked.IsDeleted() {
		t.Fatal("expected the linked identity's account to still show as deleted")
	}
}

func TestPostgresAccountRepository_Deactivate_IsIdempotent(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresAccountRepository(pool.Pool())
	ctx := context.Background()

	created, err := repo.CreateWithLinkedIdentity(ctx,
		domain.Account{},
		domain.LinkedIdentity{Provider: domain.ProviderApple, Subject: "apple-sub-idempotent"},
	)
	if err != nil {
		t.Fatalf("CreateWithLinkedIdentity: %v", err)
	}

	if err := repo.Deactivate(ctx, created.ID); err != nil {
		t.Fatalf("first Deactivate: %v", err)
	}
	if err := repo.Deactivate(ctx, created.ID); err != nil {
		t.Fatalf("second Deactivate (idempotent) should not error: %v", err)
	}
}
