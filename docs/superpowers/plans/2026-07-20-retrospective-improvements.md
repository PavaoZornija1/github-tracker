# Retrospective Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use Research → Worker → Review (`.cursor/skills/research-worker-review/SKILL.md`). Implement work-package by work-package. Each commit is one focused, reviewable unit. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every P0 + P1 + P2 item in `docs/superpowers/specs/2026-07-20-retrospective-improvements.md` without breaking invariants (409 create, QoS 5, idempotent jobs, cache single-flight, error envelope, graceful shutdown).

**Architecture:** Keep two binaries + shared `internal/`. Fix ack/retry/enqueue correctness first; then HTTP/Redis hygiene; then replace in-process sleep retries with one TTL retry queue + parking DLQ (derived names, existing `Config` env vars); finish with packaging, migration gate, minimal metrics, DLQ replay docs.

**Tech Stack:** Go 1.22+, Gin, Ent, Postgres, Redis, amqp091-go, Docker Compose, optional `prometheus/client_golang` for P2-2.

**Source spec:** `docs/superpowers/specs/2026-07-20-retrospective-improvements.md`

## Global Constraints

- Module `github.com/PavaoZornija1/github-tracker`; binaries only `cmd/api`, `cmd/worker`
- Redis = cache/locks only; RabbitMQ = jobs
- Worker concurrency ≤ 5; at-least-once + conditional job status updates
- Never ack a delivery if DLQ publish failed
- Rate-limit waits must not burn `WORKER_MAX_RETRIES`
- Error JSON `{ "error": { "code", "message" } }`
- No inline imports; exhaustive `switch` with `default: return fmt.Errorf("unhandled %v", x)`
- Small commits; do not skip Review for queue/cache/shutdown changes
- Do not force-push or amend unless user asks

## Recommendations (locked for this plan)

| ID | Decision |
|----|----------|
| **P0-3** | Prefer **batch kick + fan-out** over full outbox table: commit batch/job rows, publish one kick message; worker fans out `PublishRefresh` for still-`pending` jobs. Kick redelivery recovers mid-loop failure. |
| **P1-6** | **One retry queue + TTL → main; parking via existing DLX/DLQ; attempt via `x-death` (and keep `x-attempt` header as fallback).** No new required env vars — derive `repo.refresh.retry` from `RABBITMQ_QUEUE`. |
| **P2-1** | **Pragmatic:** gate `Schema.Create` behind `APP_ENV != production` + document Atlas as follow-up. **Do not** stand up full Atlas migrate dir/CI in this pass (too heavy for one session; local demo still works). |
| **P2-2** | **Ship minimal Prometheus:** `/metrics` with HTTP request counter + GitHub error counter. Skip tracing, queue-depth gauges, RED histograms for this pass. |

## File map (touched across packages)

| Area | Files |
|------|--------|
| Queue | `internal/queue/rabbit.go`, `errors.go`, `message.go` (+ new `consumer_test.go`, maybe `topology.go`) |
| Batch / enqueue | `internal/service/batch.go`, `batch_test.go`; optional kick type in `internal/queue/message.go` |
| Cache locks | `internal/cache/github.go`, `github_test.go` |
| HTTP | `internal/httpapi/router.go`, `response.go` (+ new `accesslog.go`, `readyz.go`); `cmd/api/main.go` |
| Worker | `cmd/worker/main.go` |
| Config / env | `internal/config/config.go`, `.env.example` |
| DB migrate gate | `internal/platform/db/db.go` |
| Compose / Docker | `Dockerfile`, `docker-compose.yml`, `Makefile`, `README.md` |
| Metrics | new `internal/metrics/` or thin helpers in `httpapi` + `githubclient` |
| Docs | `AGENTS.md` (migration note), `README.md`, short DLQ replay section |
| Tests | queue consumer unit tests; batch rate-limit / kick tests; cache Lua release tests |

---

## Execution order (work packages)

