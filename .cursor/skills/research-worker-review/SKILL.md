---
name: research-worker-review
description: >-
  Project copy of the three-lane agentic pipeline (Research → Worker → Review)
  for github-tracker. Use for multi-step feature work in this repository.
---

# Research → Worker → Review (project)

This repo expects the three-lane flow for non-trivial work. Full skill text also lives in the operator’s personal Cursor skills as `research-worker-review`.

## Invariants for this codebase (review must check)

- Concurrent `POST /api/repos` → one row + clean **409**
- Redis cache single-flight; refresh invalidates
- RabbitMQ at-least-once + idempotent batch jobs; max 5 in flight
- Graceful shutdown without losing unacked jobs
- Error envelope `{ "error": { "code", "message" } }`
- Small focused commits; no drive-by refactors

## Lane cheat-sheet

1. **Research** — explore handlers/services/queue/cache; propose commit split  
2. **Worker** — implement + `go test ./...` + commits  
3. **Review** — code-reviewer on the new commits only  

See `docs/workflows/research-worker-review.md` for the human-oriented explanation.
