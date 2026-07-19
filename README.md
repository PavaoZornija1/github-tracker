# github-tracker

Production-shaped Go service that tracks GitHub repositories.

## Status

Watchlist API + async refresh-all worker are implemented. Swagger and README polish are next.

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
| Binaries | `cmd/api`, `cmd/worker` |

## Quick start

```bash
cp .env.example .env
docker compose up -d
set -a && source .env && set +a
make run-api      # terminal 1
make run-worker   # terminal 2
```

```bash
curl -s localhost:8080/healthz
curl -s -X POST localhost:8080/api/repos -H 'content-type: application/json' \
  -d '{"owner":"golang","name":"go"}'
curl -s -X POST localhost:8080/api/repos/refresh-all
# poll: GET /api/batches/{batch_id}
make test
```

## Architecture choices (summary)

- **RabbitMQ for jobs, Redis for cache** — durable work must not share eviction policy with disposable cache.
- **Two binaries** — scale and shut down API vs worker independently; shared `internal/` code.
- **At-least-once jobs** — manual ack after DB terminal state; retries republish with backoff + jitter; permanent/exhausted → DLQ.
- **Idempotent worker** — conditional status updates on `refresh_batch_jobs`; duplicate delivery is a no-op.
- **Cursor pagination** and **at-least-once `/changes`** — see design doc.
- **Concurrent create** — UNIQUE `full_name` → HTTP 409.
- **AI workflow** — use Cursor/Claude aggressively; validate concurrency, retries, and shutdown by hand (`AGENTS.md`).
