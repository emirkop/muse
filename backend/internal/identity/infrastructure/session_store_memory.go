package infrastructure

import (
	"context"
	"sync"
	"time"

	"muse-backend/internal/identity/domain"
)

type InMemorySessionStore struct {
	mu       sync.Mutex
	sessions map[domain.SessionID]domain.Session
	refresh  map[string]domain.RefreshToken
}

func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{
		sessions: make(map[domain.SessionID]domain.Session),
		refresh:  make(map[string]domain.RefreshToken),
	}
}

func (s *InMemorySessionStore) CreateSession(_ context.Context, session domain.Session, refresh domain.RefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[session.ID] = session
	s.refresh[refresh.Digest] = refresh
	return nil
}

func (s *InMemorySessionStore) FindByRefreshDigest(_ context.Context, digest string) (domain.Session, domain.RefreshToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	refresh, ok := s.refresh[digest]
	if !ok {
		return domain.Session{}, domain.RefreshToken{}, domain.ErrRefreshTokenNotFound
	}
	session, ok := s.sessions[refresh.SessionID]
	if !ok {
		return domain.Session{}, domain.RefreshToken{}, domain.ErrSessionNotFound
	}
	return session, refresh, nil
}

func (s *InMemorySessionStore) RotateRefreshToken(_ context.Context, oldDigest string, newToken domain.RefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, ok := s.refresh[oldDigest]
	if !ok {
		return domain.ErrRefreshTokenNotFound
	}

	now := time.Now()
	old.RotatedAt = &now
	s.refresh[oldDigest] = old
	s.refresh[newToken.Digest] = newToken
	return nil
}

func (s *InMemorySessionStore) RevokeSession(_ context.Context, sessionID domain.SessionID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return domain.ErrSessionNotFound
	}

	now := time.Now()
	session.RevokedAt = &now
	s.sessions[sessionID] = session
	return nil
}

func (s *InMemorySessionStore) RevokeAllForAccount(_ context.Context, accountID domain.AccountID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, session := range s.sessions {
		if session.AccountID == accountID && !session.IsRevoked() {
			session.RevokedAt = &now
			s.sessions[id] = session
		}
	}
	return nil
}

func (s *InMemorySessionStore) RevokeFamily(_ context.Context, familyID domain.FamilyID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, session := range s.sessions {
		if session.FamilyID == familyID && !session.IsRevoked() {
			session.RevokedAt = &now
			s.sessions[id] = session
		}
	}
	return nil
}
