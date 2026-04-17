# conway-life

A colorful web implementation of **Conway's Game of Life** — Go HTTP API, embedded Web UI, Firestore-backed session save/replay — deployed to Google Cloud Run.

> **For developers:** read [ARCHITECTURE.md](ARCHITECTURE.md) first. It is the authoritative spec for every endpoint, struct, Terraform resource, and design decision.

---

## What It Does

- Play Conway's Game of Life in your browser with an age-colored grid (live cells cycle through the HSL wheel as they get older).
- Paint the initial board by clicking cells, then hit **Start** to watch the simulation.
- **Save** a running simulation to Firestore with its full generation history.
- **Replay** any saved session by scrubbing through its generations.
- **List** and **delete** saved sessions.

Hard board edges (no toroidal wrap) — gliders die at the boundary.

---

## Live URLs

| Environment | URL |
|-------------|-----|
| Staging | https://conway-life.stage.demo.devops-for-hire.com |
| Production | https://conway-life.demo.devops-for-hire.com |
| Health (staging) | https://conway-life.stage.demo.devops-for-hire.com/health |
| Health (production) | https://conway-life.demo.devops-for-hire.com/health |

---

## Local Development

### Prerequisites

- Go `1.22+`
- (Optional) `gcloud` CLI authenticated, with access to `dfh-stage-id`, if you want Firestore to work locally.

### Run Locally Without Firestore

```bash
cd service
go run .
```

- Server starts on `http://localhost:8080`.
- Without `GCP_PROJECT_ID`, the **save/list/load/delete** endpoints return `503 Service Unavailable` — game play still works fully in memory.

### Run Locally With Firestore (Staging DB)

```bash
gcloud auth application-default login
cd service
export GCP_PROJECT_ID=dfh-stage-id
export FIRESTORE_DATABASE_NAME=conway-life
export ENVIRONMENT=local
go run .
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP port |
| `APP_VERSION` | `dev` | Displayed in UI and `/health` |
| `ENVIRONMENT` | `local` | `local` / `staging` / `production` |
| `GCP_PROJECT_ID` | _(empty)_ | If empty, Firestore is disabled |
| `FIRESTORE_DATABASE_NAME` | `(default)` | Firestore DB name (set to `conway-life` in deployed envs) |
| `MAX_BOARD_WIDTH` | `200` | Input validation upper bound |
| `MAX_BOARD_HEIGHT` | `200` | Input validation upper bound |
| `MAX_SESSION_CELLS` | `2000000` | Reject save if `w*h*gens` exceeds this |

### Run Tests

```bash
cd service
go test ./...
go vet ./...
golangci-lint run ./...
```

All three commands MUST pass before you commit — CI runs identical checks.

---

## API Reference (summary)

Full request/response examples in [ARCHITECTURE.md §3](ARCHITECTURE.md#3-api-endpoint-specification).

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/{$}` | Serve the Web UI |
| `GET` | `/health` | Health check |
| `POST` | `/api/game/new` | Create new game (width, height, optional initial cells) |
| `GET` | `/api/game/{id}` | Current game state |
| `POST` | `/api/game/{id}/step` | Advance N generations (default 1) |
| `POST` | `/api/game/{id}/save` | Persist game + history to Firestore |
| `GET` | `/api/sessions` | List saved sessions |
| `GET` | `/api/sessions/{id}` | Load session with full history |
| `DELETE` | `/api/sessions/{id}` | Delete session and its history |

Error responses use the shape `{"error": "..."}`.

---

## Deployment

Deployments are automated via GitHub Actions. Never deploy manually.

| Action | Environment | Trigger |
|--------|-------------|---------|
| Push to `stage` | Staging (`dfh-stage-id`) | `git push origin stage` |
| Push to `main` | Production (`dfh-prod-id`) | `git push origin main` |
| Pull request to `stage` or `main` | Tests only | — |

Typical flow:

```bash
# 1. Merge feature → stage, watch staging deploy, smoke test
git checkout stage
git merge my-feature
git push origin stage

# 2. Once staging is green, promote to production
git checkout main
git merge stage
git push origin main
```

### CI/CD Pipeline Stages

1. **Test** (runs on all pushes and PRs): `go test ./...`, `go vet ./...`, `golangci-lint`.
2. **Build-and-deploy** (pushes to `stage` or `main` only):
   1. Determine env from branch.
   2. Authenticate to GCP.
   3. **Pre-deploy resource check** — fails early if GCP resources conflict with Terraform state.
   4. Compute version `v<MAJOR.MINOR>.<commit_count>`.
   5. Build Docker image → push to `gcr.io/<project>/conway-life:<tag>`.
   6. `terraform init && terraform plan && terraform apply` against the env's state.
   7. Smoke test `curl -f $SERVICE_URL/health` with retries.

