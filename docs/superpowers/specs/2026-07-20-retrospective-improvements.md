# Retrospective — Audit findings & improvement backlog

**Date:** 2026-07-20  
**Scope:** Full codebase audit against assignment invariants + industry practice (Go/Gin, RabbitMQ, Redis locks, Ent migrations)  
**Status:** P0–P2 backlog items implemented (2026-07-20) — see commits on `main` / implementation plan `docs/superpowers/plans/2026-07-20-retrospective-improvements.md`

---

## Executive summary

**Take-home strength:** The service is a credible production *shape*: two binaries, Postgres source of truth, Redis for cache/locks only, RabbitMQ for durable refresh jobs, typed GitHub errors, concurrent-safe create (**409** on `full_name`), idempotent batch job updates, QoS-capped worker (5), and graceful API/worker shutdown. The design and agent invariants in `AGENTS.md` match what most of the code actually does.

**Must-fix before calling this “ops-ready”:** Three P0 gaps can lose work or waste GitHub quota under failure:

1. **Rate-limit burns retries** — 429 / secondary rate limit counts toward `maxRetries` and can permanently fail jobs while the window is still closed; in-process sleep + republish also holds concurrency slots. (`waitOutRateLimit` / Redis `github:rate_limit_until` already exist — the bug is attempt accounting, not a missing cool-down key.)
2. **Ack after failed DLQ publish** — permanent-failure path ignores `PublishDeadLetter` errors then acks, so a message can vanish from both the main queue and the DLQ.
3. **Partial refresh-all enqueue** — job rows are committed, then publishes run in a loop; a mid-loop publish failure leaves a batch with pending rows that may never get a message (or a client error after partial enqueue).

**Separate demo/ops packaging gap (still high priority for the assignment):**

4. **Compose ships infra only** — no `Dockerfile` / api / worker services despite the implementation plan listing them; local demo still depends on host `make run-*`. This does not lose messages by itself, but it fails the “everything boots with docker-compose” bar.

Everything else below is P1/P2 polish or conscious take-home trade-offs — not blockers for a technical interview demo if you own them honestly.

---

## Mapping assignment requirements → status

| Requirement / invariant | Status | Notes |
|-------------------------|--------|--------|
| Track repos via GitHub API; sync create/refresh | **Met** | `RepoService` + typed `githubclient` |
| Redis cache TTL ~5m + single-flight | **Met** | `SET NX` + TTL; double-check cache after lock; invalidate on refresh |
| Concurrent duplicate `POST /api/repos` → one row + **409** | **Met** | UNIQUE `full_name` + mapped conflict |
| Async refresh-all via RabbitMQ (not Redis queue) | **Met** | Publisher + consumer; Redis cache-only |
| At-least-once + idempotent job handling | **Mostly met** | Conditional status updates are solid; ack/DLQ edge cases undermine “no lost jobs” |
| Worker concurrency capped at 5 | **Met** | QoS + semaphore |
| DLQ for permanent / exhausted retries | **Partial** | Topology exists; ack-on-failed-DLQ-publish can drop messages |
| Error JSON `{ "error": { "code", "message" } }` | **Met** | `apierror` + Gin renderer |
| Graceful shutdown (drain / redeliver in-flight) | **Met** | API `Shutdown`; worker nack-on-cancel during backoff |
| OpenAPI / Swagger | **Met** | `/swagger/index.html` |
| Two binaries `cmd/api`, `cmd/worker` | **Met** | Shared `internal/` |
| Docker Compose for full stack | **Partial** | Postgres + Redis + RabbitMQ only; no app images |
| Structured logging with request/job id | **Partial** | Context helpers + slog present; no access-log middleware; middleware order suboptimal |
| Production-grade migrations | **Dev-only** | `Schema.Create` on startup (documented trade-off) |

---

## Gaps vs industry best practice

### Go / Gin HTTP

