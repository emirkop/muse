package infrastructure

import (
	"context"
	"fmt"
	"sync"

	"muse-backend/internal/identity/domain"
)

type InMemoryAccountResolver struct {
	mu         sync.Mutex
	byIdentity map[string]domain.AccountID
	nextID     int
}

func NewInMemoryAccountResolver() *InMemoryAccountResolver {
	return &InMemoryAccountResolver{byIdentity: make(map[string]domain.AccountID)}
}

func (r *InMemoryAccountResolver) ResolveOrCreateAccount(_ context.Context, identity domain.ExternalIdentity) (domain.AccountID, bool, error) {
	key := string(identity.Provider) + ":" + identity.Subject

	r.mu.Lock()
	defer r.mu.Unlock()

	if id, ok := r.byIdentity[key]; ok {
		return id, false, nil
	}

	r.nextID++
	id := domain.AccountID(fmt.Sprintf("acct_%d", r.nextID))
	r.byIdentity[key] = id
	return id, true, nil
}