```text
WP1  P0-2 + P2-4     Never ack on DLQ publish failure (+ test)
WP2  P0-1            Rate-limit does not burn retries
WP3  P0-3            Atomic/recoverable refresh-all (kick + fan-out)
WP4  P0-4 + P2-5     Dockerfile + Compose api/worker + profiles
WP5  P1-1 + P1-2     Access log middleware + HTTP timeouts
WP6  P1-3            /healthz vs /readyz
WP7  P1-4            Redis lock token + Lua release
WP8  P1-5            Publisher confirms on enqueue path
WP9  P1-6            Topology TTL retry (replaces sleep+republish)
WP10 P2-1            APP_ENV gate for Schema.Create + Atlas docs
WP11 P2-2            Minimal Prometheus /metrics
WP12 P2-3            DLQ replay tooling / docs
```

**Why this order:** correctness that can drop messages or burn quota first; packaging next for demo; HTTP/Redis polish; confirms before topology cutover; topology last among queue changes so P0-1/P0-2 stay valid under both retry strategies; migrations/metrics/replay last.

---

## WP1 — P0-2 + P2-4: Never ack if DLQ publish failed

**Effort:** S  
**Depends on:** none  
**Review required:** yes (ack path)

### Current bug

`internal/queue/rabbit.go` `handleDelivery` (permanent + max-retry transient paths):

```go
_ = c.publisher.PublishDeadLetter(...)
_ = d.Ack(false)
```

### Target behavior

1. If `PublishDeadLetter` returns error → `Nack(false, true)` (requeue) + slog error with job id / attempt / reason.
2. If DLQ publish succeeds → Ack.
3. Unmarshal failure: keep Ack (poison body) **or** DLQ-then-ack with same publish-error rule; prefer DLQ with reason `invalid_payload` then ack only on success.

### Files

- `internal/queue/rabbit.go` — `handleDelivery`
- `internal/queue/consumer_test.go` (**new**) — table-driven with fake publisher / injectable deps
- Optionally extract `deadLetterPublisher` interface on `Consumer` for tests without live Rabbit

### Commits

1. `test: cover DLQ publish failure nacks delivery`  
   - Failing test: mock publisher returns error → expect Nack requeue, no Ack.
2. `fix: nack when dead-letter publish fails`  
   - Implement; also log; same for max-retry transient path.
3. (optional if refactor needed) `refactor: inject dead-letter publisher for consumer tests`

### Acceptance

- [ ] Unit test fails before fix, passes after
- [ ] Success path still Acks
- [ ] Shutdown nack-during-backoff unchanged
- [ ] `go test ./internal/queue/...`

---

## WP2 — P0-1: Rate-limit must not burn retries

**Effort:** M (accounting only here; slot-holding sleep fixed fully in WP9)  
**Depends on:** none (can parallel WP1)  
**Review required:** yes

### Current bug

`internal/service/batch.go` `ProcessRefreshJob`:

- `waitOutRateLimit` returns `queue.NewTransient(...)` → consumer increments attempt on republish.
- `KindRateLimited` shares the same `attempt >= maxRetries` budget as 5xx/network.

### Target behavior

1. Introduce a distinct transient signal for rate-limit / cool-down, e.g. `queue.NewRateLimited(err, retryAfter)` or `TransientError{RateLimit: true}`.
2. In `handleDelivery` (until WP9): on rate-limit transient → **republish with same `attempt`** (or skip attempt bump); still sleep/backoff using `RetryAfter` (slot hold accepted until WP9).
3. In `ProcessRefreshJob`: `KindRateLimited` and `waitOutRateLimit` never call `markFailed` due to attempt exhaustion; only 5xx/network consume `maxRetries`.
4. Keep Redis `github:rate_limit_until` write on 429.

### Files

- `internal/queue/errors.go` — rate-limit transient type or flag
- `internal/queue/rabbit.go` — attempt bump only for non-rate-limit transient
- `internal/service/batch.go` — split rate-limit vs server/network budget
- `internal/service/batch_test.go` — rate-limit at attempt==maxRetries still returns transient, job stays pending

### Commits

