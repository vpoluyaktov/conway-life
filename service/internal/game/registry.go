package game

import "sync"

// Registry is a thread-safe in-memory store for active Game instances.
type Registry struct {
	mu    sync.Mutex
	games map[string]*Game
}

func NewRegistry() *Registry {
	return &Registry{games: make(map[string]*Game)}
}

func (r *Registry) Put(g *Game) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.games[g.ID] = g
}

func (r *Registry) Get(id string) (*Game, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.games[id]
	return g, ok
}
