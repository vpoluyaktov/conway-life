# conway-life Architecture

> **Authoritative architecture reference for the `conway-life` service.** All team members (Backend, Frontend, DevOps, QA) must read this file before writing any code. Every endpoint, struct, route pattern, and Terraform resource is specified below.

---

## 1. Overview

`conway-life` is a web application that implements **Conway's Game of Life** with a colorful interactive Web UI, a Go HTTP JSON API, and Firestore-backed session persistence that supports replay of previously-saved simulations.

### Features

- **Interactive grid** — users can click cells in "setup mode" to paint the initial pattern; randomize or clear the board with one click.
- **Simulation controls** — Start/Stop (auto-step), Step (advance one generation), Clear, Random fill, and Speed slider (steps per second).
- **Age-based cell coloring** — each live cell has an integer `age` (generations it has been continuously alive). The UI colors cells on an HSL gradient that cycles with age, producing a colorful, animated appearance.
- **Save / List / Load / Delete** sessions to Firestore, including the **full generation history** for replay.
- **Replay mode** — scrub through saved generations generation-by-generation.

### Technology Stack

- **Language:** Go 1.22+ (uses Go 1.22 `http.ServeMux` routing patterns).
- **Persistence:** Google Cloud Firestore (Native mode).
- **UI:** Embedded Go `html/template` with inline CSS/JavaScript (no separate frontend build).
- **Deployment:** Google Cloud Run (Cloud Run template — see `rules/deployment-templates.md`).
- **Container registry:** GCR (`gcr.io/<project>/conway-life`).
- **IaC:** Terraform `>= 1.6`, `hashicorp/google ~> 5.0`.
- **CI/CD:** GitHub Actions (branch-based: `stage` → staging, `main` → production).

### Design Goals

1. Self-contained binary — single Go process serves both UI and API; no external web server needed.
2. Colorful, responsive UI that works well on desktop and mobile.
3. Replay fidelity — saved sessions preserve every intermediate generation exactly.
4. Stateless Cloud Run instances — in-memory games are per-instance; persistent storage lives in Firestore.
5. Clean `Store` interface enabling mock-based unit tests for all handlers.

---

## 2. Directory and File Structure

```
conway-life/
├── VERSION                              # "1.0" — MAJOR.MINOR (patch = commit count from CI)
├── ARCHITECTURE.md                      # This file
├── README.md                            # Setup, usage, deployment
├── .gitignore
│
├── service/                             # Go application
│   ├── main.go                          # Entrypoint: load config → store → server → listen → graceful shutdown
│   ├── go.mod                           # module conway-life; go 1.22
│   ├── go.sum
│   ├── Dockerfile                       # Multi-stage: golang:1.22-alpine → alpine:latest
│   ├── internal/
│   │   ├── config/
│   │   │   ├── config.go                # Config struct; Load() reads env vars
│   │   │   └── config_test.go           # Defaults + env var override tests
│   │   ├── game/
│   │   │   ├── game.go                  # Game struct + Step() + Randomize() + NewGame()
│   │   │   └── game_test.go             # Rule tests: still life, oscillator, glider, edge cases
│   │   ├── server/
│   │   │   ├── server.go                # Server struct + New() + SetupRoutes()
│   │   │   ├── handlers.go              # All HTTP handlers
│   │   │   ├── handlers_test.go         # Table-driven handler tests with MockStore
│   │   │   └── middleware.go            # Request logging (optional) + content-type helpers
│   │   └── store/
│   │       ├── store.go                 # Store interface + FirestoreStore implementation
│   │       └── store_test.go            # (integration tests — skipped in unit CI)
│   └── templates/
│       ├── templates.go                 # //go:embed index.html — embed.FS
│       └── index.html                   # Full UI: HTML + inline CSS + inline JS
│
├── terraform/                           # Shared modules
│   ├── main.tf                          # Google provider config
│   ├── versions.tf                      # terraform >= 1.6; google ~> 5.0
│   ├── variables.tf                     # All input variables
│   ├── outputs.tf                       # service_url, custom_domain_url, runtime SA email, firestore db name
│   ├── apis.tf                          # google_project_service for required APIs
│   ├── cloud-run.tf                     # google_cloud_run_v2_service + public IAM
│   ├── iam.tf                           # Runtime SA (conway-life-<env>) + role bindings
│   ├── firestore.tf                     # google_firestore_database (no seed — games are user-created)
│   ├── dns.tf                           # google_dns_record_set CNAME in dfh-ops-id
│   ├── stage/
│   │   ├── backend.tf                   # bucket=dfh-stage-tfstate; prefix=conway-life/state
│   │   └── stage.tfvars                 # project_id=dfh-stage-id, service_name=conway-life-stage
│   └── prod/
│       ├── backend.tf                   # bucket=dfh-prod-tfstate; prefix=conway-life/state
│       └── prod.tfvars                  # project_id=dfh-prod-id, service_name=conway-life-prod
│
└── .github/
    └── workflows/
        └── main.yml                     # test → pre-deploy check → build → push → terraform apply → smoke test
```

### Go Module

- **Module path:** `conway-life`
- **Go version:** `1.22`
- **Dependencies:** `cloud.google.com/go/firestore`, `google.golang.org/api`, `github.com/google/uuid`. Standard library for everything else.

---

## 3. API Endpoint Specification

All endpoints return `application/json` unless otherwise specified. Error responses use the shape:

```json
{"error": "human-readable message"}
```

### 3.1 `GET /{$}` — Serve UI

- **Pattern:** `GET /{$}` (Go 1.22 exact-match — does NOT catch non-existent paths, so method-mismatch 405s from API routes are preserved).
- **Response:** `text/html; charset=utf-8` — renders the embedded `index.html` with template data:

```go
type indexData struct {
    Version     string
    Environment string
}
```

- **Status codes:** `200 OK`.

### 3.2 `GET /health` — Health Check

**Response body:**
```json
{"status": "ok", "version": "1.0.42", "environment": "staging"}
```

- **Status codes:** `200 OK`.

### 3.3 `POST /api/game/new` — Create a New Game

Creates a new in-memory game and returns its full state. Optionally accepts an initial set of live cells. Games are stored per-instance in memory under a UUIDv4 identifier.

**Request:**
```json
{
  "width": 40,
  "height": 30,
  "cells": [[5, 5], [5, 6], [5, 7]]
}
```

Fields:
| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `width` | int | yes | 1 ≤ width ≤ 200 |
| `height` | int | yes | 1 ≤ height ≤ 200 |
| `cells` | `[][2]int` | no | List of `[x, y]` live-cell coordinates. `x` is column (0 ≤ x < width), `y` is row (0 ≤ y < height). Duplicates are ignored. Out-of-bounds entries return `400 Bad Request`. If omitted or `null`, the board is all-dead. |

**Response** (`201 Created`):
```json
{
  "id": "1c5f02c9-3b8b-4b3b-8f9a-3a0b1b5a9a0e",
  "width": 40,
  "height": 30,
  "generation": 0,
  "cells": [
    [0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],
    "...",
    [0,0,0,0,0,1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]
  ],
  "created_at": "2026-04-17T14:00:00Z",
  "updated_at": "2026-04-17T14:00:00Z"
}
```

- `cells` is a 2-D matrix of **ages**. `0` means dead; any positive integer `n` means the cell has been alive for `n` consecutive generations (age). Initial generation — all living cells have age `1`.
- Outer index is the **row** (`y`); inner index is the **column** (`x`). Dimensions: `len(cells) == height`, `len(cells[0]) == width`.

**Status codes:** `201 Created`, `400 Bad Request` (bad dimensions, out-of-bounds cell, malformed JSON).

### 3.4 `GET /api/game/{id}` — Get Current Game State

**Response** (`200 OK`): same shape as `POST /api/game/new` response.

**Status codes:** `200 OK`, `404 Not Found` (unknown game id).

### 3.5 `POST /api/game/{id}/step` — Advance N Generations

Advances the game by `steps` generations (default `1`).

**Request** (optional body):
```json
{"steps": 1}
```
- `steps`: int, optional, 1 ≤ steps ≤ 500. Default `1` when body absent or `{}`.

**Response** (`200 OK`): updated game state (same shape as §3.3). `generation` is incremented by `steps`.

**Status codes:** `200 OK`, `400 Bad Request` (invalid `steps`), `404 Not Found`.

### 3.6 `POST /api/game/{id}/save` — Save Session to Firestore

Persists the entire **history** of the current in-memory game to Firestore as a replayable session. History is captured internally: every time `POST /api/game/{id}/step` runs, the prior state is pushed into a per-game history ring. On save, the full history plus the current state are written atomically.

**Request:**
```json
{"name": "Pulsar at generation 37"}
```
- `name`: string, optional, 1–100 chars (trimmed). If empty/omitted, default is `"Game <id-prefix> @ gen <n>"`.

**Response** (`201 Created`):
```json
{
  "id": "8c3a9f90-4a9b-4e2c-9c6e-1e5b1e3c9f01",
  "name": "Pulsar at generation 37",
  "width": 40,
  "height": 30,
  "generation_count": 38,
  "created_at": "2026-04-17T14:05:33Z"
}
```

- `id` is the **session id** (different from the game id). It is a new UUIDv4 minted on save.
- `generation_count` = number of snapshots stored (initial board + each step = `current_generation + 1`).

**Status codes:** `201 Created`, `404 Not Found` (unknown game id), `413 Payload Too Large` (if `width * height * generation_count > 2_000_000` cells — prevents Firestore quota blow-up), `500 Internal Server Error` (Firestore write failure).

### 3.7 `GET /api/sessions` — List Saved Sessions

**Response** (`200 OK`):
```json
{
  "sessions": [
    {
      "id": "8c3a9f90-4a9b-4e2c-9c6e-1e5b1e3c9f01",
      "name": "Pulsar at generation 37",
      "width": 40,
      "height": 30,
      "generation_count": 38,
      "created_at": "2026-04-17T14:05:33Z"
    }
  ]
}
```

- Sessions are ordered by `created_at DESC`.
- Empty list when none exist: `{"sessions": []}` (never `null`).

**Status codes:** `200 OK`, `500 Internal Server Error`.

### 3.8 `GET /api/sessions/{id}` — Load Session (Full History)

Returns the session metadata plus every generation for replay.

**Response** (`200 OK`):
```json
{
  "id": "8c3a9f90-4a9b-4e2c-9c6e-1e5b1e3c9f01",
  "name": "Pulsar at generation 37",
  "width": 40,
  "height": 30,
  "generation_count": 38,
  "created_at": "2026-04-17T14:05:33Z",
  "generations": [
    {"generation": 0, "cells": [[0,0,0],[0,1,0],[0,0,0]]},
    {"generation": 1, "cells": [[0,0,0],[0,0,0],[0,0,0]]}
  ]
}
```