1. `test: rate limit does not exhaust retry budget`
2. `fix: exclude github rate limits from retry budget`
3. `fix: republish rate-limit waits without incrementing attempt`

### Acceptance

- [ ] Job at attempt=3 with 429 still retries (pending), not failed/DLQ
- [ ] KindServer/KindNetwork still fail after maxRetries
- [ ] Redis cool-down key still set
- [ ] `go test ./internal/service/... ./internal/queue/...`

---

## WP3 — P0-3: Recoverable refresh-all enqueue (kick + fan-out)

**Effort:** M  
**Depends on:** WP1 helpful (enqueue durability), not hard-blocked  
**Review required:** yes

### Current bug

`StartRefreshAll` CreateBulk then loops `PublishRefresh`; mid-loop error returns error after some messages exist → orphaned `pending` rows with no message.

### Recommended design (minimal viable)

1. **DB:** Create batch + all job rows (unchanged).
2. **Publish one kick:** new message type, e.g. `queue.BatchKick{BatchID uuid.UUID}` on same exchange with new routing key `refresh.kick` (constant in `message.go`), bound to **same main queue** OR a dedicated consumer path on the worker.
   - Simplest binding: same queue, JSON envelope with `"type":"kick"|"refresh"` discriminated union.
3. **Worker handler:** if kick → list jobs where `batch_id=X AND status=pending` → `PublishRefresh` each; on mid-loop publish error return transient so kick redelivers; fan-out is idempotent because duplicate refresh messages are already handled by conditional updates.
4. **API:** `StartRefreshAll` returns 202 after successful kick publish only; if kick publish fails → return 503 (batch rows exist — document that client may retry kick via new repair endpoint **or** re-call a `POST /api/batches/:id/enqueue` that re-publishes kick).

**Prefer also:** `POST /api/batches/:id/enqueue` (internal/repair) that publishes kick if batch exists — makes partial failure recoverable without new batch.

**Avoid for this pass:** full outbox table (heavier schema + poller).

### Files

- `internal/queue/message.go` — envelope / kick type + marshal tests
- `internal/queue/rabbit.go` — `PublishBatchKick`; declare bind for kick RK if separate; consumer dispatch
- `internal/service/batch.go` — `StartRefreshAll` publishes kick; `FanOutBatch(ctx, batchID)`; optional `RequeueBatch`
- `internal/httpapi/batches.go`, `router.go` — optional repair route
- `internal/service/batch_test.go` — publisher that fails on Nth PublishRefresh; kick fan-out retries pending only
- `cmd/worker/main.go` — wire kick vs refresh in handler (or keep dispatch inside queue.Consumer)

### Commits

1. `feat: add batch kick message type`
2. `feat: fan-out pending refresh jobs from batch kick`
3. `fix: refresh-all enqueues via kick instead of per-repo publish loop`
4. `feat: add POST /api/batches/:id/enqueue repair endpoint` (optional but recommended)
5. `test: kick fan-out recovers after mid-loop publish failure`

### Acceptance

- [ ] No orphaned pending without a path to get a message (kick redelivery or repair)
- [ ] Duplicate kick does not double-succeed counters (idempotent job updates)
- [ ] Empty repo list still returns batch_id with no kick (or no-op kick)
- [ ] `go test ./internal/service/... ./internal/queue/... ./internal/httpapi/...`

---

## WP4 — P0-4 + P2-5: Dockerfile, Compose app services, profiles

**Effort:** M  
**Depends on:** none (demo packaging)  
**Review required:** no (unless healthcheck wiring wrong)

### Target

- Multi-stage `Dockerfile`: build `api` + `worker` binaries (Go 1.22+), minimal runtime image.
- `docker-compose.yml`:
  - **profile `infra`** (default for host `make run-*`): postgres, redis, rabbitmq (today’s services).
  - **profile `full`** (or default full stack): + `api`, `worker` with `depends_on: condition: service_healthy`, env from `.env`, port `8080:8080`.
