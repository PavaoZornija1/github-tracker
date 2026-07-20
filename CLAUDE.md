# CLAUDE.md

North star for Claude and other coding agents. Prefer `AGENTS.md` as the full brief; keep this file aligned with it.

## Project

GitHub repo tracker API + worker: **Gin, Ent, Postgres, Redis (cache only), RabbitMQ (jobs).**

## Agentic default

**Research → Worker → Review** for non-trivial work (Research starts from the design spec).  
Live edits: `.cursor/skills/micro-rwr/SKILL.md`.  
Quick map: `.cursor/skills/project-overview/SKILL.md`.  
See `.cursor/skills/research-worker-review/SKILL.md` and `docs/workflows/research-worker-review.md`.

## Non-negotiables

- Do **not** use Redis as the job queue.
- Two entrypoints: `cmd/api`, `cmd/worker`.
- Concurrent duplicate create → DB unique + **409**.
- Worker: max 5 in flight, idempotent, DLQ after permanent/exhausted retries.
- Structured `slog`; request-id / job-id via context.
- Small commits; own every concurrency and retry path.

## Docs

- Design: `docs/superpowers/specs/2026-07-19-github-tracker-design.md`
- Plan: `docs/superpowers/plans/2026-07-19-github-tracker-implementation.md`