- `generations` is ordered ascending by `generation`. Exactly `generation_count` entries.
- `cells` format identical to §3.3.

**Status codes:** `200 OK`, `404 Not Found`, `500 Internal Server Error`.

### 3.9 `DELETE /api/sessions/{id}` — Delete a Saved Session

Deletes the session metadata document **and all its `generations` subcollection documents** using Firestore **`BulkWriter`**.

**Response** (`204 No Content`): empty body.

**Status codes:** `204 No Content`, `404 Not Found`, `500 Internal Server Error`.

### 3.10 Route Registration Table

| Method | Path | Handler | Notes |
|--------|------|---------|-------|
| `GET`  | `/{$}` | `handleIndex` | Exact match (see §6) |
| `GET`  | `/health` | `handleHealth` | |
| `POST` | `/api/game/new` | `handleNewGame` | |
| `GET`  | `/api/game/{id}` | `handleGetGame` | |
| `POST` | `/api/game/{id}/step` | `handleStepGame` | |
| `POST` | `/api/game/{id}/save` | `handleSaveSession` | |
| `GET`  | `/api/sessions` | `handleListSessions` | |
| `GET`  | `/api/sessions/{id}` | `handleLoadSession` | |
| `DELETE` | `/api/sessions/{id}` | `handleDeleteSession` | |

---

## 4. Data Models

### 4.1 Board Representation

Cells are stored internally as `[][]uint16` — outer slice is rows (`y` = `0..height-1`), inner slice is columns (`x` = `0..width-1`). Value semantics:

- `0` — cell is dead.
- `n > 0` — cell is alive and has age `n` generations.
- Age is capped at `65535` (`uint16` max); cells older than that remain at the cap. This is safe — the UI already cycles color hue with a period of 60 generations, so the cap is purely defensive.

### 4.2 Go Structs

```go
// internal/game/game.go
type Game struct {
    ID         string      `json:"id"`
    Width      int         `json:"width"`
    Height     int         `json:"height"`
    Generation int         `json:"generation"`
    Cells      [][]uint16  `json:"cells"`
    CreatedAt  time.Time   `json:"created_at"`
    UpdatedAt  time.Time   `json:"updated_at"`

    // Unexported: full history for save. history[0] is the initial state; history[n]
    // is the state at generation n. Entries append on each Step call.
    history    [][]uint16Grid // [][]byte-like flattened snapshots (see §4.4)
}
```

```go
// internal/store/store.go (session models)
type SessionMeta struct {
    ID              string    `json:"id"              firestore:"id"`
    Name            string    `json:"name"            firestore:"name"`
    Width           int       `json:"width"           firestore:"width"`
    Height          int       `json:"height"          firestore:"height"`
    GenerationCount int       `json:"generation_count" firestore:"generation_count"`
    CreatedAt       time.Time `json:"created_at"      firestore:"created_at,serverTimestamp"`
}

type GenerationSnapshot struct {
    Generation int    `json:"generation" firestore:"generation"`
    // Firestore: flattened bytes (len == width*height). Wire-format: 2-D matrix of ints.
    // Conversion happens in the store layer.
    Cells      []byte `json:"-"          firestore:"cells"`
}

type Session struct {
    SessionMeta
    Generations []GenerationSnapshot `json:"generations"`
}
```

### 4.3 Firestore Data Layout

| Path | Purpose | Document fields |
|------|---------|-----------------|
| `sessions/{sessionId}` | Session metadata | `id`, `name`, `width`, `height`, `generation_count`, `created_at` |
| `sessions/{sessionId}/generations/{generation}` | One doc per generation snapshot. Document ID is the generation number left-padded to 6 digits (e.g. `000000`, `000001`) to preserve lexicographic = numeric ordering. | `generation` (int), `cells` (bytes) |

**Document ID strategy:**
- Session id — `uuid.NewString()` (UUIDv4).
- Generation doc id — `fmt.Sprintf("%06d", generation)`; caps at `999999` (enforced by the 500-step-per-call and size-limit rules).

**Indexes:**
- Composite index on `sessions` collection: `created_at DESC` (single-field; no composite index file needed — Firestore auto-indexes single fields).
- No additional indexes required.

### 4.4 Seed Data

**None.** Unlike the template, this application does not seed Firestore — all sessions are user-created. `terraform/firestore.tf` creates the `google_firestore_database` but **no `google_firestore_document` seeds**.

---

## 5. Store Interface

All persistence lives behind a single Go interface for dependency injection and easy mocking in tests.

```go
// internal/store/store.go

type Store interface {
    // SaveSession writes session metadata and all generation snapshots atomically.
    // Uses BulkWriter for the generation documents.
    SaveSession(ctx context.Context, meta SessionMeta, gens []GenerationSnapshot) error

    // ListSessions returns all session metadata, sorted by created_at DESC.
    // Returns an empty (non-nil) slice when no sessions exist.
    ListSessions(ctx context.Context) ([]SessionMeta, error)

    // LoadSession returns the metadata and every generation snapshot in ascending order.
    // Returns ErrSessionNotFound if the id does not exist.
    LoadSession(ctx context.Context, id string) (SessionMeta, []GenerationSnapshot, error)

    // DeleteSession removes the session and all its generation snapshots.
    // Uses BulkWriter for the bulk delete. Returns ErrSessionNotFound if missing.
    DeleteSession(ctx context.Context, id string) error

    // Close releases resources (Firestore client).
    Close() error
}

var ErrSessionNotFound = errors.New("session not found")
```