- `DATABASE_URL` / `REDIS_URL` / `RABBITMQ_URL` use Compose DNS hostnames (`postgres`, `redis`, `rabbitmq`).
- Makefile: `compose-up` / `compose-up-full` documented.
- README: both demo paths.

### Files

- `Dockerfile` (**new**)
- `docker-compose.yml`
- `.env.example` (compose-oriented comments / optional `.env.compose.example`)
- `Makefile`
- `README.md`
- `.dockerignore` (**new**, recommended)

### Commits

1. `chore: add multi-stage Dockerfile for api and worker`
2. `chore: add api and worker Compose services`
3. `chore: add Compose profiles infra vs full`
4. `docs: document Compose profiles and full-stack demo`

### Acceptance

- [ ] `docker compose --profile full up -d --build` brings API to `/healthz`
- [ ] Worker connects and consumes (log line)
- [ ] `docker compose up -d` (infra-only / profile) still works for host `make run-api`
- [ ] No secrets committed

---

## WP5 — P1-1 + P1-2: Access logs + HTTP timeouts

**Effort:** S  
**Depends on:** none  
**Review required:** no

### Target

Middleware order in `NewRouter`:

1. `RequestID()`
2. slog access log (`request_id`, method, path, status, latency_ms)
3. `gin.Recovery()` (or custom recovery that logs with request_id)

`cmd/api/main.go` `http.Server`:

- keep `ReadHeaderTimeout: 5s`
- add `WriteTimeout: 60s`, `IdleTimeout: 90s` (document: no long-poll endpoints today)

### Files

- `internal/httpapi/router.go`
- `internal/httpapi/accesslog.go` (**new**)
- `internal/httpapi/accesslog_test.go` (**new**, optional)
- `cmd/api/main.go`
- `README.md` or config comment for timeouts

### Commits

1. `feat: add slog HTTP access log middleware`
2. `fix: order RequestID before access log and Recovery`
3. `chore: set WriteTimeout and IdleTimeout on API server`

### Acceptance

- [ ] Access log lines include `request_id`
- [ ] Panic still recovers with 500
- [ ] Timeouts set as above

---

## WP6 — P1-3: `/healthz` vs `/readyz`

**Effort:** S  
**Depends on:** WP4 nice-to-have for Compose healthchecks  
**Review required:** no

### Target

- `GET /healthz` — process up → `{"status":"ok"}` (unchanged semantics).
- `GET /readyz` — Ping Postgres (required); optionally Redis `PING`; Rabbit optional (skip if awkward without shared client in router).
- Wire deps: extend `RouterDeps` with `Ready func(context.Context) error` or `*sql.DB` / ent ping helper from `cmd/api`.

Compose `api` healthcheck should use `/readyz` once present.

### Files

- `internal/httpapi/router.go`
- `internal/httpapi/health.go` (**new**)
- `cmd/api/main.go` — pass ping closures
- `internal/platform/db/db.go` — export `Ping` if needed
- `docker-compose.yml` — api healthcheck path
- Swagger: optional skip for ops routes

### Commits

1. `feat: add /readyz with database ping`
2. `chore: point Compose api healthcheck at /readyz`

### Acceptance

- [ ] `/healthz` 200 even if DB down (liveness)
- [ ] `/readyz` 503 when DB unreachable
- [ ] `go test` for handler with fake ready func

---

## WP7 — P1-4: Redis lock token + Lua release

**Effort:** S  
**Depends on:** none  
**Review required:** yes (cache invariant)

### Current

`SET NX` value `"1"`; release via blind `DEL` in `fetchAndCache` defer and early paths.

### Target

1. Generate random token (e.g. `uuid` or 16 crypto bytes hex) on acquire.
2. `SET key token NX PX lockTTL`.
3. Release with Lua: `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) else return 0 end`.
4. Waiters unchanged (poll cache + retry lock).

### Files

- `internal/cache/github.go`
- `internal/cache/github_test.go` — miniredis or existing redis test harness: expired lock + new holder not deleted by old defer

### Commits

1. `test: lock release must not delete another holder token`
2. `fix: use token + Lua compare-and-del for cache locks`

