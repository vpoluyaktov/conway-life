package store

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MockStore is a thread-safe in-memory implementation of Store for use in tests.
// Each method checks its corresponding *Fn field first; if nil, uses the default
// in-memory behaviour. Set a *Fn field to inject errors or custom logic in tests.
type MockStore struct {
	mu       sync.RWMutex
	sessions map[string]mockSession

	SaveSessionFn   func(ctx context.Context, meta SessionMeta, gens []GenerationSnapshot) error
	ListSessionsFn  func(ctx context.Context) ([]SessionMeta, error)
	LoadSessionFn   func(ctx context.Context, id string) (SessionMeta, []GenerationSnapshot, error)
	DeleteSessionFn func(ctx context.Context, id string) error
}

type mockSession struct {
	meta SessionMeta
	gens []GenerationSnapshot
}

func NewMockStore() *MockStore {
	return &MockStore{sessions: make(map[string]mockSession)}
}

func (m *MockStore) SaveSession(ctx context.Context, meta SessionMeta, gens []GenerationSnapshot) error {
	if m.SaveSessionFn != nil {
		return m.SaveSessionFn(ctx, meta, gens)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}
	gensCopy := make([]GenerationSnapshot, len(gens))
	copy(gensCopy, gens)
	m.sessions[meta.ID] = mockSession{meta: meta, gens: gensCopy}
	return nil
}

func (m *MockStore) ListSessions(ctx context.Context) ([]SessionMeta, error) {
	if m.ListSessionsFn != nil {
		return m.ListSessionsFn(ctx)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]SessionMeta, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s.meta)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (m *MockStore) LoadSession(ctx context.Context, id string) (SessionMeta, []GenerationSnapshot, error) {
	if m.LoadSessionFn != nil {
		return m.LoadSessionFn(ctx, id)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return SessionMeta{}, nil, ErrSessionNotFound
	}
	gensCopy := make([]GenerationSnapshot, len(s.gens))
	copy(gensCopy, s.gens)
	return s.meta, gensCopy, nil
}

func (m *MockStore) DeleteSession(ctx context.Context, id string) error {
	if m.DeleteSessionFn != nil {
		return m.DeleteSessionFn(ctx, id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; !ok {
		return ErrSessionNotFound
	}
	delete(m.sessions, id)
	return nil
}

func (m *MockStore) Close() error { return nil }