### Implementation Notes

- **`FirestoreStore` is the only implementation.** Constructed with `NewFirestoreStore(ctx, projectID, databaseName)`. Uses `firestore.NewClientWithDatabase()` (not the default database).
- **`SaveSession`** — write the metadata document first; then use `client.BulkWriter(ctx)` to enqueue all generation documents and call `bw.End()` to flush. On any per-write error, return the first error encountered.
- **`DeleteSession`** — fetch all documents in `sessions/{id}/generations` (via `DocumentRefs`), enqueue each into `BulkWriter`, then delete the parent `sessions/{id}` document. Call `bw.End()` to flush.
- **MANDATORY:** all bulk write/delete paths MUST use Firestore **`BulkWriter`** — never the deprecated `Batch()`. This is enforced by the architecture standards.
- **Tests:** Handlers are tested with a `MockStore` that implements the same interface. `MockStore` lives in `handlers_test.go` (same package). Do not import `testify/mock`; use a hand-written mock with configurable function fields.

---

## 6. Server and Routing Design

### 6.1 Server Struct and Lifecycle

```go
// internal/server/server.go
type Server struct {
    cfg       *config.Config
    store     store.Store            // Firestore-backed or MockStore (tests)
    games     *game.Registry         // in-memory map[string]*Game with sync.Mutex
    indexTmpl *template.Template
}

func New(cfg *config.Config, st store.Store) *Server {
    tmpl := template.Must(template.ParseFS(templates.FS, "index.html"))
    return &Server{
        cfg:       cfg,
        store:     st,
        games:     game.NewRegistry(),
        indexTmpl: tmpl,
    }
}

func (s *Server) SetupRoutes() *http.ServeMux {
    mux := http.NewServeMux()
    // Health and UI
    mux.HandleFunc("GET /health", s.handleHealth)
    mux.HandleFunc("GET /{$}", s.handleIndex) // EXACT match — see §6.2

    // Games (in-memory)
    mux.HandleFunc("POST /api/game/new", s.handleNewGame)
    mux.HandleFunc("GET /api/game/{id}", s.handleGetGame)
    mux.HandleFunc("POST /api/game/{id}/step", s.handleStepGame)
    mux.HandleFunc("POST /api/game/{id}/save", s.handleSaveSession)

    // Sessions (Firestore)
    mux.HandleFunc("GET /api/sessions", s.handleListSessions)
    mux.HandleFunc("GET /api/sessions/{id}", s.handleLoadSession)
    mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)

    return mux
}
```

### 6.2 Why the Index Route MUST Use `GET /{$}`

Go 1.22 `http.ServeMux` treats the pattern `"/"` as a **catch-all prefix** — it matches every request path that no other route matches, for every method. If the index is registered as `"/"`, then a `PUT /api/sessions/foo` hits the index handler instead of returning `405 Method Not Allowed`, because the index's catch-all shadows the method-specific mismatch.

`GET /{$}` is an **exact match for the root `/` only** — requests to other paths fall through to the mux's default `404 Not Found`, and method mismatches on explicitly-registered paths correctly return `405`. This is the required pattern.

### 6.3 Request Handlers — Behavior Summary

| Handler | Key logic |
|---------|-----------|
| `handleIndex` | `indexTmpl.Execute(w, indexData{Version, Environment})` |
| `handleHealth` | JSON `{status, version, environment}` |
| `handleNewGame` | Parse & validate body → `game.NewGame(width, height, cells)` → `s.games.Put(g)` → 201 + JSON |
| `handleGetGame` | `id := r.PathValue("id")` → `s.games.Get(id)` → 200 + JSON or 404 |
| `handleStepGame` | Lookup game; parse optional `steps`; loop `g.Step()`; return updated game |
| `handleSaveSession` | Build `SessionMeta` + `[]GenerationSnapshot` from `g.history`; size check; `s.store.SaveSession(...)` |
| `handleListSessions` | `s.store.ListSessions(...)` → wrap in `{"sessions": [...]}` |
| `handleLoadSession` | `s.store.LoadSession(...)` → build combined JSON with generations in ascending order |
| `handleDeleteSession` | `s.store.DeleteSession(...)` → `204` or map `ErrSessionNotFound` → `404` |

All handlers:
- Set `Content-Type: application/json; charset=utf-8` before writing the body (except `handleIndex` which sets `text/html`).
- Write status code **before** the body (`w.WriteHeader(status)` → `json.NewEncoder(w).Encode(payload)`).
- On a JSON decode error, return `400 Bad Request` with `{"error": "invalid JSON: <cause>"}`.

### 6.4 Static Files

There are none. All CSS and JS are inlined in `index.html`. The binary is fully self-contained via `//go:embed`.

### 6.5 In-Memory Game Registry

```go
// internal/game/registry.go
type Registry struct {
    mu    sync.Mutex
    games map[string]*Game
}
func NewRegistry() *Registry { return &Registry{games: map[string]*Game{}} }
func (r *Registry) Put(g *Game) { ... }
func (r *Registry) Get(id string) (*Game, bool) { ... }
```