Required GitHub secrets: `GCP_STAGE_SA_KEY`, `GCP_PROD_SA_KEY`.

---

## Manual One-Time Setup

These steps are required **once per environment**, only on first deploy:

### 1. Verify Domain Ownership

The deploy SAs (`gcp-cloudrun-deploy@<project>.iam.gserviceaccount.com`) must be owners of `demo.devops-for-hire.com` in Google Search Console. Confirm with:

```bash
gcloud domains list-user-verified --account=gcp-cloudrun-deploy@dfh-stage-id.iam.gserviceaccount.com
```

### 2. Create Cloud Run Domain Mapping

Run once per environment after the first successful deploy:

```bash
# Staging
gcloud beta run domain-mappings create \
  --service=conway-life-stage \
  --domain=conway-life.stage.demo.devops-for-hire.com \
  --region=us-central1 \
  --project=dfh-stage-id

# Production
gcloud beta run domain-mappings create \
  --service=conway-life-prod \
  --domain=conway-life.demo.devops-for-hire.com \
  --region=us-central1 \
  --project=dfh-prod-id
```

Terraform creates the Cloud DNS CNAME → `ghs.googlehosted.com`; Google automatically provisions the managed SSL certificate once the mapping is in place.

### 3. GitHub Repo and Secrets

```bash
gh repo create <org>/conway-life --private --source=. --push
gh secret set GCP_STAGE_SA_KEY < /path/to/stage-key.json
gh secret set GCP_PROD_SA_KEY  < /path/to/prod-key.json
```

---

## Versioning

- `VERSION` file at repo root contains `MAJOR.MINOR` (e.g., `1.0`).
- CI appends commit count to produce the patch: `1.0.42`.
- Docker images are tagged `v1.0.42` and `latest`.
- Bump `VERSION` for feature or breaking changes.

---

## Troubleshooting

### Web UI loads but shows "Loading…" forever

The UI calls `/api/game/new` on first render. Open the browser dev console:

- `503 Service Unavailable` on save endpoints — `GCP_PROJECT_ID` is not set (running locally without Firestore). Game play still works; only save/load is disabled.
- `500` on any endpoint — check Cloud Run logs: `gcloud logging read 'resource.type=cloud_run_revision AND resource.labels.service_name=conway-life-stage' --project=dfh-stage-id --limit=50`.

### Deploy fails at `terraform apply` with "resource already exists"

The pre-deploy resource check should catch this. If a GCP resource exists but is not in Terraform state, either:

- Import it: `terraform import <addr> <id>` (Terraform 1.6+ supports variables in `import` blocks).
- Or delete the orphan resource and re-run the pipeline.

### Smoke test fails after deploy

Cold-start retry is built in (5 × 5s). If it still fails:

1. Check the service URL returned by Terraform — Cloud Run sometimes takes an extra 30 seconds to begin serving on a brand-new revision.
2. `gcloud run services describe conway-life-stage --region=us-central1 --project=dfh-stage-id` — confirm the revision is `READY`.
3. Check logs with the command above.

### Firestore writes fail with `PERMISSION_DENIED`

The runtime SA needs `roles/datastore.user`. Terraform grants this — confirm with:

```bash
gcloud projects get-iam-policy dfh-stage-id \
  --flatten=bindings \
  --filter="bindings.members:conway-life-staging@dfh-stage-id.iam.gserviceaccount.com"
```

### DNS not resolving

- Terraform creates the CNAME in `dfh-ops-id` zone. Verify: `dig conway-life.stage.demo.devops-for-hire.com CNAME`.
- Domain mapping must be created manually (see §Manual One-Time Setup).
- SSL provisioning takes ~15 minutes after the first successful mapping.

---

## Project Structure

```
service/                → Go application
service/internal/       → Private packages: config, game, server, store, templates
service/Dockerfile      → Multi-stage Alpine build
terraform/              → Shared Terraform modules
terraform/stage/        → Staging tfvars + backend
terraform/prod/         → Production tfvars + backend
.github/workflows/      → CI/CD pipeline
VERSION                 → MAJOR.MINOR
ARCHITECTURE.md         → Full architecture reference
```

For everything else — endpoint schemas, Store interface, Terraform resources, design decisions — see [ARCHITECTURE.md](ARCHITECTURE.md).
