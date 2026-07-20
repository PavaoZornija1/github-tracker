## Summary

<!-- What changed and why (1–3 bullets). -->

-

## Test plan

- [ ] `go test ./...` (or targeted packages listed below)
- [ ] Manual / Compose check if behavior is user-visible:

```bash
# optional
make compose-up-full
curl -s localhost:8080/readyz
```

## Validate AI output (from AGENTS.md)

Before merge, re-read anything this PR could have broken:

- [ ] Unique constraint / **409** mapping (concurrent duplicate `POST /api/repos`)
- [ ] Ack-after-commit and idempotent batch counters (no double-count on redelivery)
- [ ] Lock TTL vs holder-died path; refresh still invalidates cache
- [ ] Shutdown ordering (API drain; worker nack/redeliver; no lost unacked jobs)
- [ ] Every GitHub failure mode → correct HTTP status or retry/DLQ decision
- [ ] Error envelope still `{ "error": { "code", "message" } }` if API errors changed
- [ ] Redis not used as a job queue

N/A is fine — mark N/A and say why in a bullet under the item.

## Notes for reviewers

<!-- Invariants touched, residual risk, follow-ups. -->
