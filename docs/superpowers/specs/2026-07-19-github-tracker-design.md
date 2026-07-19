# GitHub Tracker — Design

**Date:** 2026-07-19  
**Status:** Approved for implementation

## Goal

Production-shaped Go service that tracks GitHub repositories: sync fetch on write, Redis-cached GitHub reads with single-flight, async refresh-all via RabbitMQ, idempotent workers, and pollable batch status.

## Architecture

```text
Client → cmd/api (Gin) → application services → ports
                              ├─ RepoStore / BatchStore (Ent + Postgres)
                              ├─ GitHubClient (HTTP + Redis cache/lock)
                              └─ RefreshQueue (RabbitMQ publisher)

cmd/worker ← RabbitMQ consumer (max 5 in flight) → same application services
```

- **Postgres** — source of truth (repos, batches, batch jobs).
- **RabbitMQ** — durable async jobs + DLQ (not Redis).
- **Redis** — GitHub response cache (5m TTL) + distributed single-flight locks only.

## Process model

Two binaries, one module:

- `cmd/api` — HTTP, enqueue only for refresh-all.
- `cmd/worker` — consume refresh jobs, bounded concurrency 5.

Shared logic lives under `internal/`.

## Queue (RabbitMQ)

- Queue: `repo.refresh`; dead-letter via DLX → `repo.refresh.dlq`.
- Delivery: **at-least-once** (manual ack after DB terminal state).
- Retries: max 3 for transient (429/5xx/network), backoff + jitter, honor `Retry-After`.
- Permanent failures (404/401): DLQ + job marked failed with reason.
- Message payload: `{ job_id, batch_id, repo_id }`.

## Cache / single-flight

- Cache key: `gh:repo:{owner}/{name}`, TTL 5 minutes.
- Lock key: `gh:lock:{owner}/{name}`, `SET NX` + short expiry; waiters poll cache; expired lock allows another fetcher (holder-died recovery).
- Explicit refresh deletes the cache key.
- Multi-replica safe: lock lives in Redis.

## Migration strategy

For local/dev, the API and worker will call Ent schema migrate (`client.Schema.Create`) on startup. That is convenient and acceptable for this service size; production long-term would switch to versioned migrations (Atlas/goose) before multi-instance rolling deploys where auto-migrate races matter.

**Repository:** id, owner, name, full_name (unique), description, stars, language, html_url, notes, fetched_at, created_at, updated_at.

**RefreshBatch:** id, created_at, updated_at (aggregates derived from jobs).

**RefreshBatchJob:** id, batch_id, repo_id, status (pending|succeeded|failed), attempt, error_reason; unique `(batch_id, repo_id)` or stable `job_id` for idempotency.

## HTTP API

| Method | Path | Notes |
|--------|------|--------|
| POST | `/api/repos` | GitHub fetch + insert; UNIQUE → 409 under concurrency |
| GET | `/api/repos` | `language`, `sort=stars_desc\|updated_desc`, **cursor** pagination |
| GET | `/api/repos/:id` | |
| PATCH | `/api/repos/:id` | notes only |
| DELETE | `/api/repos/:id` | |
| POST | `/api/repos/:id/refresh` | sync refresh; invalidate cache |
| POST | `/api/repos/refresh-all` | 202 + `batch_id`; enqueue only |
| GET | `/api/batches/:id` | total, pending, succeeded, failed[] |
| GET | `/api/repos/stats` | SQL aggregates |
| GET | `/api/repos/changes` | cursor `(updated_at, id)`; at-least-once for poller |

Error envelope: `{ "error": { "code", "message" } }`.

OpenAPI via swaggo; UI at `/swagger/index.html`.

## Pagination & changes

- List: keyset cursor on `(sort_column, id)`, opaque base64 cursor.
- Changes: `WHERE (updated_at, id) > cursor ORDER BY updated_at, id`; guarantee **at-least-once** (duplicates OK, gaps not).

## Observability & ops

- `slog` structured logs; request-id middleware; job-id on worker via context.
- Graceful shutdown both binaries (`signal.NotifyContext`).
- Config via env; `.env.example` provided.

## Tests (beyond rubric minimum)

1. Concurrent duplicate POST → one row + 409  
2. Worker idempotent double-process  
3. Cache single-flight (one GitHub call)  
4. GitHub error class mapping  
5. Batch status aggregation  
6. Changes cursor no-gap  

Prefer Testcontainers for Postgres/Redis/Rabbit; mock GitHub HTTP at the boundary.

## AI workflow artifacts

- `AGENTS.md`, optional `CLAUDE.md`, `.cursor/rules/*` encoding invariants.
- README documents trade-offs and how AI output is validated.

## Conscious trade-offs

| Choice | Why | Cost |
|--------|-----|------|
| RabbitMQ vs Redis/asynq | Durable jobs separate from disposable cache | Extra Compose service |
| Two binaries | Scale/IAM/shutdown isolation | Two processes in Compose |
| Cursor pagination | Stable under writes; aligns with `/changes` | No jump-to-page |
| Job rows in DB for batch | Source of truth for status + idempotency | Extra table |
