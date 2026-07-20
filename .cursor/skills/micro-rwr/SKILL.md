---
name: micro-rwr
description: >-
  Compressed Research → Worker → Review for live or time-boxed changes
  (interview demos, small invariant-touching edits). Use when the user says
  micro-RWR, live change, interview change, or needs a fast plan→test→implement
  loop without full three-lane ceremony.
---

# Micro-RWR (live change)

Same pipeline as Research → Worker → Review, **compressed** for ~10–15 minute changes. Do **not** spawn three long-running subagents unless the user asks for full RWR.

## When to use

- Live interview / demo edits
- Small changes that still touch queue, cache, concurrency, shutdown, or error mapping
- User says “micro-RWR”, “live change”, or “interview change”

## When **not** to use

- Multi-file features, new endpoints, topology changes → full `research-worker-review`
- Typos / renames / pure Q&A → inline, no skill ceremony

## Before coding (always)

1. Read `docs/superpowers/specs/2026-07-19-github-tracker-design.md` (design north star).
2. Read relevant slices of `AGENTS.md` invariants.
3. Speak a **30-second plan**: goal, files to touch (≤4), invariant at risk, acceptance check.

## Loop (strict order)

```text
plan → test → implement → invariant checklist
```

1. **Plan** — one sentence acceptance; list files; name the failure mode (e.g. double delivery, stampede, lost ack).
2. **Test** — add or extend a focused test that would fail if the change is wrong; run that package (`go test ./path/...`).
3. **Implement** — smallest diff; no drive-by refactors; match existing style.
4. **Invariant checklist** — tick only what the change could break (see below). Re-run the targeted test.

## Invariant checklist (from AGENTS.md)

Before calling done, confirm each that applies:

- [ ] Unique `full_name` / concurrent create → clean **409** (not bare 500 / double row)
- [ ] Ack only after terminal DB state; idempotent job status (no double-count)
- [ ] Cache: single-flight lock; refresh invalidates; holder-died via lock TTL; Lua unlock safe
- [ ] Shutdown: unacked work redelivered / not lost
- [ ] GitHub failure kinds → correct HTTP status **or** retry/DLQ decision
- [ ] Error envelope `{ "error": { "code", "message" } }` if API surface changed
- [ ] Redis still **not** used as the job queue

## Narration (interview)

Say the plan and the invariant **before** typing. After the test passes, point at the line that enforces the guarantee. “The AI wrote it” is not an answer.

## Related

- Full flow: `.cursor/skills/research-worker-review/SKILL.md`
- Human explainer: `docs/workflows/research-worker-review.md`
- PR template checklist: `.github/pull_request_template.md`
