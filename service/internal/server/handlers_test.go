package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"conway-life/internal/config"
	"conway-life/internal/game"
	"conway-life/internal/store"
)

// ---------------------------------------------------------------------------
// MockStore — hand-written, no testify dependency.
// ---------------------------------------------------------------------------

type MockStore struct {
	SaveSessionFn   func(ctx context.Context, meta store.SessionMeta, gens []store.GenerationSnapshot) error
	ListSessionsFn  func(ctx context.Context) ([]store.SessionMeta, error)
	LoadSessionFn   func(ctx context.Context, id string) (store.SessionMeta, []store.GenerationSnapshot, error)
	DeleteSessionFn func(ctx context.Context, id string) error
	CloseFn         func() error
}

func (m *MockStore) SaveSession(ctx context.Context, meta store.SessionMeta, gens []store.GenerationSnapshot) error {
	if m.SaveSessionFn != nil {
		return m.SaveSessionFn(ctx, meta, gens)
	}
	return nil
}

func (m *MockStore) ListSessions(ctx context.Context) ([]store.SessionMeta, error) {
	if m.ListSessionsFn != nil {
		return m.ListSessionsFn(ctx)
	}
	return []store.SessionMeta{}, nil
}

func (m *MockStore) LoadSession(ctx context.Context, id string) (store.SessionMeta, []store.GenerationSnapshot, error) {
	if m.LoadSessionFn != nil {
		return m.LoadSessionFn(ctx, id)
	}
	return store.SessionMeta{}, nil, store.ErrSessionNotFound
}

func (m *MockStore) DeleteSession(ctx context.Context, id string) error {
	if m.DeleteSessionFn != nil {
		return m.DeleteSessionFn(ctx, id)
	}
	return nil
}

func (m *MockStore) Close() error {
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Test server helpers
// ---------------------------------------------------------------------------

func defaultCfg() *config.Config {
	return &config.Config{
		Version:         "test",
		Environment:     "test",
		MaxBoardWidth:   200,
		MaxBoardHeight:  200,
		MaxSessionCells: 2_000_000,
	}
}

// newTestServer wires a Server with a MockStore and a minimal test template.
// Uses package-level access (same package) to bypass template.ParseFS dependency.
func newTestServer(t *testing.T, st store.Store) *httptest.Server {
	t.Helper()
	return newTestServerWithConfig(t, st, defaultCfg())
}

func newTestServerWithConfig(t *testing.T, st store.Store, cfg *config.Config) *httptest.Server {
	t.Helper()
	tmpl := template.Must(template.New("index.html").Parse(`<html>{{.Version}} {{.Environment}}</html>`))
	srv := &Server{
		cfg:       cfg,
		store:     st,
		games:     game.NewRegistry(),
		indexTmpl: tmpl,
	}
	ts := httptest.NewServer(srv.SetupRoutes())
	t.Cleanup(ts.Close)
	return ts
}

// do sends a request and returns the response. Body (if any) is closed by the caller.
func do(t *testing.T, ts *httptest.Server, method, path, body string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, r)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return b
}

func decodeJSON(t *testing.T, b []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatalf("decode JSON: %v\nbody: %s", err, b)
	}
}

// createGame is a test helper: POST /api/game/new and return the decoded response.
func createGame(t *testing.T, ts *httptest.Server, width, height int, cells [][2]int) game.Game {
	t.Helper()
	body := map[string]any{"width": width, "height": height}
	if cells != nil {
		body["cells"] = cells
	}
	b, _ := json.Marshal(body)
	resp := do(t, ts, http.MethodPost, "/api/game/new", string(b))
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("createGame: status %d, body: %s", resp.StatusCode, raw)
	}
	var g game.Game
	decodeJSON(t, raw, &g)
	return g
}

// ---------------------------------------------------------------------------
// Response types for decoding
// ---------------------------------------------------------------------------

type errorResponse struct {
	Error string `json:"error"`
}

type healthResponse struct {
	Status      string `json:"status"`
	Version     string `json:"version"`
	Environment string `json:"environment"`
}

type sessionListResponse struct {
	Sessions []store.SessionMeta `json:"sessions"`
}

// generationWire is the JSON wire shape for a single generation in the
// GET /api/sessions/{id} response. store.GenerationSnapshot.Cells is json:"-"
// (Firestore bytes), so the handler converts to this 2-D age matrix for output.
type generationWire struct {
	Generation int        `json:"generation"`
	Cells      [][]uint16 `json:"cells"`
}

