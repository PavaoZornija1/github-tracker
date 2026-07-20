---
name: research-worker-review
description: >-
  Project copy of the three-lane agentic pipeline (Research → Worker → Review)
  for github-tracker. Use for multi-step feature work in this repository.
---

# Research → Worker → Review (project)

This repo expects the three-lane flow for non-trivial work. Full skill text also lives in the operator’s personal Cursor skills as `research-worker-review`.

For time-boxed / interview edits, use **micro-RWR** instead: `.cursor/skills/micro-rwr/SKILL.md`.

## Research must start from the design

Before proposing a plan, **read** (do not skip):

- `docs/superpowers/specs/2026-07-19-github-tracker-design.md`

Then map the live code under `internal/` (handlers, service, queue, cache) against that spec. Cite file paths in the plan. Prefer the design’s trade-offs over inventing a new architecture.

Optional context if the change overlaps past retros:

- `docs/superpowers/specs/2026-07-20-retrospective-improvements.md` (status of P0–P2 — do not reopen fixed items as open bugs)

## Invariants for this codebase (review must check)

- Concurrent `POST /api/repos` → one row + clean **409**
- Redis cache single-flight; refresh invalidates
- RabbitMQ at-least-once + idempotent batch jobs; max 5 in flight
- Graceful shutdown without losing unacked jobs
- Error envelope `{ "error": { "code", "message" } }`
- Small focused commits; no drive-by refactors

## Lane cheat-sheet

1. **Research** — read design spec above; explore handlers/services/queue/cache; propose commit split + acceptance checks  
2. **Worker** — implement + `go test ./...` + commits  
3. **Review** — code-reviewer on the new commits only; fix Critical/Important before done  

See `docs/workflows/research-worker-review.md` for the human-oriented explanation.
