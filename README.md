# github-tracker

Production-shaped Go service that tracks GitHub repositories (Gin + Ent + Postgres + Redis cache + RabbitMQ worker).

## Status

**Done:** HTTP API (`cmd/api`), async refresh worker (`cmd/worker`), OpenAPI/Swagger UI at `/swagger/index.html`.

Design and plan:

- [Architecture design](docs/superpowers/specs/2026-07-19-github-tracker-design.md)
- [Implementation plan](docs/superpowers/plans/2026-07-19-github-tracker-implementation.md)
- Agent brief: [AGENTS.md](AGENTS.md)

## Stack

| Piece | Choice |
|-------|--------|
| HTTP | Gin |
| ORM | Ent + PostgreSQL |
| Cache / locks | Redis |
| Job queue | RabbitMQ (+ DLQ) |
| Docs | swaggo OpenAPI |
| Binaries | `cmd/api`, `cmd/worker` |

## Runbook

```bash
# 1. Infra + env
cp .env.example .env
docker compose up -d

# 2. Load env into the shell (or export vars yourself)
set -a && source .env && set +a

# 3. API + worker (separate terminals)
make run-api
make run-worker

# 4. Tests / OpenAPI regen
make test
make swag
```

Swagger UI: [http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html)

### Sample curls

```bash
curl -s localhost:8080/healthz

# Track a repo
curl -s -X POST localhost:8080/api/repos \
  -H 'content-type: application/json' \
  -d '{"owner":"golang","name":"go"}'

# List / get / patch notes
curl -s 'localhost:8080/api/repos?limit=20&sort=stars_desc'
curl -s localhost:8080/api/repos/<repo_id>
curl -s -X PATCH localhost:8080/api/repos/<repo_id> \
  -H 'content-type: application/json' \
  -d '{"notes":"interesting"}'

# Single-repo refresh (sync)
curl -s -X POST localhost:8080/api/repos/<repo_id>/refresh

# Refresh-all (async batch) + poll status
curl -s -X POST localhost:8080/api/repos/refresh-all
# → {"batch_id":"..."}
curl -s localhost:8080/api/batches/<batch_id>

# Stats + change feed
curl -s localhost:8080/api/repos/stats
curl -s 'localhost:8080/api/repos/changes?limit=20'
curl -s 'localhost:8080/api/repos/changes?since=<next_cursor>&limit=20'
```

## Conscious trade-offs

| Choice | Why | Cost |
|--------|-----|------|
| RabbitMQ vs Redis queue | Durable jobs must not share eviction with disposable cache | Extra Compose service |
| Two binaries (`api` / `worker`) | Scale and shut down independently; shared `internal/` | Two processes to run |
| Cursor pagination | Stable under concurrent writes; same keyset as `/changes` | No jump-to-page / offset |
| Job rows in DB for batches | Source of truth for status + idempotent worker updates | Extra table |
| Cache lock (`SET NX`) | Concurrent miss → one GitHub call | Lock TTL / holder-died path |
| At-least-once delivery | Manual ack after terminal DB state; retries with backoff | Handlers must be idempotent |
| DLQ after permanent / exhausted retries | Failed work is inspectable, not silently dropped | Ops must drain DLQ |

## Delivery & idempotency

- Refresh jobs are **at-least-once**. The worker uses conditional status updates on `refresh_batch_jobs` (`pending` → `succeeded` / `failed`); a second delivery of a terminal job is a no-op.
- Concurrent `POST /api/repos` for the same `full_name` hits the UNIQUE constraint and returns a clean **409**, never a double row or bare 500.
- Worker concurrency is capped at **5**. On shutdown, in-flight messages are nacked/redelivered — no silent loss.

## Pagination & `/changes` guarantee

- List endpoints use opaque base64 keyset cursors on `(sort_column, id)`.
- `GET /api/repos/changes` orders by `(updated_at, id)` ascending and advances with `since`.
- Poller contract: **at-least-once** — duplicates are OK if you re-poll the same cursor; **gaps are not**.

## AI workflow

Non-trivial work uses **Research → Worker → Review**:

- Human explainer: [docs/workflows/research-worker-review.md](docs/workflows/research-worker-review.md)
- Project skill: [.cursor/skills/research-worker-review/SKILL.md](.cursor/skills/research-worker-review/SKILL.md)
- Invariants and validate-AI checklist: [AGENTS.md](AGENTS.md)

Treat generated code as untrusted until concurrency, retries, cache locks, and shutdown paths are checked by hand.