// sessionWire is the JSON wire shape for the full GET /api/sessions/{id} response.
type sessionWire struct {
	store.SessionMeta
	Generations []generationWire `json:"generations"`
}

// ---------------------------------------------------------------------------
// Routing smoke tests — correct HTTP methods and basic status codes
// ---------------------------------------------------------------------------

func TestRouting_MethodMismatch(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	tests := []struct {
		method     string
		path       string
		wantStatus int
	}{
		// Wrong method on defined routes should return 405.
		{http.MethodGet, "/api/game/new", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/api/game/new", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/game/abc/step", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/game/abc/save", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/sessions", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/api/sessions", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/sessions/abc", http.StatusMethodNotAllowed},
		{http.MethodPost, "/health", http.StatusMethodNotAllowed},
		// Exact index route: GET / should return 200; POST / should 405.
		{http.MethodPost, "/", http.StatusMethodNotAllowed},
		// Unknown path should return 404.
		{http.MethodGet, "/api/unknown-route", http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resp := do(t, ts, tc.method, tc.path, "")
			resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("got %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GET /health
// ---------------------------------------------------------------------------

func TestHandleHealth(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	resp := do(t, ts, http.MethodGet, "/health", "")
	raw := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var h healthResponse
	decodeJSON(t, raw, &h)
	if h.Status != "ok" {
		t.Errorf("status field: got %q, want \"ok\"", h.Status)
	}
	if h.Version != "test" {
		t.Errorf("version field: got %q, want \"test\"", h.Version)
	}
	if h.Environment != "test" {
		t.Errorf("environment field: got %q, want \"test\"", h.Environment)
	}
}

// ---------------------------------------------------------------------------
// GET /{$} — index UI
// ---------------------------------------------------------------------------

func TestHandleIndex(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	resp := do(t, ts, http.MethodGet, "/", "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type: got %q, want text/html", resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(raw), "test") {
		t.Errorf("body does not contain version \"test\": %s", raw)
	}
}

// ---------------------------------------------------------------------------
// POST /api/game/new
// ---------------------------------------------------------------------------

func TestHandleNewGame_Valid(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	resp := do(t, ts, http.MethodPost, "/api/game/new", `{"width":5,"height":4,"cells":[[1,1],[2,2]]}`)
	raw := readBody(t, resp)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body: %s", resp.StatusCode, raw)
	}
	var g game.Game
	decodeJSON(t, raw, &g)

	if g.ID == "" {
		t.Error("id: empty")
	}
	if g.Width != 5 {
		t.Errorf("width: got %d, want 5", g.Width)
	}
	if g.Height != 4 {
		t.Errorf("height: got %d, want 4", g.Height)
	}
	if g.Generation != 0 {
		t.Errorf("generation: got %d, want 0", g.Generation)
	}
	if len(g.Cells) != 4 {
		t.Errorf("cells rows: got %d, want 4 (height)", len(g.Cells))
	}
	if len(g.Cells[0]) != 5 {
		t.Errorf("cells cols: got %d, want 5 (width)", len(g.Cells[0]))
	}
	// Initial live cells should have age 1.
	if g.Cells[1][1] != 1 {
		t.Errorf("cells[1][1]: got %d, want 1 (initial age)", g.Cells[1][1])
	}
	if g.Cells[2][2] != 1 {
		t.Errorf("cells[2][2]: got %d, want 1 (initial age)", g.Cells[2][2])
	}
}

func TestHandleNewGame_NoCells(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	resp := do(t, ts, http.MethodPost, "/api/game/new", `{"width":5,"height":5}`)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body: %s", resp.StatusCode, raw)
	}
	var g game.Game
	decodeJSON(t, raw, &g)
	// All cells should be 0 (dead).
	for y, row := range g.Cells {
		for x, v := range row {
			if v != 0 {
				t.Errorf("cells[%d][%d] = %d, want 0 (dead)", y, x, v)
			}
		}
	}
}

func TestHandleNewGame_InvalidDimensions(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	tests := []struct {
		name string
		body string
	}{
		{"width zero", `{"width":0,"height":5}`},
		{"height zero", `{"width":5,"height":0}`},
		{"width negative", `{"width":-1,"height":5}`},
		{"height negative", `{"width":5,"height":-1}`},
		{"width too large", `{"width":201,"height":5}`},
		{"height too large", `{"width":5,"height":201}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := do(t, ts, http.MethodPost, "/api/game/new", tc.body)
			raw := readBody(t, resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status: got %d, want 400; body: %s", resp.StatusCode, raw)
			}
			var e errorResponse
			decodeJSON(t, raw, &e)
			if e.Error == "" {
				t.Error("error field: empty")
			}
		})
	}
}

func TestHandleNewGame_OutOfBoundsCells(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	resp := do(t, ts, http.MethodPost, "/api/game/new", `{"width":5,"height":5,"cells":[[5,0]]}`)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("out-of-bounds cell: got status %d, want 400; body: %s", resp.StatusCode, raw)
	}
}

func TestHandleNewGame_MalformedJSON(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	resp := do(t, ts, http.MethodPost, "/api/game/new", `{not valid json}`)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed JSON: got status %d, want 400; body: %s", resp.StatusCode, raw)
	}
}

func TestHandleNewGame_ContentTypeJSON(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	resp := do(t, ts, http.MethodPost, "/api/game/new", `{"width":5,"height":5}`)
	resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
}

// ---------------------------------------------------------------------------
// GET /api/game/{id}
// ---------------------------------------------------------------------------

func TestHandleGetGame_Found(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	created := createGame(t, ts, 5, 5, nil)

	resp := do(t, ts, http.MethodGet, "/api/game/"+created.ID, "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, raw)
	}
	var g game.Game
	decodeJSON(t, raw, &g)
	if g.ID != created.ID {
		t.Errorf("id: got %q, want %q", g.ID, created.ID)
	}
	if g.Width != 5 || g.Height != 5 {
		t.Errorf("dims: got %dx%d, want 5x5", g.Width, g.Height)
	}
}

func TestHandleGetGame_NotFound(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	resp := do(t, ts, http.MethodGet, "/api/game/no-such-id", "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id: got status %d, want 404; body: %s", resp.StatusCode, raw)
	}
}

// ---------------------------------------------------------------------------
// POST /api/game/{id}/step
// ---------------------------------------------------------------------------

func TestHandleStepGame_AdvancesGeneration(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	created := createGame(t, ts, 5, 5, nil)

	resp := do(t, ts, http.MethodPost, "/api/game/"+created.ID+"/step", `{"steps":3}`)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, raw)
	}
	var g game.Game
	decodeJSON(t, raw, &g)
	if g.Generation != 3 {
		t.Errorf("generation after 3 steps: got %d, want 3", g.Generation)
	}
}

func TestHandleStepGame_DefaultSteps(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	created := createGame(t, ts, 5, 5, nil)

	// Empty body → default steps=1
	resp := do(t, ts, http.MethodPost, "/api/game/"+created.ID+"/step", "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, raw)
	}
	var g game.Game
	decodeJSON(t, raw, &g)
	if g.Generation != 1 {
		t.Errorf("generation after default step: got %d, want 1", g.Generation)
	}
}

func TestHandleStepGame_EmptyObjectBody_DefaultSteps(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	created := createGame(t, ts, 5, 5, nil)

	// {} body → default steps=1
	resp := do(t, ts, http.MethodPost, "/api/game/"+created.ID+"/step", `{}`)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, raw)
	}
	var g game.Game
	decodeJSON(t, raw, &g)
	if g.Generation != 1 {
		t.Errorf("generation after {} step: got %d, want 1", g.Generation)
	}
}

func TestHandleStepGame_InvalidSteps(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	created := createGame(t, ts, 5, 5, nil)
	id := created.ID

	tests := []struct {
		name string
		body string
	}{
		{"steps zero", `{"steps":0}`},
		{"steps negative", `{"steps":-1}`},
		{"steps over 500", `{"steps":501}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := do(t, ts, http.MethodPost, "/api/game/"+id+"/step", tc.body)
			raw := readBody(t, resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status: got %d, want 400; body: %s", resp.StatusCode, raw)
			}
		})
	}
}

func TestHandleStepGame_NotFound(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	resp := do(t, ts, http.MethodPost, "/api/game/no-such-id/step", `{"steps":1}`)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id: got status %d, want 404; body: %s", resp.StatusCode, raw)
	}
}

