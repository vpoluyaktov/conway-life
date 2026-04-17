package server

import (
	"html/template"
	"net/http"

	"conway-life/internal/config"
	"conway-life/internal/game"
	"conway-life/internal/store"
	"conway-life/templates"
)

// Server wires together config, persistence, and the in-memory game registry.
type Server struct {
	cfg       *config.Config
	store     store.Store
	games     *game.Registry
	indexTmpl *template.Template
}

// New constructs a Server and parses the embedded index.html template.
func New(cfg *config.Config, st store.Store) *Server {
	tmpl := template.Must(template.ParseFS(templates.FS, "index.html"))
	return &Server{
		cfg:       cfg,
		store:     st,
		games:     game.NewRegistry(),
		indexTmpl: tmpl,
	}
}

// SetupRoutes registers all HTTP handlers and returns the configured mux.
func (s *Server) SetupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.handleHealth)
	// Exact match — prevents the catch-all from swallowing 405s on API routes.
	mux.HandleFunc("GET /{$}", s.handleIndex)

	mux.HandleFunc("POST /api/game/new", s.handleNewGame)
	mux.HandleFunc("GET /api/game/{id}", s.handleGetGame)
	mux.HandleFunc("POST /api/game/{id}/step", s.handleStepGame)
	mux.HandleFunc("POST /api/game/{id}/save", s.handleSaveSession)

	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleLoadSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)

	return mux
}
