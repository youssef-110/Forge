# Redis memory pressure

Tags: redis, memory, capacity

All job state lives in Redis. If Redis runs out of memory the queue stops accepting jobs.

## Check

```
redis-cli INFO memory | grep -E "used_memory_human|maxmemory_human|maxmemory_policy"
redis-cli --bigkeys
redis-cli ZCARD forge:state:succeeded
```

## Rules

- `maxmemory-policy` must be **noeviction** (set in docker-compose and the Helm chart).
  Any eviction policy (for example `allkeys-lru`) silently deletes job keys, which loses
  queued jobs. This caused the 2026-03-14 incident.
- With `noeviction`, writes fail when memory is full: `POST /v1/jobs` returns 500 with
  an OOM error instead of losing data.
- Finished jobs (`succeeded`, `dead`, `canceled`) are kept; there is no TTL yet.
  Automatic cleanup of old finished jobs is future work, so finished jobs are the main
  source of slow memory growth.

There is no Redis memory alert yet; check memory as part of the weekly on-call handover.

## Remediation

1. Find what is large: a backlog of queued jobs or many old finished jobs.
2. If the backlog is the cause, scale workers (see scaling runbook) to drain it.
3. Raise `maxmemory` only with the platform lead's approval and log it in #forge-incidents.
4. Never run `FLUSHALL` or `FLUSHDB` on the production queue.
