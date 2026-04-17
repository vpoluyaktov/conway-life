package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"conway-life/internal/game"
	"conway-life/internal/store"

	"github.com/google/uuid"
)

// indexData is the template data for the UI page.
type indexData struct {
	Version     string
	Environment string
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.indexTmpl.Execute(w, indexData{
		Version:     s.cfg.Version,
		Environment: s.cfg.Environment,
	}); err != nil {
		log.Printf("template execute: %v", err)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":      "ok",
		"version":     s.cfg.Version,
		"environment": s.cfg.Environment,
	})
}

// --- Game handlers (in-memory) ---

type newGameRequest struct {
	Width  int      `json:"width"`
	Height int      `json:"height"`
	Cells  [][2]int `json:"cells"`
}

func (s *Server) handleNewGame(w http.ResponseWriter, r *http.Request) {
	var req newGameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	if req.Width < 1 || req.Width > s.cfg.MaxBoardWidth {
		jsonError(w, fmt.Sprintf("width must be between 1 and %d", s.cfg.MaxBoardWidth), http.StatusBadRequest)
		return
	}
	if req.Height < 1 || req.Height > s.cfg.MaxBoardHeight {
		jsonError(w, fmt.Sprintf("height must be between 1 and %d", s.cfg.MaxBoardHeight), http.StatusBadRequest)
		return
	}
	g, err := game.NewGame(req.Width, req.Height, req.Cells)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.games.Put(g)
	writeJSON(w, http.StatusCreated, g.Snapshot())
}

func (s *Server) handleGetGame(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g, ok := s.games.Get(id)
	if !ok {
		jsonError(w, "game not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, g.Snapshot())
}

func (s *Server) handleStepGame(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g, ok := s.games.Get(id)
	if !ok {
		jsonError(w, "game not found", http.StatusNotFound)
		return
	}

	var req struct {
		Steps *int `json:"steps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		jsonError(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	steps := 1 // default when field is absent or body is empty
	if req.Steps != nil {
		steps = *req.Steps
	}
	if steps < 1 || steps > 500 {
		jsonError(w, "steps must be between 1 and 500", http.StatusBadRequest)
		return
	}

	var state game.GameState
	for i := 0; i < steps; i++ {
		state = g.Step()
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleSaveSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g, ok := s.games.Get(id)
	if !ok {
		jsonError(w, "game not found", http.StatusNotFound)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		jsonError(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	info := g.SaveInfo()

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fmt.Sprintf("Game %s @ gen %d", info.ID[:8], info.Generation)
	}
	if len(name) > 100 {
		jsonError(w, "name too long (max 100 chars)", http.StatusBadRequest)
		return
	}

	generationCount := len(info.History)
	if info.Width*info.Height*generationCount > s.cfg.MaxSessionCells {
		jsonError(w, "session too large to save", http.StatusRequestEntityTooLarge)
		return
	}

	sessionID := uuid.NewString()
	now := time.Now().UTC()
	meta := store.SessionMeta{
		ID:              sessionID,
		Name:            name,
		Width:           info.Width,
		Height:          info.Height,
		GenerationCount: generationCount,
		CreatedAt:       now,
	}

	gens := make([]store.GenerationSnapshot, generationCount)
	for i, snap := range info.History {
		gens[i] = store.GenerationSnapshot{
			Generation: i,
			Cells:      store.EncodeCells(snap, info.Width, info.Height),
		}
	}

	if err := s.store.SaveSession(r.Context(), meta, gens); err != nil {
		if errors.Is(err, store.ErrStoreUnavailable) {
			jsonError(w, "service unavailable: Firestore is disabled", http.StatusServiceUnavailable)
			return
		}
		log.Printf("SaveSession: %v", err)
		jsonError(w, "failed to save session", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, meta)
}

// --- Session handlers (Firestore) ---

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.ListSessions(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrStoreUnavailable) {
			jsonError(w, "service unavailable: Firestore is disabled", http.StatusServiceUnavailable)
			return
		}
		log.Printf("ListSessions: %v", err)
		jsonError(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []store.SessionMeta{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// generationResponse is the JSON wire format for one generation in a loaded session.
type generationResponse struct {
	Generation int        `json:"generation"`
	Cells      [][]uint16 `json:"cells"`
}

// sessionResponse is the JSON wire format for GET /api/sessions/{id}.
type sessionResponse struct {
	store.SessionMeta
	Generations []generationResponse `json:"generations"`
}

func (s *Server) handleLoadSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, gens, err := s.store.LoadSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrSessionNotFound) {
			jsonError(w, "session not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, store.ErrStoreUnavailable) {
			jsonError(w, "service unavailable: Firestore is disabled", http.StatusServiceUnavailable)
			return
		}
		log.Printf("LoadSession: %v", err)
		jsonError(w, "failed to load session", http.StatusInternalServerError)
		return
	}

	genResponses := make([]generationResponse, len(gens))
	for i, gn := range gens {
		genResponses[i] = generationResponse{
			Generation: gn.Generation,
			Cells:      store.DecodeCells(gn.Cells, meta.Width, meta.Height),
		}
	}

	writeJSON(w, http.StatusOK, sessionResponse{
		SessionMeta: meta,
		Generations: genResponses,
	})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteSession(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrSessionNotFound) {
			jsonError(w, "session not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, store.ErrStoreUnavailable) {
			jsonError(w, "service unavailable: Firestore is disabled", http.StatusServiceUnavailable)
			return
		}
		log.Printf("DeleteSession: %v", err)
		jsonError(w, "failed to delete session", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