- Games are kept in memory only. A Cloud Run instance restart drops them. Users must save to Firestore to persist.
- **Per-game mutex**: each `Game` holds its own `sync.Mutex` — concurrent `step` calls on the same game ID serialize correctly.
- **No eviction policy required** for v1; instances are short-lived. If memory pressure becomes a concern, add an LRU later.

---

## 7. Configuration

Loaded from environment variables. Same pattern as the template (`config.Load() *Config`, no external deps).

| Field | Env Var | Type | Default | Description |
|-------|---------|------|---------|-------------|
| `Port` | `PORT` | string | `"8080"` | HTTP listen port (Cloud Run sets this to `8080`). |
| `Version` | `APP_VERSION` | string | `"dev"` | Displayed in UI and `/health`. Set by CI/CD. |
| `Environment` | `ENVIRONMENT` | string | `"local"` | `staging`, `production`, or `local`. |
| `ProjectID` | `GCP_PROJECT_ID` | string | `""` | If empty, Firestore is disabled and save/list endpoints return `503`. |
| `FirestoreDatabaseName` | `FIRESTORE_DATABASE_NAME` | string | `"(default)"` | Set to `conway-life` in deployed envs. |
| `MaxBoardWidth` | `MAX_BOARD_WIDTH` | int | `200` | Upper bound validation in `handleNewGame`. |
| `MaxBoardHeight` | `MAX_BOARD_HEIGHT` | int | `200` | Upper bound validation in `handleNewGame`. |
| `MaxSessionCells` | `MAX_SESSION_CELLS` | int | `2000000` | Rejects `POST /api/game/{id}/save` if `width*height*generation_count` exceeds this. |

### Configuration Loading Strategy

- `Load()` reads each env var with `os.Getenv`. If empty, applies the default constant.
- Integer env vars use `strconv.Atoi`; parse errors fall back to the default (log a warning).
- No config file. No config precedence logic.

---

## 8. Infrastructure and Deployment

### 8.1 GCP Environments

| Environment | GCP Project | Branch | Terraform Dir | State Bucket | State Prefix |
|-------------|------------|--------|---------------|--------------|---------------|
| Staging | `dfh-stage-id` | `stage` | `terraform/stage/` | `dfh-stage-tfstate` | `conway-life/state` |
| Production | `dfh-prod-id` | `main` | `terraform/prod/` | `dfh-prod-tfstate` | `conway-life/state` |
| DNS/Ops | `dfh-ops-id` | — | — | — (Cloud DNS zone: `demo-devops-for-hire-com`) |

### 8.2 Terraform Resources

| Resource | File | Purpose |
|----------|------|---------|
| `google_project_service` (×8) | `apis.tf` | Enables: `run.googleapis.com`, `firestore.googleapis.com`, `iam.googleapis.com`, `cloudresourcemanager.googleapis.com`, `artifactregistry.googleapis.com`, `containerregistry.googleapis.com`, `dns.googleapis.com`, `logging.googleapis.com`. |
| `google_cloud_run_v2_service` | `cloud-run.tf` | Cloud Run service `conway-life-<env>`; image `gcr.io/<project>/conway-life:<tag>`; port 8080; env vars `ENVIRONMENT`, `APP_VERSION`, `GCP_PROJECT_ID`, `FIRESTORE_DATABASE_NAME`. |
| `google_cloud_run_service_iam_member` | `cloud-run.tf` | `roles/run.invoker` → `allUsers` (public service). |
| `google_service_account` | `iam.tf` | Runtime SA: `conway-life-<env>@<project>.iam.gserviceaccount.com`. (Full ID ≤ 30 chars: `conway-life-staging` = 19 chars ✓.) |
| `google_project_iam_member` (×2) | `iam.tf` | Runtime SA grants: `roles/logging.logWriter`, `roles/datastore.user`. |
| `google_firestore_database` | `firestore.tf` | Name `conway-life`, type `FIRESTORE_NATIVE`, location from `firestore_location` var. **No seed documents.** |
| `google_dns_record_set` | `dns.tf` | In `dfh-ops-id` / zone `demo-devops-for-hire-com`. Type `CNAME`, target `ghs.googlehosted.com.` (note trailing dot — Cloud DNS requires FQDN). |

### 8.3 Terraform Variables

Same variable set as the Cloud Run template, with these concrete values:

`terraform/stage/stage.tfvars`:
```hcl
project_id              = "dfh-stage-id"
service_name            = "conway-life-stage"
environment             = "staging"
tfstate_bucket_name     = "dfh-stage-tfstate"
min_instances           = 0
max_instances           = 3
cpu_limit               = "1"
memory_limit            = "512Mi"
timeout                 = "30s"
dns_project_id          = "dfh-ops-id"
dns_zone_name           = "demo-devops-for-hire-com"
dns_domain              = "demo.devops-for-hire.com"
custom_domain           = "conway-life.stage.demo.devops-for-hire.com"
firestore_database_name = "conway-life"
firestore_location      = "nam5"
```

`terraform/prod/prod.tfvars` — identical except:
```hcl
project_id    = "dfh-prod-id"
service_name  = "conway-life-prod"
environment   = "production"
tfstate_bucket_name = "dfh-prod-tfstate"
custom_domain = "conway-life.demo.devops-for-hire.com"
```

### 8.4 Container Image