### Acceptance

- [ ] Concurrent miss still single upstream fetch (existing tests green)
- [ ] Stale unlock does not delete new holder’s lock
- [ ] `go test ./internal/cache/...`

---

## WP8 — P1-5: Publisher confirms on enqueue

**Effort:** M  
**Depends on:** WP3 (kick is the critical publish)  
**Review required:** yes

### Target

- Confirm channel for API/publisher path used by kick + `PublishRefresh` fan-out.
- `Confirm(false)` + wait for ack with context timeout (e.g. 5s) after each `PublishWithContext`.
- If nack/timeout → return error to caller (StartRefreshAll / FanOut / repair fails loudly; kick redelivery helps fan-out).
- Worker republish/DLQ path: confirms recommended but secondary; at least DLQ + kick/refresh publish from API must confirm.

### Files

- `internal/queue/rabbit.go` — `publish` helper waits for confirm
- Possibly `Publisher` holds a dedicated confirm channel with mutex (amqp channels are not concurrent-safe)
- `internal/service/batch_test.go` — mock already returns error; add integration note if no live Rabbit in CI

### Commits

1. `feat: enable RabbitMQ publisher confirms on publish path`
2. `test: treat missing confirm as enqueue failure` (unit with fake if feasible)

### Acceptance

- [ ] Failed confirm surfaces as error from `PublishRefresh` / `PublishBatchKick`
- [ ] No fire-and-forget on refresh-all kick
- [ ] Channel locking documented; no concurrent use of one channel

### Note

If WP3 kick+redelivery is solid, confirms are belt-and-suspenders — still do them for assignment “enqueue durability” story.

---

## WP9 — P1-6: Topology-based retries (MVP)

**Effort:** L  
**Depends on:** WP1, WP2 (correct ack + rate-limit accounting); ideally WP8  
**Review required:** yes (mandatory)

### Minimal viable topology (fits existing Config)

**Existing env (unchanged required set):**

| Env | Default | Role |
|-----|---------|------|
| `RABBITMQ_EXCHANGE` | `repo.jobs` | Main direct exchange |
| `RABBITMQ_QUEUE` | `repo.refresh` | Work queue (consumer) |
| `RABBITMQ_DLX` | `repo.jobs.dlx` | Parking (+ retry ingress OK) |
| `RABBITMQ_DLQ` | `repo.refresh.dlq` | Parking lot queue |

**Derived (no new required env):**

| Name | Derivation | Role |
|------|------------|------|
| Retry queue | `cfg.Queue + ".retry"` → `repo.refresh.retry` | TTL hold, no consumers |
| Retry RK | `refresh.retry` (const) | Bind retry queue ← `cfg.Exchange` **or** ← `cfg.DLX` |

**Declare order (`declare`):**

1. Exchanges: `cfg.Exchange` (direct, durable), `cfg.DLX` (direct, durable) — as today.
2. **Retry queue** `repo.refresh.retry`:
   - `x-message-ttl`: default **5000** ms (optional env `RABBITMQ_RETRY_TTL_MS` later; not required).
   - Prefer **per-message `Expiration`** for rate-limit `RetryAfter` (cap e.g. 60s) so one queue works for backoff + 429.
   - `x-dead-letter-exchange`: `cfg.Exchange`
   - `x-dead-letter-routing-key`: `RoutingKeyRefresh` (`refresh`)
3. **Main queue** `cfg.Queue`: durable; bind RK `refresh` (+ kick RK if used). **Do not** set main queue DLX to parking for all nacks (would bypass retry). Transient path is explicit publish to retry RK.
4. **DLQ** `cfg.DLQ`: bind `cfg.DLX` RK `dead` — parking only via `PublishDeadLetter` (keep explicit reason header).

**Consumer behavior (replace sleep + RepublishRefresh):**