func TestHandleStepGame_ResponseShape(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	created := createGame(t, ts, 3, 3, nil)

	resp := do(t, ts, http.MethodPost, "/api/game/"+created.ID+"/step", `{"steps":1}`)
	raw := readBody(t, resp)
	var g game.Game
	decodeJSON(t, raw, &g)

	if len(g.Cells) != 3 {
		t.Errorf("cells rows: got %d, want 3", len(g.Cells))
	}
	if len(g.Cells[0]) != 3 {
		t.Errorf("cells cols: got %d, want 3", len(g.Cells[0]))
	}
}

// ---------------------------------------------------------------------------
// POST /api/game/{id}/save
// ---------------------------------------------------------------------------

func TestHandleSaveSession_Success(t *testing.T) {
	var savedMeta store.SessionMeta
	var savedGens []store.GenerationSnapshot
	ms := &MockStore{
		SaveSessionFn: func(_ context.Context, meta store.SessionMeta, gens []store.GenerationSnapshot) error {
			savedMeta = meta
			savedGens = gens
			return nil
		},
	}
	ts := newTestServer(t, ms)
	created := createGame(t, ts, 5, 4, nil)

	resp := do(t, ts, http.MethodPost, "/api/game/"+created.ID+"/save", `{"name":"test session"}`)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body: %s", resp.StatusCode, raw)
	}

	// Decode the response SessionMeta.
	var meta store.SessionMeta
	decodeJSON(t, raw, &meta)
	if meta.ID == "" {
		t.Error("response id: empty")
	}
	if meta.Name != "test session" {
		t.Errorf("response name: got %q, want \"test session\"", meta.Name)
	}
	if meta.Width != 5 {
		t.Errorf("response width: got %d, want 5", meta.Width)
	}
	if meta.Height != 4 {
		t.Errorf("response height: got %d, want 4", meta.Height)
	}
	if meta.GenerationCount < 1 {
		t.Errorf("response generation_count: got %d, want >= 1", meta.GenerationCount)
	}

	// Verify SaveSession was called with consistent data.
	if savedMeta.ID == "" {
		t.Error("SaveSession: meta.ID empty — store was not called or ID not set")
	}
	if savedMeta.Name != "test session" {
		t.Errorf("SaveSession meta.Name: got %q, want \"test session\"", savedMeta.Name)
	}
	if len(savedGens) == 0 {
		t.Error("SaveSession: no generation snapshots passed to store")
	}
}