- **Registry:** `gcr.io/<project_id>/conway-life`
- **Tags:** `v<MAJOR.MINOR.COMMITCOUNT>` (e.g. `v1.0.42`) and `latest`
- **Dockerfile:** multi-stage.
  - Stage 1 — `golang:1.22-alpine` — `go mod download`, `CGO_ENABLED=0 GOOS=linux go build -o conway-life .`.
  - Stage 2 — `alpine:latest` — `apk add ca-certificates tzdata`, non-root user `rdapp`, `COPY --from=builder /app/conway-life .`, `EXPOSE 8080`, `HEALTHCHECK CMD wget -qO- http://localhost:8080/health || exit 1`, `CMD ["./conway-life"]`.

### 8.5 DNS Records

| Environment | Record Type | Name | Target |
|-------------|-------------|------|--------|
| Staging | `CNAME` | `conway-life.stage.demo.devops-for-hire.com.` | `ghs.googlehosted.com.` |
| Production | `CNAME` | `conway-life.demo.devops-for-hire.com.` | `ghs.googlehosted.com.` |

Cloud Run domain mapping (`gcloud beta run domain-mappings create ...`) is a **one-time manual step** documented in README.md §Manual Setup.

### 8.6 CI/CD Pipeline — `.github/workflows/main.yml`

**Triggers:** `push` to `main` or `stage`; `pull_request` to `main` or `stage`.

**Jobs (in order):**

1. **`test`** (runs on all triggers) —
   - `actions/checkout@v4`
   - `actions/setup-go@v5` with `go-version: '1.22'`
   - `actions/cache@v4` for `~/go/pkg/mod`
   - `cd service && go mod download`
   - `cd service && go test ./...`
   - `cd service && go vet ./...`
   - `golangci/golangci-lint-action@v6` with `version: latest`, `working-directory: service`

2. **`build-and-deploy`** (pushes only; `needs: test`) —
   - `actions/checkout@v4` with `fetch-depth: 0` (for commit count)
   - Determine env from branch: `main` → `ENV_NAME=prod`, `stage` → `ENV_NAME=stage`; set `PROJECT_ID` accordingly.
   - `google-github-actions/auth@v2` with the env-specific secret (`GCP_PROD_SA_KEY` or `GCP_STAGE_SA_KEY`).
   - `google-github-actions/setup-gcloud@v2`
   - `hashicorp/setup-terraform@v3` with `terraform_version: 1.6.0` — **MUST be ≥ 1.6** (1.5 does not support variables in import blocks).
   - Compute version: `VERSION=$(cat VERSION).$(git rev-list --count HEAD)`.
   - **Pre-deploy resource check (MANDATORY):** Before `terraform apply`, run a `gcloud` query to list existing Cloud Run services, service accounts, and Firestore DB. Fail with a clear message if something conflicts with Terraform state (e.g. SA exists but is not in state).
   - Docker: `gcloud auth configure-docker`, `docker build -t gcr.io/$PROJECT_ID/conway-life:v$VERSION -t gcr.io/$PROJECT_ID/conway-life:latest ./service`.
   - Docker push both tags.
   - Copy `terraform/<env>/backend.tf` → `terraform/backend.tf`; `terraform init`; `terraform plan -var-file=<env>/<env>.tfvars -var=image_tag=v$VERSION`; `terraform apply -auto-approve ...`.
   - `SERVICE_URL=$(terraform output -raw service_url)`.
   - Smoke test: `curl -f "$SERVICE_URL/health"` (retry up to 5 times with 5s backoff to tolerate cold start).
   - Summary comment to the job log: deployed URL, version, environment.

**Required GitHub Actions versions (mandatory):**

- `actions/checkout@v4`
- `actions/setup-go@v5`
- `actions/cache@v4`
- `google-github-actions/auth@v2`
- `google-github-actions/setup-gcloud@v2`
- `golangci/golangci-lint-action@v6`
- `hashicorp/setup-terraform@v3`

Do **not** use `@v3` or older versions of checkout/setup-go/cache — they fail under Node.js 24.

### 8.7 Required GitHub Secrets

| Secret | Purpose |
|--------|---------|
| `GCP_STAGE_SA_KEY` | JSON key for deploy SA in `dfh-stage-id` |
| `GCP_PROD_SA_KEY` | JSON key for deploy SA in `dfh-prod-id` |

---

## 9. Game Logic and Algorithm Edge Cases

### 9.1 Conway's Rules

For every cell, count live neighbours (8-connected Moore neighbourhood):

| Current | Neighbours | Next |
|---------|------------|------|
| alive | `< 2` or `> 3` | dead (underpopulation / overpopulation) |
| alive | `2` or `3` | alive, age increments by 1 |
| dead | `== 3` | alive, age `1` (newborn) |
| dead | other | dead |

### 9.2 Boundary Behavior — NO Wrapping

The board is **finite with hard edges**, not a torus. Cells at `x == 0`, `x == width-1`, `y == 0`, or `y == height-1` count only the in-bounds neighbours they have (3, 5, or fewer). This is documented in the UI as "hard boundaries — gliders die at the edge."

The decision to NOT use toroidal wrapping is explicit. Wrapping changes pattern dynamics (gliders wrap infinitely) and is out of scope for v1. If wrapping is later needed, add a `"boundary": "wrap" | "dead"` field to `POST /api/game/new`.

### 9.3 Step Algorithm