```text
success            → Ack
permanent error    → PublishDeadLetter (confirm); on OK Ack; else Nack(requeue)  [WP1]
transient:
  deathCount = x-death count for cfg.Queue (or HeaderAttempt)
  if rate-limit (WP2): publish to retry RK with Expiration=RetryAfter; Ack original
  else if deathCount >= maxRetries: DLQ path
  else: publish to retry RK with Expiration=backoff(attempt); Ack original
  if retry publish fails: Nack(requeue)
shutdown during handle: Nack(requeue) — no in-process sleep
```

**Attempt / x-death:**

- Primary: sum `x-death` entries for queue `cfg.Queue` (each cycle main→retry→main increments).
- Keep writing `HeaderAttempt` on publish for DB `SetAttempt` and debugging.
- Rate-limit publishes: **do not** increment logical attempt / ignore for maxRetries (WP2).

**Remove:** in-handler `time.Sleep` / timer select for retry delay (shutdown nack-during-sleep becomes unnecessary for retries; keep cancel awareness on handler ctx only).

### Files

- `internal/queue/rabbit.go` — `declare`, `PublishRetry`, `handleDelivery`, helpers for `x-death`
- `internal/queue/message.go` — constants for retry RK
- `internal/config/config.go` — optional `RabbitMQRetryTTL` only if you want override; **default derive**
- `.env.example` — comment documenting derived retry queue name
- `cmd/worker/main.go` — log retry queue name
- Tests: topology unit tests with amqp mock or document manual Compose check
- `.cursor/rules/worker-rabbit.mdc` — update retry description
- `AGENTS.md` — one line on TTL retry

### Commits

1. `feat: declare TTL retry queue dead-lettering to main`
2. `feat: publish transient failures to retry queue instead of sleeping`
3. `feat: count retries via x-death toward maxRetries`
4. `fix: keep rate-limit retries from consuming x-death budget` (align with WP2)
5. `docs: document retry topology and derived queue name`
6. `test: handleDelivery routes transient to retry without sleep`

### Acceptance

- [ ] Worker slots free while message sits in retry queue
- [ ] After `WORKER_MAX_RETRIES` non-rate-limit failures → DLQ + job failed
- [ ] Rate-limit still cools down via TTL without permanent fail
- [ ] WP1 still holds (failed DLQ publish → Nack)
- [ ] Graceful shutdown: in-flight handler cancel → Nack requeue (no lost jobs)
- [ ] `go test ./...` + manual: publish poison transient, watch retry queue in management UI `:15672`

### Rollback note

App-level republish can remain behind a bool `RABBITMQ_TOPOLOGY_RETRY=true` default on for one release; **prefer hard cut** in take-home to avoid two paths — hard cut OK if tests cover handleDelivery.

---

## WP10 — P2-1: Migration gate (pragmatic Atlas path)

**Effort:** S for gate + docs; (defer full Atlas)  
**Depends on:** none  
**Review required:** no

### Recommendation (do this)

**Gate `Schema.Create`:** in `internal/platform/db/db.go`, only call `client.Schema.Create(ctx)` when `APP_ENV` is empty/`development`/`local`/`test` — **not** when `APP_ENV=production`.

- Add `APP_ENV` to `config.Config` + `.env.example` (default `development`).
- Pass flag into `OpenPostgres` or read env inside `db` package (prefer config plumb from both mains).
- Document in `README.md` + `AGENTS.md`: production must apply versioned migrations; **Atlas is next step** (link Ent docs). Optionally add stub dir `migrations/README.md` saying “not wired yet”.

### Explicitly defer (do not do in this pass)

- `atlas.hcl`, `migrations/*.sql` generation, CI `atlas migrate apply`, replacing Create entirely for local — too many moving parts for one session.

### Files

- `internal/platform/db/db.go`
- `internal/config/config.go`
- `cmd/api/main.go`, `cmd/worker/main.go`
- `.env.example`
- `README.md`, `AGENTS.md`
- `migrations/README.md` (**new**, placeholder)

### Commits

1. `feat: skip Ent Schema.Create when APP_ENV=production`
2. `docs: document migration gate and Atlas as follow-up`

### Acceptance

- [ ] Local default still auto-migrates
- [ ] `APP_ENV=production` skips Create (API starts only if schema already present — document)
- [ ] Design doc trade-off remains honest

