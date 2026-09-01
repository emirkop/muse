package infrastructure

import (
	"context"
	"testing"
	"time"

	"muse-backend/internal/identity/domain"
)

func newTestSession() (domain.Session, domain.RefreshToken) {
	now := time.Now()
	session := domain.Session{
		ID:        "sess_1",
		AccountID: "acct_1",
		FamilyID:  "fam_1",
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	refresh := domain.RefreshToken{
		SessionID: session.ID,
		FamilyID:  session.FamilyID,
		Digest:    "digest-1",
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}
	return session, refresh
}

func TestInMemorySessionStore_CreateAndFind(t *testing.T) {
	store := NewInMemorySessionStore()
	session, refresh := newTestSession()
	ctx := context.Background()

	if err := store.CreateSession(ctx, session, refresh); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	gotSession, gotRefresh, err := store.FindByRefreshDigest(ctx, refresh.Digest)
	if err != nil {
		t.Fatalf("FindByRefreshDigest: %v", err)
	}
	if gotSession.ID != session.ID {
		t.Errorf("expected session ID %q, got %q", session.ID, gotSession.ID)
	}
	if gotRefresh.WasRotated() {
		t.Error("expected a freshly created refresh token to not be rotated")
	}
}

func TestInMemorySessionStore_FindByRefreshDigest_UnknownDigest(t *testing.T) {
	store := NewInMemorySessionStore()

	_, _, err := store.FindByRefreshDigest(context.Background(), "does-not-exist")
	if err != domain.ErrRefreshTokenNotFound {
		t.Fatalf("expected ErrRefreshTokenNotFound, got %v", err)
	}
}

func TestInMemorySessionStore_RotateRefreshToken_MarksOldRotatedAndAddsNew(t *testing.T) {
	store := NewInMemorySessionStore()
	session, refresh := newTestSession()
	ctx := context.Background()
	_ = store.CreateSession(ctx, session, refresh)

	newToken := domain.RefreshToken{
		SessionID: session.ID,
		FamilyID:  session.FamilyID,
		Digest:    "digest-2",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.RotateRefreshToken(ctx, refresh.Digest, newToken); err != nil {
		t.Fatalf("RotateRefreshToken: %v", err)
	}

	_, oldGot, err := store.FindByRefreshDigest(ctx, refresh.Digest)
	if err != nil {
		t.Fatalf("FindByRefreshDigest(old): %v", err)
	}
	if !oldGot.WasRotated() {
		t.Fatal("expected the old refresh token to be marked rotated after RotateRefreshToken")
	}

	_, newGot, err := store.FindByRefreshDigest(ctx, "digest-2")
	if err != nil {
		t.Fatalf("FindByRefreshDigest(new): %v", err)
	}
	if newGot.WasRotated() {
		t.Fatal("expected the newly rotated-in refresh token to not itself be marked rotated")
	}
}

func TestInMemorySessionStore_RevokeSession(t *testing.T) {
	store := NewInMemorySessionStore()
	session, refresh := newTestSession()
	ctx := context.Background()
	_ = store.CreateSession(ctx, session, refresh)

	if err := store.RevokeSession(ctx, session.ID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	gotSession, _, err := store.FindByRefreshDigest(ctx, refresh.Digest)
	if err != nil {
		t.Fatalf("FindByRefreshDigest: %v", err)
	}
	if !gotSession.IsRevoked() {
		t.Fatal("expected session to be revoked")
	}
}

func TestInMemorySessionStore_RevokeFamily_RevokesMatchingSession(t *testing.T) {
	store := NewInMemorySessionStore()
	session, refresh := newTestSession()
	ctx := context.Background()
	_ = store.CreateSession(ctx, session, refresh)

	if err := store.RevokeFamily(ctx, session.FamilyID); err != nil {
		t.Fatalf("RevokeFamily: %v", err)
	}

	gotSession, _, err := store.FindByRefreshDigest(ctx, refresh.Digest)
	if err != nil {
		t.Fatalf("FindByRefreshDigest: %v", err)
	}
	if !gotSession.IsRevoked() {
		t.Fatal("expected session sharing the revoked family to be revoked")
	}
}