Computed with a double-buffer: allocate a fresh `[][]uint16` of the same dimensions, fill it based on the current grid, then swap. Do **not** mutate the grid in place — that corrupts the neighbor counts.

```
next[y][x] =
  if curr[y][x] > 0 and (n == 2 or n == 3) → curr[y][x] + 1    // age++
  if curr[y][x] == 0 and n == 3            → 1                 // birth
  otherwise                                → 0                 // death
  where n = count of alive neighbours of (x, y)
```

### 9.4 Edge Cases — Required Test Coverage

| Input | Expected behavior |
|-------|-------------------|
| Empty board (all zeros) | Step → still empty (`generation++`, `cells` unchanged). |
| Single live cell | Step → dies (0 neighbours → underpop). `cells` all zero, `generation++`. |
| 1×1 board, single live cell | Step → dies. Same as above. |
| 1×1 board, dead | Step → still dead. |
| Full board (every cell alive) | Step → all cells with neighbour count `< 2` remain dead; corner cells have 3 neighbours and survive. Verify manually in tests for a `3x3` full board → after step, only corners alive (age 2), middle and edges dead. |
| Still-life (2×2 block) | After any number of steps, the block is unchanged and each cell's age increments per step. |
| Blinker (horizontal 3-in-a-row) | Step → vertical 3-in-a-row. Step again → back to horizontal. The two outer cells die+are reborn; their ages reset to `1`. The center cell survives; its age grows monotonically. |
| Glider | After 4 steps, translates by `(+1, +1)` on an unbounded region; must work on a large-enough board to confirm. |
| Board dimensions `width=1 height=H` or `H=1 width=W` | No logic corner cases — neighbour counting already generalizes. Test a `1x5` strip to confirm no panics. |
| Request `width=0` or `height=0` or negative | `400 Bad Request` (validated in handler). |
| Initial cells out of bounds | `400 Bad Request`. |
| `steps` = 0 | `400 Bad Request` (must be ≥ 1). |
| `steps` > 500 | `400 Bad Request`. |

### 9.5 Why No Regex in the Engine

This application performs no regex operations. The "zero-length pattern verification" rule from global standards does not apply. If regex is added later (e.g. RLE pattern import), the implementer MUST write a local Go script to confirm match counts for zero-length patterns before documenting the spec.

---

## 10. Web UI Design

### 10.1 Layout

Single-page application, rendered from `index.html`:

```
┌──────────────────────────────────────────────────────┐
│              Conway's Game of Life                   │
│                [version] · [environment]             │
├──────────────────────────────────────────────────────┤
│  ╔══════════╗                                        │
│  ║          ║   Controls:                            │
│  ║          ║   [▶ Start] [⏸ Stop] [⏭ Step] [⟲ Clear]│
│  ║  GRID    ║   [🎲 Random] Speed: [====|====] 5 fps │
│  ║          ║                                        │
│  ║          ║   Board: W [40] × H [30]  [New]        │
│  ╚══════════╝   Generation: 37   Live: 142           │
│                                                      │
│  Mode: ⦿ Setup  ○ Simulate  ○ Replay                │
├──────────────────────────────────────────────────────┤
│  Sessions                                            │
│  [Save Current…]                                     │
│                                                      │
│  ▸ Pulsar at generation 37 · 40×30 · 38 gens  [Load][🗑]│
│  ▸ Glider run          · 60×40 · 120 gens    [Load][🗑]│
└──────────────────────────────────────────────────────┘
```

### 10.2 Color Scheme

**Background:** deep space gradient `linear-gradient(135deg, #0a0a1e, #1a0a2e)`.

**Dead cells:** `#151530` with a 1px darker outline `#0a0a1e`.

**Live cells — age-based HSL gradient:**

```js
function cellColor(age) {
  // Hue cycles every 60 generations; saturation fixed; lightness dips then rises.
  const hue = (age * 6) % 360;           // 6° per generation → full cycle in 60 gens
  const sat = 85;                         // vivid
  const light = 50 + Math.min(age, 20);   // young = 50%, saturates at 70% at age 20+
  return `hsl(${hue} ${sat}% ${light}%)`;
}
```

- Newborn (age 1): hot red/orange.
- Maturing (age 10–20): yellow/green.
- Older (age 30+): cyan → blue → purple → cycles back to red.

This produces a visibly colorful, dynamic grid — stable still-lifes pulse through the hue wheel; long-lived oscillators strobe; newborns always flash bright.

### 10.3 Controls — Behavior

| Control | Action |
|---------|--------|
| **Start** | Begin `setInterval(step, 1000/speed)`. Button toggles to **Stop**. |
| **Stop** | Clear the interval. |
| **Step** | Call `POST /api/game/{id}/step` with `{"steps": 1}`; repaint grid. |
| **Clear** | Locally reset `cells` to all zeros; call `POST /api/game/new` with empty `cells`. |
| **Random** | Locally randomize `cells` at ~25% density; call `POST /api/game/new` with those coords. |
| **Speed slider** | 1–30 steps per second. Changes the interval immediately when adjusted mid-run. |
| **W / H / New** | Create a new game at the given dimensions. |
| **Mode: Setup** | Click a cell to toggle alive/dead. Sends no API call until "New" is pressed. |
| **Mode: Simulate** | Grid is read-only; controls drive the simulation. |
| **Mode: Replay** | Slider to scrub through loaded session's `generations` array. No API calls during scrub. |

