# GitHub Tracker — Agent Guide

## What this is

Production-shaped Go service that tracks GitHub repositories (Gin + Ent + Postgres + Redis cache + RabbitMQ worker).

Read `docs/superpowers/specs/2026-07-19-github-tracker-design.md` before large changes.

## How we work (agentic)

**Default for non-trivial tasks:** Research → Worker → Review.

- Skill: `.cursor/skills/research-worker-review/SKILL.md`
- Human explainer: `docs/workflows/research-worker-review.md`
- Personal Cursor skill (all repos): `research-worker-review`

Trivial one-liners can be inline. Anything touching invariants below gets the three lanes.

## Stack

- Go 1.22+, Gin, Ent, PostgreSQL
- Redis — **cache and single-flight locks only**
- RabbitMQ — async refresh jobs + DLQ
- swaggo OpenAPI at `/swagger/index.html`
- Binaries: `cmd/api`, `cmd/worker`

## Invariants (do not break)

1. `full_name` UNIQUE → concurrent `POST /api/repos` returns clean **409**, never double-row or bare 500.
2. Refresh jobs are **at-least-once**; handlers must be **idempotent** (conditional job status updates).
3. Worker concurrency capped at **5**.
4. GitHub client: explicit timeout, context, typed errors (not-found / rate-limit / unauthorized / server / network).
5. Cache TTL 5m; concurrent miss → exactly one GitHub call via Redis `SET NX` lock.
6. Error JSON: `{ "error": { "code", "message" } }`.
7. Graceful shutdown: API drains; worker nacks/redelivers in-flight — no lost jobs.
8. Imports at package top only (no inline imports).

## Layout

| Path | Role |
|------|------|
| `cmd/api` | HTTP server |
| `cmd/worker` | Rabbit consumer |
| `internal/service` | Use cases |
| `internal/githubclient` | GitHub HTTP + error classes |
| `internal/cache` | Redis cache/lock |
| `internal/queue` | Rabbit publisher/consumer |
| `ent/schema` | Ent schema source |

## Validate AI output

Before accepting generated code, re-read:

- Unique constraint / 409 mapping
- Ack-after-commit and idempotent batch counters
- Lock TTL vs holder-died path
- Shutdown ordering
- Every GitHub failure mode → status or retry

"The AI wrote it" is not a defense in review.

## Commands

```bash
docker compose up -d
cp .env.example .env
make run-api
make run-worker
make test
make swag
```
