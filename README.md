# github-tracker

Production-shaped Go service that tracks GitHub repositories (Gin + Ent + Postgres + Redis cache + RabbitMQ worker).

## Status

**Done:** HTTP API (`cmd/api`), async refresh worker (`cmd/worker`), OpenAPI/Swagger UI at `/swagger/index.html`, Compose `full` profile, `/readyz`, `/metrics`, TTL retry topology.

Design and plan:

- [Architecture design](docs/superpowers/specs/2026-07-19-github-tracker-design.md)
- [Implementation plan](docs/superpowers/plans/2026-07-19-github-tracker-implementation.md)
- [Retrospective & improvement backlog](docs/superpowers/specs/2026-07-20-retrospective-improvements.md) (P0–P2 implemented)
- Agent brief: [AGENTS.md](AGENTS.md)

## Stack

| Piece | Choice |
|-------|--------|
| HTTP | Gin |
| ORM | Ent + PostgreSQL |
| Cache / locks | Redis |
| Job queue | RabbitMQ (+ TTL retry + DLQ) |
| Docs | swaggo OpenAPI |
| Binaries | `cmd/api`, `cmd/worker` |
| Metrics | Prometheus `/metrics` |

## Runbook

### Infra only (host `make run-*`)

```bash
cp .env.example .env
docker compose up -d          # postgres (host :5433), redis, rabbitmq
set -a && source .env && set +a
make run-api                  # terminal 1
make run-worker               # terminal 2
```

Postgres is mapped to **host port 5433** so it does not clash with a local Postgres on 5432. Containers still talk on internal `postgres:5432`.

### Full stack in Compose

```bash
cp .env.example .env
make compose-up-full          # docker compose --profile full up -d --build
# API: http://localhost:8080/healthz  (liveness)
#      http://localhost:8080/readyz   (Postgres + Redis)
#      http://localhost:8080/metrics
```

Swagger UI: [http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html)

### Sample curls

```bash
curl -s localhost:8080/healthz
curl -s localhost:8080/readyz
curl -s localhost:8080/metrics | head

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
# Repair kick if enqueue failed after rows were created:
curl -s -X POST localhost:8080/api/batches/<batch_id>/enqueue

# Stats + change feed
curl -s localhost:8080/api/repos/stats
curl -s 'localhost:8080/api/repos/changes?limit=20'
curl -s 'localhost:8080/api/repos/changes?since=<next_cursor>&limit=20'
```

## Ops notes

| Topic | Detail |
|-------|--------|
| Compose profiles | Default `docker compose up -d` = infra. `--profile full` adds api + worker images. |
| Liveness / readiness | `/healthz` = process up; `/readyz` = Postgres + Redis ping (503 if not). |
| Metrics | Scrape `GET /metrics` (`http_requests_total`, `github_errors_total`). |
| Migrations | `APP_ENV=production` skips Ent `Schema.Create`. See [migrations/README.md](migrations/README.md); Atlas is the follow-up. |
| DLQ replay | [docs/runbooks/dlq-replay.md](docs/runbooks/dlq-replay.md) |
| Retry topology | Transient failures publish to `{queue}.retry` with per-message TTL, then dead-letter back to `refresh`. Rate limits do not burn `WORKER_MAX_RETRIES`. |

## Conscious trade-offs

| Choice | Why | Cost |
|--------|-----|------|
| RabbitMQ vs Redis queue | Durable jobs must not share eviction with disposable cache | Extra Compose service |
| Two binaries (`api` / `worker`) | Scale and shut down independently; shared `internal/` | Two processes to run |
| Cursor pagination | Stable under concurrent writes; same keyset as `/changes` | No jump-to-page / offset |
| Job rows in DB for batches | Source of truth for status + idempotent worker updates | Extra table |
| Cache lock (token + Lua) | Concurrent miss → one GitHub call; safe release | Lock TTL / holder-died path |
| At-least-once delivery | Manual ack after terminal DB state; TTL retry queue | Handlers must be idempotent |
| Batch kick + fan-out | One durable kick after CreateBulk; redelivery recovers partial fan-out | Extra message type |
| DLQ after permanent / exhausted retries | Failed work is inspectable, not silently dropped | Ops must drain DLQ |

## Delivery & idempotency

- Refresh jobs are **at-least-once**. The worker uses conditional status updates on `refresh_batch_jobs` (`pending` → `succeeded` / `failed`); a second delivery of a terminal job is a no-op.
- Concurrent `POST /api/repos` for the same `full_name` hits the UNIQUE constraint and returns a clean **409**, never a double row or bare 500.
- Worker concurrency is capped at **5**. Transient retries free the slot (TTL retry queue). On shutdown, in-flight messages are nacked/redelivered — no silent loss.
- Publisher confirms wait on enqueue (kick / refresh / DLQ / retry).

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