func TestHandleSaveSession_DefaultName(t *testing.T) {
	ms := &MockStore{}
	ts := newTestServer(t, ms)
	created := createGame(t, ts, 5, 5, nil)

	// Empty name → handler sets default name.
	resp := do(t, ts, http.MethodPost, "/api/game/"+created.ID+"/save", `{}`)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body: %s", resp.StatusCode, raw)
	}
	var meta store.SessionMeta
	decodeJSON(t, raw, &meta)
	if meta.Name == "" {
		t.Error("response name: empty (expected a default name)")
	}
}

func TestHandleSaveSession_StoreError_Returns500(t *testing.T) {
	ms := &MockStore{
		SaveSessionFn: func(_ context.Context, _ store.SessionMeta, _ []store.GenerationSnapshot) error {
			return errors.New("firestore unavailable")
		},
	}
	ts := newTestServer(t, ms)
	created := createGame(t, ts, 5, 5, nil)

	resp := do(t, ts, http.MethodPost, "/api/game/"+created.ID+"/save", `{"name":"fail"}`)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("store error: got status %d, want 500; body: %s", resp.StatusCode, raw)
	}
}

func TestHandleSaveSession_NotFound(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	resp := do(t, ts, http.MethodPost, "/api/game/no-such-id/save", `{"name":"x"}`)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown game id: got status %d, want 404; body: %s", resp.StatusCode, raw)
	}
}

func TestHandleSaveSession_SizeExceeded_Returns413(t *testing.T) {
	// Set MaxSessionCells very small so a tiny board triggers the limit.
	cfg := defaultCfg()
	cfg.MaxSessionCells = 10
	ts := newTestServerWithConfig(t, &MockStore{}, cfg)

	// 5x5 board = 25 cells * 1 generation = 25 > 10 → 413.
	created := createGame(t, ts, 5, 5, nil)
	resp := do(t, ts, http.MethodPost, "/api/game/"+created.ID+"/save", `{}`)
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("size exceeded: got status %d, want 413; body: %s", resp.StatusCode, raw)
	}
}

// ---------------------------------------------------------------------------
// GET /api/sessions
// ---------------------------------------------------------------------------

