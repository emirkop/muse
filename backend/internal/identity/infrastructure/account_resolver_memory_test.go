package infrastructure

import (
	"context"
	"testing"

	"muse-backend/internal/identity/domain"
)

func TestInMemoryAccountResolver_SameIdentityResolvesToSameAccount(t *testing.T) {
	resolver := NewInMemoryAccountResolver()
	identity := domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "apple-sub-1"}
	ctx := context.Background()

	first, firstIsNew, err := resolver.ResolveOrCreateAccount(ctx, identity)
	if err != nil {
		t.Fatalf("ResolveOrCreateAccount (1): %v", err)
	}
	second, secondIsNew, err := resolver.ResolveOrCreateAccount(ctx, identity)
	if err != nil {
		t.Fatalf("ResolveOrCreateAccount (2): %v", err)
	}

	if first != second {
		t.Fatalf("expected the same identity to resolve to the same account, got %q and %q", first, second)
	}
	if !firstIsNew {
		t.Error("expected the first resolution of a never-seen identity to report isNewAccount true")
	}
	if secondIsNew {
		t.Error("expected the second resolution of the same identity to report isNewAccount false")
	}
}

func TestInMemoryAccountResolver_DifferentIdentitiesResolveToDifferentAccounts(t *testing.T) {
	resolver := NewInMemoryAccountResolver()
	ctx := context.Background()

	apple, _, err := resolver.ResolveOrCreateAccount(ctx, domain.ExternalIdentity{Provider: domain.ProviderApple, Subject: "sub-a"})
	if err != nil {
		t.Fatalf("ResolveOrCreateAccount (apple): %v", err)
	}
	google, _, err := resolver.ResolveOrCreateAccount(ctx, domain.ExternalIdentity{Provider: domain.ProviderGoogle, Subject: "sub-a"})
	if err != nil {
		t.Fatalf("ResolveOrCreateAccount (google): %v", err)
	}

	if apple == google {
		t.Fatal("expected different providers with the same subject to resolve to different accounts")
	}
}