### 10.4 Setup Mode

- Only available when `generation === 0`.
- Click toggles the cell in the local `cells` array.
- Hitting **Start** or **Step** for the first time submits the configured cells via `POST /api/game/new`.

### 10.5 Save / Load / Replay

- **Save** — prompts for a name, calls `POST /api/game/{id}/save`, refreshes the sessions list.
- **Load** — calls `GET /api/sessions/{id}`, switches to Replay mode, sets a slider ranging `0..generation_count-1`.
- **Delete** — confirms, calls `DELETE /api/sessions/{id}`, refreshes the list.

### 10.6 Rendering Strategy

- **Canvas 2D** (`<canvas>`) for the grid. DOM-per-cell is too slow for 200×200 boards.
- Redraw on every state change. For a 200×200 grid the canvas work is negligible (< 1 ms).
- Grid cell size is `cellPx = max(4, min(24, floor(720 / max(width, height))))` — keeps the visible area ~720×720 px.

### 10.7 Responsive Design

- Desktop: grid on the left, controls on the right.
- Mobile (`max-width: 768px`): grid on top, controls stack below. Grid is `100vw` wide. Sessions panel collapses to a full-width accordion.

---

## 11. Embedded Templates

```go
// templates/templates.go
package templates

import "embed"

//go:embed index.html
var FS embed.FS
```

- Exactly one template file. No additional HTML, CSS, or JS files — all are inline inside `index.html`.
- Parsed once in `server.New()` via `template.ParseFS(templates.FS, "index.html")`.
- Template variables: `{{.Version}}`, `{{.Environment}}`.

---

## 12. Design Decisions and Rationale

1. **`GET /{$}` exact-match index route** — see §6.2. Prevents catch-all from swallowing method-mismatch 405 responses.
2. **`[][]uint16` ages, not booleans** — enables age-based UI coloring with no separate data structure. `uint16` caps at 65,535 generations (more than enough for any practical session).
3. **`BulkWriter` for Firestore bulk deletes** — required by global rule. The deprecated `Batch()` has write-count limits and was removed from the Go Firestore SDK's supported surface.
4. **Generation-per-subcollection-doc** — avoids the Firestore 1 MiB document-size limit for sessions with many generations. A 100×100 board × 500 generations × 1 byte = 5 MB total; no way that fits in one doc.
5. **Zero-padded generation document IDs** — Firestore orders by string id lexicographically. `"000001"` < `"000010"` < `"000100"` works; unpadded `"1"` < `"10"` < `"2"` does not.
6. **In-memory games, Firestore sessions** — games are ephemeral (only meaningful while the user is interacting). Persistence is an explicit user action (Save).
7. **Canvas rendering over DOM cells** — DOM rendering 40,000 `<div>`s kills any browser; canvas is an order of magnitude faster.
8. **Hard board edges, no toroidal wrap** — simpler to reason about, matches user expectation ("cells die at the edge"). Explicit design decision to defer wrap to a future `boundary` parameter.
9. **Cloud Run template** — no need for Kubernetes. Simulation is stateless per-request (the in-memory game is per-instance; users must explicitly save to persist). The app scales to zero.
10. **No seed Firestore data** — unlike the template's `greetings/hello`, sessions are user-created.
11. **Embedded HTML template** — self-contained binary (global repo convention).
12. **Store interface for DI** — lets handlers be unit-tested with `MockStore`, with no Firestore emulator or network access in CI.

---

## 13. GitHub Actions Versions (mandatory)

Per `~/.claude/rules/template-standards.md`:

| Action | Version | Reason |
|--------|---------|--------|
| `actions/checkout` | `@v4` | Node.js 24 compatible |
| `actions/setup-go` | `@v5` | Node.js 24 compatible |
| `actions/cache` | `@v4` | Node.js 24 compatible |
| `google-github-actions/auth` | `@v2` | Current stable |
| `google-github-actions/setup-gcloud` | `@v2` | Current stable |
| `golangci/golangci-lint-action` | `@v6` | Current stable |
| `hashicorp/setup-terraform` | `@v3` | Current stable |

Do **not** use `@v3` or older versions of `actions/*`. CI will fail on Node.js 24 runners.

---

## 14. Task Sequencing for Implementation

Recommended order — tasks in the same row can run in parallel:

| Phase | Tasks |
|-------|-------|
| 1 | DevOps: create GitHub repo, set secrets, scaffold `.github/workflows/main.yml` with `test` job only. |
| 2 | Backend: `config`, `game` package (pure logic), `store` interface + `FirestoreStore`, `server` handlers, `main.go`. Frontend: `templates/index.html`. QA: write `game_test.go` and `handlers_test.go` (MockStore) from spec — does NOT wait for backend. |
| 3 | DevOps: fill in Terraform (`cloud-run.tf`, `firestore.tf`, `iam.tf`, `dns.tf`, `apis.tf`, `outputs.tf`, `variables.tf`, `main.tf`, `versions.tf`, stage/prod tfvars). Extend workflow with `build-and-deploy` job + pre-deploy resource check. |
| 4 | Push `stage` branch; DevOps monitors CI; QA verifies `/health` and runs end-to-end smoke against the deployed URL. |
| 5 | Fast-forward merge `stage` → `main`; deploy to production; QA verifies. |

---

*End of ARCHITECTURE.md — contact the Architect for any gaps or ambiguities.*
