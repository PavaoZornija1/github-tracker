# github-tracker

Production-shaped Go service that tracks GitHub repositories.

## Status

Scaffolding in progress. Design and plan:

- [Architecture design](docs/superpowers/specs/2026-07-19-github-tracker-design.md)
- [Implementation plan](docs/superpowers/plans/2026-07-19-github-tracker-implementation.md)
- Agent brief: [AGENTS.md](AGENTS.md)

## Stack

| Piece | Choice |
|-------|--------|
| HTTP | Gin (wired next) |
| ORM | Ent + PostgreSQL |
| Cache / locks | Redis |
| Job queue | RabbitMQ (+ DLQ) |
| Binaries | `cmd/api`, `cmd/worker` |

## Quick start (infra)

```bash
cp .env.example .env
docker compose up -d
# Postgres :5432, Redis :6379, RabbitMQ :5672, management UI :15672 (guest/guest)
```

Load env (example):

```bash
set -a && source .env && set +a
make run-api      # waits on SIGTERM until HTTP is wired
make run-worker   # same for consumer
make test
```

## Architecture choices (summary)

- **RabbitMQ for jobs, Redis for cache** — durable work must not share eviction policy with disposable cache.
- **Two binaries** — scale and shut down API vs worker independently; shared `internal/` code.
- **Cursor pagination** and **at-least-once `/changes`** — see design doc.
- **AI workflow** — use Cursor/Claude aggressively; validate concurrency, retries, and shutdown by hand (`AGENTS.md`).

Full trade-off write-up will expand as features land.
