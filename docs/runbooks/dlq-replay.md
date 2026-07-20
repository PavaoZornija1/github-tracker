# DLQ replay

When a refresh job lands in `repo.refresh.dlq` (permanent failure or exhausted non-rate-limit retries), inspect it, fix the underlying issue, reset the job row if needed, then republish to the main exchange.

## Inspect

RabbitMQ Management UI: http://localhost:15672 (guest/guest) → Queues → `repo.refresh.dlq` → Get messages.

Or with `rabbitmqadmin` (management plugin):

```bash
rabbitmqadmin get queue=repo.refresh.dlq count=1 ackmode=ack_requeue_true
```

Note `x-reason` and `x-attempt` headers plus the JSON body (`job_id`, `batch_id`, `repo_id`).

## Reset failed job row

If the worker already marked the job `failed`, set it back to `pending` before replay (idempotent consumer only processes `pending`):

```sql
UPDATE refresh_batch_jobs
SET status = 'pending', error_reason = NULL, attempt = 0
WHERE id = '<job_id>';
```

## Republish to main queue

Publish the **same body** to exchange `repo.jobs` with routing key `refresh`, and set headers:

- `x-message-type` = `refresh`
- `x-attempt` = `1` (or leave prior attempt if you want the budget to continue)

### Management UI

Exchanges → `repo.jobs` → Publish message → routing key `refresh` → paste body + headers.

### rabbitmqadmin

```bash
# Save payload from get into body.json, then:
rabbitmqadmin publish exchange=repo.jobs routing_key=refresh \
  properties='{"delivery_mode":2,"headers":{"x-message-type":"refresh","x-attempt":1},"content_type":"application/json"}' \
  payload="$(cat body.json)"
```

### curl (HTTP API)

```bash
curl -u guest:guest -H 'content-type: application/json' \
  -X POST 'http://localhost:15672/api/exchanges/%2F/repo.jobs/publish' \
  -d '{
    "properties": {
      "delivery_mode": 2,
      "content_type": "application/json",
      "headers": {"x-message-type": "refresh", "x-attempt": 1}
    },
    "routing_key": "refresh",
    "payload": "{\"job_id\":\"...\",\"batch_id\":\"...\",\"repo_id\":\"...\"}",
    "payload_encoding": "string"
  }'
```

After a successful publish, ack/remove the DLQ copy if you used `ackmode` that requeued, or purge the inspected message.

## Notes

- Consumer handlers are **idempotent**: duplicate refresh deliveries for an already-succeeded job are no-ops.
- Prefer fixing root cause (token, rate limit, bad repo) before mass replay.
- Batch kick repair (without DLQ): `POST /api/batches/{id}/enqueue` re-publishes a kick so still-`pending` jobs are fanned out again.