func TestHandleListSessions_ReturnsList(t *testing.T) {
	now := time.Now().UTC()
	sessions := []store.SessionMeta{
		{ID: "sess-1", Name: "Alpha", Width: 10, Height: 10, GenerationCount: 5, CreatedAt: now},
		{ID: "sess-2", Name: "Beta", Width: 20, Height: 15, GenerationCount: 10, CreatedAt: now.Add(-time.Minute)},
	}
	ms := &MockStore{
		ListSessionsFn: func(_ context.Context) ([]store.SessionMeta, error) {
			return sessions, nil
		},
	}
	ts := newTestServer(t, ms)

	resp := do(t, ts, http.MethodGet, "/api/sessions", "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, raw)
	}

	// Response must be wrapped: {"sessions": [...]}
	var wrapper sessionListResponse
	decodeJSON(t, raw, &wrapper)
	if len(wrapper.Sessions) != 2 {
		t.Errorf("sessions count: got %d, want 2", len(wrapper.Sessions))
	}
	if wrapper.Sessions[0].ID != "sess-1" {
		t.Errorf("sessions[0].id: got %q, want \"sess-1\"", wrapper.Sessions[0].ID)
	}
	if wrapper.Sessions[1].ID != "sess-2" {
		t.Errorf("sessions[1].id: got %q, want \"sess-2\"", wrapper.Sessions[1].ID)
	}
}

func TestHandleListSessions_EmptyList(t *testing.T) {
	ms := &MockStore{
		ListSessionsFn: func(_ context.Context) ([]store.SessionMeta, error) {
			return []store.SessionMeta{}, nil
		},
	}
	ts := newTestServer(t, ms)

	resp := do(t, ts, http.MethodGet, "/api/sessions", "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, raw)
	}

	var wrapper sessionListResponse
	decodeJSON(t, raw, &wrapper)
	// Must return {"sessions": []} — not null.
	if wrapper.Sessions == nil {
		t.Error("sessions: got null, want empty array []")
	}
	if len(wrapper.Sessions) != 0 {
		t.Errorf("sessions count: got %d, want 0", len(wrapper.Sessions))
	}
}

func TestHandleListSessions_StoreError_Returns500(t *testing.T) {
	ms := &MockStore{
		ListSessionsFn: func(_ context.Context) ([]store.SessionMeta, error) {
			return nil, errors.New("firestore error")
		},
	}
	ts := newTestServer(t, ms)

	resp := do(t, ts, http.MethodGet, "/api/sessions", "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("store error: got status %d, want 500; body: %s", resp.StatusCode, raw)
	}
}