| Practice | Ours | Gap |
|----------|------|-----|
| Prefer [`gin.New()`](https://gin-gonic.com/docs/introduction/) + explicit middleware over `gin.Default()` | **Done** — `gin.New()` | — |
| Middleware: RequestID before access logging; Recovery wraps handlers | Recovery → RequestID; **no** access logger | Prefer RequestID → slog access log → Recovery so every line (including panics after RequestID) carries `request_id` ([Go slog](https://pkg.go.dev/log/slog), common Gin pattern) |
| [`http.Server`](https://pkg.go.dev/net/http#Server) timeouts: `ReadHeaderTimeout`, `WriteTimeout`, `IdleTimeout` | Only `ReadHeaderTimeout` set | Add Write/Idle (and optionally `ReadTimeout`) to bound slowloris / stuck writers |
| Liveness vs readiness (`/healthz` vs `/readyz`) | Only `/healthz` → `ok` | Split: liveness = process up; readiness = Postgres (+ optionally Redis/Rabbit) ping ([Kubernetes probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)) |
| Graceful `Shutdown` | **Done** | Keep |

### RabbitMQ

| Practice | Ours | Gap |
|----------|------|-----|
| Retry via topology (TTL retry / delay queues + DLX), not app sleep + republish | In-handler `time.Sleep`/timer then `RepublishRefresh` | Prefer [dead-letter / TTL retry](https://www.rabbitmq.com/docs/dlx) or [delayed message](https://www.rabbitmq.com/docs/delayed-message-exchange) patterns so workers stay free ([CloudAMQP: retry strategies](https://www.cloudamqp.com/blog/when-to-use-rabbitmq-dead-letter-exchanges)) |
| Manual ack **only after success**; never ack if DLQ publish failed | Ack always after `_ = PublishDeadLetter(...)` | Check publish error; **nack/requeue** (or fail loudly) if DLQ write fails ([Consumer ack](https://www.rabbitmq.com/docs/confirms#consumer-acks-overview)) |
| QoS / prefetch; durable queues + persistent messages | **Done** | Keep |
| Publisher confirms | Fire-and-forget `PublishWithContext` | Enable [publisher confirms](https://www.rabbitmq.com/docs/confirms) for enqueue-critical paths (refresh-all) |
| Idempotent consumers | **Done** — conditional job status updates | Keep |
| Parking lot / DLQ after max retries | Intended; undermined by silent DLQ publish failure | Fix ack/DLQ coupling; optional poison-message metrics |

### Redis locks

| Practice | Ours | Gap |
|----------|------|-----|
| `SET key token NX PX ttl` for stampede control | `SET NX` + expiry with value `"1"` | Correct shape; improve token |
| Release only if owner: Lua `if GET==token then DEL` | Blind `DEL` on lock key | Another holder after TTL expiry can lose lock early ([Redis SET](https://redis.io/docs/latest/commands/set/); [distributed locks](https://redis.io/docs/latest/develop/clients/patterns/distributed-locks/)) |
| Double-check cache after acquiring lock | **Done** | Keep |

### Ent migrations

| Practice | Ours | Gap |
|----------|------|-----|
| Auto-migrate (`Schema.Create`) for local only | Used in both api and worker startup | Acceptable for take-home |
| Versioned migrations (Atlas) for production | Not present | Follow [Ent + Atlas versioned migrations](https://entgo.io/docs/versioned-migrations) before multi-instance rolling deploys |

---

## Prioritized backlog

Effort: **S** ≤ ~half day · **M** ~1–2 days · **L** multi-day / design-heavy

### P0 — correctness / data loss / quota (P0-1–P0-3) + demo packaging (P0-4)

| ID | Item | Effort | Suggested approach |
|----|------|--------|-------------------|
| P0-1 | **Rate-limit must not burn retries** | M | Keep/extend existing `waitOutRateLimit` + Redis `github:rate_limit_until`. **Do not advance attempt** on pure rate-limit waits or `KindRateLimited` (separate budget from 5xx/network). Prefer moving delay off the worker slot (TTL retry queue) so sleep does not hold prefetch. |
| P0-2 | **Never ack if DLQ publish failed** | S | In `handleDelivery`, if `PublishDeadLetter` returns error → `Nack(false, true)` (or nack without requeue only when safe). Log loudly. Same for max-retry transient path. Add a unit test with a failing publisher mock. |
| P0-3 | **Atomic / recoverable refresh-all enqueue** | M | Options: (a) outbox table + publisher loop; (b) publish then mark `enqueued`; (c) single “batch kick” message + worker fans out; (d) on publish failure, compensate (fail remaining jobs or return 202 with `partial` + repair endpoint). Prefer outbox or kick-message for at-least-once without orphaned `pending`. |
| P0-4 | **Dockerfile + Compose api/worker** *(demo/ops packaging, not message loss)* | M | Multi-stage Dockerfile (build both binaries). Compose services `api` / `worker` with `depends_on` healthchecks, env from `.env`, expose `8080`. Keep infra-only profile optional for host-run demos. |

### P1 — production hygiene

| ID | Item | Effort | Suggested approach |
|----|------|--------|-------------------|
| P1-1 | Access logs (+ sensible middleware stack) | S | `RequestID` → slog access middleware (`request_id`, method, path, status, latency) → `Recovery`. |
| P1-2 | `WriteTimeout` / `IdleTimeout` on `http.Server` | S | Set conservative defaults (e.g. Write 30–60s, Idle 60–120s); document for long poll if ever added. |
| P1-3 | `/readyz` vs `/healthz` | S | `/healthz` process-only; `/readyz` checks DB (and optionally Redis/Rabbit). |
| P1-4 | Redis lock token + Lua release | S | Store random token in `SET NX`; release via compare-and-del script. |
| P1-5 | Publisher confirms on enqueue | M | Confirm channel for API publish path; fail the request (or outbox retry) if unacked. |
| P1-6 | Topology-based retries | L | Replace app sleep+republish with TTL retry queues → main queue; DLX parking lot after `x-death` count ≥ max. |

### P2 — nice-to-have / scale

| ID | Item | Effort | Suggested approach |
|----|------|--------|-------------------|
| P2-1 | Atlas versioned migrations | M | Generate from Ent; CI applies; disable `Schema.Create` when `APP_ENV=production`. |
| P2-2 | Metrics / tracing | M | RED metrics for HTTP + queue depth + GitHub rate-limit remaining gauge. |
| P2-3 | DLQ replay tooling | S | Small admin command or documented `rabbitmqadmin` get/publish loop. |
| P2-4 | Integration test for DLQ-publish failure | S | Pairs with P0-2. |
| P2-5 | Compose profiles (`infra` vs `full`) | S | Document in README. |

---

## Recommended demo talking points

Own the trade-offs; do not claim “production complete.”

1. **Architecture split** — Why RabbitMQ for jobs and Redis only for cache/locks (eviction vs durability). Point at Compose + two binaries.
2. **Concurrency story** — UNIQUE + 409 demo; cache single-flight (`SET NX` + double-check); worker QoS 5 + idempotent conditional updates.
3. **Failure modes you already handle** — Typed GitHub errors; permanent → DLQ *intent*; shutdown nack/redeliver during backoff.
4. **What you would fix next (P0)** — Say out loud: “Today, if DLQ publish fails we still ack — that’s a bug I’d fix first. Refresh-all enqueue isn’t transactional with Rabbit. Rate limits can exhaust retry budget.” That honesty reads as senior.
5. **Migrations** — Auto-migrate is intentional for local/demo; Atlas before multi-replica prod.
6. **AI workflow** — Research → Worker → Review and the invariant checklist in `AGENTS.md` as how you keep agents from breaking ack/lock/409 paths.

---

## Conscious keep (intentionally unchanged for the take-home)

These are **not** oversights to “fix” in the assignment window unless an interviewer asks:

- **Ent `Schema.Create` on startup** for local/dev — documented; Atlas is P2.
- **App-level backoff + republish** as the *first* retry implementation — simpler to reason about in a take-home; topology retries are the industry upgrade (P1-6), not a demo blocker once P0-1 softens rate-limit accounting.
- **No end-user auth** — server-side optional `GITHUB_TOKEN` only (Swagger description already states this).
- **Cursor/keyset pagination only** — no offset pages.
- **Batch status derived from job rows** — extra table is the source of truth for idempotency.
- **Infra-first Compose historically** — host `make run-api` / `make run-worker` remains a valid demo path even after P0-4 lands; full Compose is convenience, not the only story.
- **Blind lock `DEL` until P1-4** — low practical risk with short lock TTL and double-check-after-acquire; token+Lua is correctness polish.
- **Single `/healthz` until P1-3** — fine for local; split when container orchestration appears.
- **No publisher confirms until P1-5** — acceptable if outbox/P0-3 covers enqueue durability another way.

---

## References (quick)

- [Gin introduction / middleware](https://gin-gonic.com/docs/introduction/)
- [net/http Server timeouts](https://pkg.go.dev/net/http#Server)
- [Kubernetes liveness/readiness probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)
- [RabbitMQ consumer acknowledgements](https://www.rabbitmq.com/docs/confirms#consumer-acks-overview)
- [RabbitMQ dead-letter exchanges](https://www.rabbitmq.com/docs/dlx)
- [RabbitMQ publisher confirms](https://www.rabbitmq.com/docs/confirms)
- [CloudAMQP: when to use DLX / retries](https://www.cloudamqp.com/blog/when-to-use-rabbitmq-dead-letter-exchanges)
- [Redis SET NX PX](https://redis.io/docs/latest/commands/set/)
- [Redis distributed locks pattern](https://redis.io/docs/latest/develop/clients/patterns/distributed-locks/)
- [Ent versioned migrations (Atlas)](https://entgo.io/docs/versioned-migrations)

---

## Related artifacts

- Design: [2026-07-19-github-tracker-design.md](./2026-07-19-github-tracker-design.md)
- Plan: [../plans/2026-07-19-github-tracker-implementation.md](../plans/2026-07-19-github-tracker-implementation.md)
- Agent invariants: [../../../AGENTS.md](../../../AGENTS.md)
