# GitHub Tracker Implementation Plan

> **For agentic workers:** Implement task-by-task. Each task ends with a focused git commit. Steps use checkbox syntax for tracking.

**Goal:** Ship a production-shaped GitHub repo tracker API + worker per `docs/superpowers/specs/2026-07-19-github-tracker-design.md`.

**Architecture:** Gin API and RabbitMQ worker as separate binaries sharing `internal/`; Postgres via Ent; Redis for cache/locks only; RabbitMQ for async refresh jobs.

**Tech Stack:** Go 1.22+, Gin, Ent, PostgreSQL, Redis, RabbitMQ (amqp091), swaggo, slog, docker-compose, Testcontainers.

## Global Constraints

- Go 1.22+; module `github.com/PavaoZornija1/github-tracker`
- Redis is cache/locks only — never the job queue
- UNIQUE `full_name`; concurrent POST → clean 409
- Worker max 5 in flight; at-least-once + idempotent job handling
- Error JSON: `{ "error": { "code", "message" } }`
- No inline imports; exhaustive switches on unions/enums
- Small, reviewable commits — not one monolith commit

---

## File structure (target)

```text
cmd/api/main.go
cmd/worker/main.go
internal/config/
internal/httpapi/          # Gin router, middleware, handlers
internal/githubclient/     # HTTP client + error classes
internal/cache/            # Redis cache + single-flight
internal/queue/            # Rabbit publisher/consumer ports + amqp impl
internal/service/          # application use cases
internal/ent/              # generated Ent
ent/schema/                # Ent schemas (source)
docs/                      # swagger + superpowers specs/plans
docker-compose.yml
Dockerfile
Makefile
AGENTS.md
CLAUDE.md
.cursor/rules/
.env.example
```

---

### Task 1: Design docs (this plan + spec)

**Commit:** `docs: add architecture design and implementation plan`

- [x] Spec + plan under `docs/superpowers/`

---

### Task 2: Go module and repository layout

**Commit:** `chore: initialize Go module and project layout`

- [x] `go mod init github.com/PavaoZornija1/github-tracker`
- [x] Scaffold empty dirs with `.gitkeep` where needed: `cmd/api`, `cmd/worker`, `internal/...`, `ent/schema`
- [x] Expand `.gitignore` (binaries, `.env`, IDE, Ent artifacts as needed)
- [x] `.env.example` with all config knobs

---

### Task 3: Local infrastructure

**Commit:** `chore: add docker-compose for Postgres, Redis, and RabbitMQ`

- [x] `docker-compose.yml`: postgres, redis, rabbitmq:management, volumes, healthchecks
- [x] App/worker services can be added later once binaries exist (or stub placeholders)

---

### Task 4: Agent guidance files

**Commit:** `docs: add AGENTS.md, CLAUDE.md, and Cursor rules`

- [x] `AGENTS.md` — stack, invariants, validate-AI checklist
- [x] `CLAUDE.md` — aligned short copy of north star
- [x] `.cursor/rules/` — go-service, github-cache, worker-rabbit (concise)

---

### Task 5: Config + runnable stubs

**Commit:** `feat: add config loading and api/worker entrypoint stubs`

- [x] `internal/config` from env
- [x] `cmd/api` and `cmd/worker` start, log config (redacted), wait on SIGTERM
- [x] `Makefile` targets: `run-api`, `run-worker`, `compose-up`, `test`, `swag`
- [x] README: how to run Compose + binaries

---

### Task 6: Ent schemas + generate

**Commit:** `feat: add Ent schemas for repositories and refresh batches`

- [x] Schemas for Repository, RefreshBatch, RefreshBatchJob
- [x] `go generate` / ent generate
- [x] Migration strategy documented (Ent migrate on startup for dev is OK; note trade-off)
- [x] Schema smoke test (unique batch+repo job constraint)

---

### Task 7: Shared HTTP/error/logging primitives

**Commit:** `feat: add error envelope, request-id middleware, and slog helpers`

- [x] Standard error type + Gin renderer
- [x] Request ID middleware + context helpers
- [x] Job ID context helpers for worker

---

### Task 8: GitHub client + Redis cache

**Commit:** `feat: add GitHub client with Redis cache and single-flight lock`

- [x] Typed error classes
- [x] Timeout + context
- [x] Cache TTL 5m + SET NX lock
- [x] Unit tests for error mapping; single-flight test with mock transport

---

### Task 9: Repository service + CRUD API

**Commit:** `feat: implement repo CRUD API with concurrent-safe create`

- [x] Services + Gin routes for CRUD, sync refresh, stats, changes, list cursor
- [x] Integration test: concurrent POST → 409
- [x] Wire `cmd/api` with Postgres, Redis cache, graceful HTTP shutdown

---

### Task 10: RabbitMQ queue + batch refresh

**Commit:** `feat: add RabbitMQ refresh queue and batch endpoints`

- [x] Declare topology (queue, DLX, DLQ)
- [x] refresh-all → 202 + batch_id
- [x] GET batch status
- [x] Worker consumer concurrency 5, idempotent processing, retries

---

### Task 11: Swagger

**Commit:** `feat: add OpenAPI via swaggo and Swagger UI`

- [x] Annotations on handlers
- [x] Generated docs + `/swagger/*`

---

### Task 12: Harden tests + README trade-offs

**Commits:** `test: cover batch status aggregation and changes cursor` · README/docs polish

- [x] Remaining tests from design (batch status aggregation, changes cursor no-gap, GitHub error mapping)
- [x] README: runbook, trade-offs, AI workflow

---

## Progress

Tasks 1–12 complete.
