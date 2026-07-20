---
name: project-overview
description: >-
  Fast orientation for the github-tracker codebase (architecture, key flows,
  where to read first). Use when the user is new to the repo, asks to explain
  the project, wants a quick overview, system map, onboarding, or “how does
  this work?” before changing code.
---

# Project overview (quick understanding)

Goal: be productive in **~2 minutes** of reading — not a full codebase tour.

## One-sentence product

Two Go binaries track GitHub repos: **API** (Gin + Ent + Postgres) and **worker** (RabbitMQ). **Redis is cache/locks only — never the job queue.**

## Read in this order (stop when oriented)

1. `README.md` — how to run + conscious trade-offs  
2. `AGENTS.md` — invariants (do not break)  
3. `docs/superpowers/specs/2026-07-19-github-tracker-design.md` — architecture north star  

Only go deeper if the question needs it.

## System map

| Area | Path | Role |
|------|------|------|
| HTTP API | `cmd/api`, `internal/httpapi` | Routes, probes, `/` index |
| Worker | `cmd/worker`, `internal/queue` | Consume kick/refresh; QoS+sem = 5 |
| Use cases | `internal/service` | Repos, batches, cursors, GitHub→API errors |
| GitHub | `internal/githubclient` | Timeout, typed error kinds |
| Cache | `internal/cache` | 5m TTL, `SET NX` + Lua unlock |
| Schema | `ent/schema` | `full_name` UNIQUE; batch jobs |
| Errors | `internal/apierror` | `{ "error": { "code", "message" } }` |

## Three flows to know

1. **Create** — GitHub (via cache single-flight) → insert; concurrent duplicate → **409** (`UNIQUE full_name`).  
2. **Refresh-all** — DB job rows → one **batch.kick** → worker fan-out → refresh jobs → idempotent status updates; repair: `POST /api/batches/:id/enqueue`.  
3. **Changes** — keyset `(updated_at, id)`; **at-least-once** (dupes OK, gaps not).

## Queue mental model

- Work queue + **TTL retry** queue + **DLQ**  
- Ack only after terminal DB state **or** successful retry/DLQ publish; else Nack(requeue)  
- 429 cool-down does **not** burn retry budget  

## What not to assume

- Redis Streams / Redis as queue  
- Exactly-once delivery  
- Offset pagination as the list API  
- Retrospective P0s as still open (many fixed — check code, don’t reopen blindly)

## After overview — pick a skill

| Need | Skill |
|------|--------|
| Live / small invariant change | `.cursor/skills/micro-rwr/SKILL.md` |
| Multi-step feature | `.cursor/skills/research-worker-review/SKILL.md` |
| Pure Q&A | Stay here; cite files above |

## Output when asked “explain the project”

Give: (1) one-sentence product, (2) two binaries + Redis vs Rabbit, (3) the three flows, (4) pointers to README + AGENTS. Keep it short unless they ask to go deeper.