// Verify the wire format uses the wrapped object, not a bare array.
func TestHandleListSessions_JSONFormat_WrappedObject(t *testing.T) {
	ms := &MockStore{
		ListSessionsFn: func(_ context.Context) ([]store.SessionMeta, error) {
			return []store.SessionMeta{}, nil
		},
	}
	ts := newTestServer(t, ms)

	resp := do(t, ts, http.MethodGet, "/api/sessions", "")
	raw := readBody(t, resp)

	// Must not be a bare array (starting with '[').
	if bytes.HasPrefix(raw, []byte("[")) {
		t.Errorf("response is a bare JSON array; want wrapped object {\"sessions\": [...]}: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"sessions"`)) {
		t.Errorf("response missing \"sessions\" key: %s", raw)
	}
}

// ---------------------------------------------------------------------------
// GET /api/sessions/{id}
// ---------------------------------------------------------------------------

func TestHandleLoadSession_Found(t *testing.T) {
	now := time.Now().UTC()
	meta := store.SessionMeta{
		ID:              "sess-abc",
		Name:            "My Session",
		Width:           3,
		Height:          3,
		GenerationCount: 2,
		CreatedAt:       now,
	}
	gens := []store.GenerationSnapshot{
		{Generation: 0, Cells: make([]byte, 9)},
		{Generation: 1, Cells: make([]byte, 9)},
	}
	ms := &MockStore{
		LoadSessionFn: func(_ context.Context, id string) (store.SessionMeta, []store.GenerationSnapshot, error) {
			if id == "sess-abc" {
				return meta, gens, nil
			}
			return store.SessionMeta{}, nil, store.ErrSessionNotFound
		},
	}
	ts := newTestServer(t, ms)

	resp := do(t, ts, http.MethodGet, "/api/sessions/sess-abc", "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, raw)
	}

	var s sessionWire
	decodeJSON(t, raw, &s)
	if s.ID != "sess-abc" {
		t.Errorf("id: got %q, want \"sess-abc\"", s.ID)
	}
	if s.Name != "My Session" {
		t.Errorf("name: got %q, want \"My Session\"", s.Name)
	}
	if s.GenerationCount != 2 {
		t.Errorf("generation_count: got %d, want 2", s.GenerationCount)
	}
	if len(s.Generations) != 2 {
		t.Errorf("generations count: got %d, want 2", len(s.Generations))
	}
	// cells must appear in the wire format (handler converts []byte → [][]uint16).
	for i, g := range s.Generations {
		if g.Cells == nil {
			t.Errorf("generations[%d].cells: nil (cells must be present in wire format)", i)
		}
	}
}

func TestHandleLoadSession_NotFound(t *testing.T) {
	ms := &MockStore{
		LoadSessionFn: func(_ context.Context, id string) (store.SessionMeta, []store.GenerationSnapshot, error) {
			return store.SessionMeta{}, nil, store.ErrSessionNotFound
		},
	}
	ts := newTestServer(t, ms)

	resp := do(t, ts, http.MethodGet, "/api/sessions/no-such-session", "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("not found: got status %d, want 404; body: %s", resp.StatusCode, raw)
	}
}

func TestHandleLoadSession_StoreError_Returns500(t *testing.T) {
	ms := &MockStore{
		LoadSessionFn: func(_ context.Context, id string) (store.SessionMeta, []store.GenerationSnapshot, error) {
			return store.SessionMeta{}, nil, errors.New("internal error")
		},
	}
	ts := newTestServer(t, ms)

	resp := do(t, ts, http.MethodGet, "/api/sessions/some-id", "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("store error: got status %d, want 500; body: %s", resp.StatusCode, raw)
	}
}

func TestHandleLoadSession_GenerationsAscendingOrder(t *testing.T) {
	gens := []store.GenerationSnapshot{
		{Generation: 0, Cells: make([]byte, 4)},
		{Generation: 1, Cells: make([]byte, 4)},
		{Generation: 2, Cells: make([]byte, 4)},
	}
	ms := &MockStore{
		LoadSessionFn: func(_ context.Context, id string) (store.SessionMeta, []store.GenerationSnapshot, error) {
			return store.SessionMeta{ID: id, Width: 2, Height: 2, GenerationCount: 3}, gens, nil
		},
	}
	ts := newTestServer(t, ms)

	resp := do(t, ts, http.MethodGet, "/api/sessions/any-id", "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", resp.StatusCode, raw)
	}
	var s sessionWire
	decodeJSON(t, raw, &s)
	for i, gen := range s.Generations {
		if gen.Generation != i {
			t.Errorf("generations[%d].generation = %d, want %d (ascending order)", i, gen.Generation, i)
		}
	}
}

// ---------------------------------------------------------------------------
// DELETE /api/sessions/{id}
// ---------------------------------------------------------------------------

func TestHandleDeleteSession_Success(t *testing.T) {
	var deletedID string
	ms := &MockStore{
		DeleteSessionFn: func(_ context.Context, id string) error {
			deletedID = id
			return nil
		},
	}
	ts := newTestServer(t, ms)

	resp := do(t, ts, http.MethodDelete, "/api/sessions/sess-to-delete", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status: got %d, want 204", resp.StatusCode)
	}
	if deletedID != "sess-to-delete" {
		t.Errorf("deletedID: got %q, want \"sess-to-delete\"", deletedID)
	}
}

func TestHandleDeleteSession_NotFound(t *testing.T) {
	ms := &MockStore{
		DeleteSessionFn: func(_ context.Context, id string) error {
			return store.ErrSessionNotFound
		},
	}
	ts := newTestServer(t, ms)

	resp := do(t, ts, http.MethodDelete, "/api/sessions/missing", "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("not found: got status %d, want 404; body: %s", resp.StatusCode, raw)
	}
}

func TestHandleDeleteSession_StoreError_Returns500(t *testing.T) {
	ms := &MockStore{
		DeleteSessionFn: func(_ context.Context, id string) error {
			return errors.New("bulk delete failed")
		},
	}
	ts := newTestServer(t, ms)

	resp := do(t, ts, http.MethodDelete, "/api/sessions/some-id", "")
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("store error: got status %d, want 500; body: %s", resp.StatusCode, raw)
	}
}

func TestHandleDeleteSession_NoBody(t *testing.T) {
	ts := newTestServer(t, &MockStore{})
	// Any valid delete that succeeds should return 204 with no body.
	resp := do(t, ts, http.MethodDelete, "/api/sessions/any", "")
	b := readBody(t, resp)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status: got %d, want 204", resp.StatusCode)
	}
	if len(b) != 0 {
		t.Errorf("204 response body: got %q, want empty", b)
	}
}