---

## WP11 — P2-2: Minimal Prometheus metrics

**Effort:** S–M  
**Depends on:** WP5 nice (middleware)  
**Review required:** no

### Recommendation (do this — minimal)

Ship `/metrics` with:

| Metric | Type | Labels |
|--------|------|--------|
| `http_requests_total` | Counter | `method`, `path` (low-cardinality: use route template if easy, else path carefully), `status` |
| `github_errors_total` | Counter | `kind` (`rate_limited`, `not_found`, `unauthorized`, `server`, `network`) |

**Skip this pass:** OpenTelemetry tracing, histogram RED, Rabbit queue depth gauges, Grafana dashboards.

### Files

- `go.mod` / `go.sum` — `github.com/prometheus/client_golang`
- `internal/metrics/metrics.go` (**new**) — register counters
- `internal/httpapi/accesslog.go` or metrics middleware — increment HTTP counter
- `internal/githubclient/client.go` — increment on classified errors (or service layer)
- `internal/httpapi/router.go` — `r.GET("/metrics", gin.WrapH(promhttp.Handler()))`
- `README.md` — one line scrape hint

### Commits

1. `chore: add prometheus client dependency`
2. `feat: expose /metrics with HTTP and GitHub error counters`

### Acceptance

- [ ] `curl localhost:8080/metrics` shows both series after traffic
- [ ] No high-cardinality raw URLs with ids if avoidable (prefer `:id` templates via Gin `FullPath()`)
- [ ] `go test ./...`

### Skip criterion

Only skip if dependency/policy blocks Prometheus — then leave a README “metrics: deferred” note. **Default: implement minimal.**

---

## WP12 — P2-3: DLQ replay tooling

**Effort:** S  
**Depends on:** WP1, WP9 (stable DLQ semantics)  
**Review required:** no

### Target

Document + thin helper:

**Option A (prefer docs-only for take-home):** README section with `rabbitmqadmin` / Management UI steps: get from `repo.refresh.dlq`, publish to `repo.jobs` RK `refresh` with body intact; reset job row to `pending` if marked failed (SQL or small endpoint).

**Option B (small command):** `cmd/dlqreplay/main.go` — consume N messages from DLQ, republish to main exchange, optional `--ack-dlq`.

### Files

- `README.md` (Option A) **or** `cmd/dlqreplay/main.go` + Makefile target (Option B)
- Prefer **Option A + SQL snippet** to reset `refresh_batch_jobs.status` unless Option B is quick

### Commits

1. `docs: add DLQ replay runbook`  
   — or — `feat: add dlqreplay command` + `docs: how to replay DLQ`

### Acceptance

- [ ] Operator can move a parked message back to work queue without code archaeology
- [ ] Notes idempotent consumer + need to reset failed job row if already marked failed

---

## Cross-cutting test / Review checklist

Before calling the backlog done, Review lane must verify:

- [ ] Concurrent `POST /api/repos` → one row + **409**
- [ ] Cache single-flight + invalidate on refresh; Lua lock safe
- [ ] Kick/fan-out + confirms: no silent orphan pending
- [ ] Ack only after successful terminal side-effect (success DB update or successful DLQ publish)
- [ ] Rate-limit ≠ retry budget
- [ ] Topology retry frees worker slots; maxRetries via x-death
- [ ] Shutdown: API drain; worker nack/redeliver in-flight
- [ ] `APP_ENV=production` does not Schema.Create
- [ ] Compose `full` profile boots; `infra` still valid
- [ ] `/metrics` present if WP11 shipped

## Suggested total commit count

~28–35 small commits across WP1–WP12 (ranges above). Prefer more small commits over squashing across work packages.

## Out of scope (conscious keep)

- End-user auth
- Offset pagination
- Redis as queue
- Full Atlas migrate CI (documented follow-up only)
- OpenTelemetry / full RED metrics
- Multi-level retry TTLs (1s/5s/30s queues) — single retry queue is enough
)
