# github-tracker

Production-shaped Go service that tracks GitHub repositories (Gin + Ent + Postgres + Redis cache + RabbitMQ worker).

**Binaries:** `cmd/api` (HTTP) and `cmd/worker` (async refresh consumer).

## Quick start (full stack)

One command brings up Postgres, Redis, RabbitMQ, API, and worker:

```bash
cp .env.example .env          # optional: set GITHUB_TOKEN for higher rate limits
make compose-up-full          # docker compose --profile full up -d --build
```

| URL | Purpose |
|-----|---------|
| http://localhost:8080/swagger/index.html | OpenAPI UI |
| http://localhost:8080/healthz | Liveness |
| http://localhost:8080/readyz | Readiness (Postgres + Redis) |
| http://localhost:8080/metrics | Prometheus |
| http://localhost:15672 | RabbitMQ management (guest/guest) |

Stop:

```bash
make compose-down
```

Compose maps Postgres to **host port 5433** (avoids clashing with a local Postgres on 5432). Inside the Compose network, services still use `postgres:5432`.

## Sample API calls

```bash
curl -s localhost:8080/healthz
curl -s localhost:8080/readyz

# Track a repo
curl -s -X POST localhost:8080/api/repos \
  -H 'content-type: application/json' \
  -d '{"owner":"gin-gonic","name":"gin"}'

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
# Repair kick if enqueue failed after rows were created:
curl -s -X POST localhost:8080/api/batches/<batch_id>/enqueue

# Stats + change feed
curl -s localhost:8080/api/repos/stats
curl -s 'localhost:8080/api/repos/changes?limit=20'
curl -s 'localhost:8080/api/repos/changes?since=<next_cursor>&limit=20'
```

## Stack

| Piece | Choice |
|-------|--------|
| HTTP | Gin |
| ORM | Ent + PostgreSQL |
| Cache / locks | Redis |
| Job queue | RabbitMQ (+ TTL retry + DLQ) |
| Docs | swaggo OpenAPI |
| Metrics | Prometheus `/metrics` |

## Ops notes

| Topic | Detail |
|-------|--------|
| Migrations | `APP_ENV=production` skips Ent `Schema.Create`. See [migrations/README.md](migrations/README.md); Atlas is the follow-up. |
| DLQ replay | [docs/runbooks/dlq-replay.md](docs/runbooks/dlq-replay.md) |
| Retry topology | Transient failures publish to `{queue}.retry` with per-message TTL, then return to `refresh`. Rate limits do not burn `WORKER_MAX_RETRIES`. |
| Tests | `make test` |

## Design docs

- [Architecture design](docs/superpowers/specs/2026-07-19-github-tracker-design.md)
- [Implementation plan](docs/superpowers/plans/2026-07-19-github-tracker-implementation.md)
- [Retrospective](docs/superpowers/specs/2026-07-20-retrospective-improvements.md)
- Agent brief: [AGENTS.md](AGENTS.md)

## Conscious trade-offs

| Choice | Why | Cost |
|--------|-----|------|
| RabbitMQ vs Redis queue | Durable jobs must not share eviction with disposable cache | Extra Compose service |
| Two binaries (`api` / `worker`) | Scale and shut down independently; shared `internal/` | Two processes |
| Cursor pagination | Stable under concurrent writes; same keyset as `/changes` | No offset pages |
| Job rows in DB for batches | Source of truth for status + idempotent worker updates | Extra table |
| Batch kick + fan-out | One durable kick after job rows; redelivery recovers partial fan-out | Extra message type |
| At-least-once delivery | Manual ack after terminal DB state; TTL retry queue | Handlers must be idempotent |

## Delivery & idempotency

- Refresh jobs are **at-least-once**. Conditional status updates on `refresh_batch_jobs` (`pending` → `succeeded` / `failed`); a second delivery of a terminal job is a no-op.
- Concurrent `POST /api/repos` for the same `full_name` returns a clean **409**.
- Worker concurrency is capped at **5**. On shutdown, in-flight messages are nacked/redelivered.

## Pagination & `/changes`

- List uses opaque base64 keyset cursors on `(sort_column, id)`.
- `GET /api/repos/changes` orders by `(updated_at, id)` ascending.
- Poller contract: **at-least-once** (duplicates OK; gaps are not).

## AI workflow

Non-trivial work uses **Research → Worker → Review**:

- [docs/workflows/research-worker-review.md](docs/workflows/research-worker-review.md)
- [AGENTS.md](AGENTS.md)
