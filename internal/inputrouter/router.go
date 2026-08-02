package inputrouter

import (
	"context"
	"fmt"
	"sync"
)

// Router decides and optionally persists a route (Task 150).
type Router struct {
	Store Store
}

// Store persists original requests and decisions.
type Store interface {
	Put(ctx context.Context, in InputRequest, d RouteDecision) error
	GetByIdempotency(ctx context.Context, key string) (InputRequest, RouteDecision, bool, error)
}

// MemoryStore is an in-memory Store.
type MemoryStore struct {
	mu   sync.Mutex
	byID map[string]struct {
		In InputRequest
		D  RouteDecision
	}
	byIdem map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:   map[string]struct {
			In InputRequest
			D  RouteDecision
		}{},
		byIdem: map[string]string{},
	}
}

func (m *MemoryStore) Put(_ context.Context, in InputRequest, d RouteDecision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byID[in.RequestID] = struct {
		In InputRequest
		D  RouteDecision
	}{In: in, D: d}
	if in.IdempotencyKey != "" {
		m.byIdem[in.IdempotencyKey] = in.RequestID
	}
	return nil
}

func (m *MemoryStore) GetByIdempotency(_ context.Context, key string) (InputRequest, RouteDecision, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byIdem[key]
	if !ok {
		return InputRequest{}, RouteDecision{}, false, nil
	}
	row := m.byID[id]
	return row.In, row.D, true, nil
}

// Route validates, decides, and persists. Duplicate idempotency keys return the
// prior decision without creating a second run.
func (r *Router) Route(ctx context.Context, in InputRequest) (RouteDecision, error) {
	if r.Store != nil && in.IdempotencyKey != "" {
		_, prev, ok, err := r.Store.GetByIdempotency(ctx, in.IdempotencyKey)
		if err != nil {
			return RouteDecision{}, err
		}
		if ok {
			return prev, nil
		}
	}
	d, err := DecideRoute(in)
	if err != nil {
		return RouteDecision{}, err
	}
	if d.RefuseReason != "" {
		if r.Store != nil {
			_ = r.Store.Put(ctx, in, d)
		}
		return d, nil
	}
	if d.Route == "" {
		return RouteDecision{}, fmt.Errorf("inputrouter: empty route")
	}
	if r.Store != nil {
		if err := r.Store.Put(ctx, in, d); err != nil {
			return RouteDecision{}, err
		}
	}
	return d, nil
}
