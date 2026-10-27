# Reading the Forge Grafana dashboard

Tags: observability, grafana, metrics

Dashboard: **Forge job queue** (uid `forge-queue`). Locally Grafana runs at
http://localhost:3000 and Prometheus at http://localhost:9091. Prometheus scrapes
`/metrics` on the API and the workers.

## Panels

- **Queued / Running / Retrying / Dead-lettered**: current counts (`forge_jobs{state}`).
- **Throughput** and **Success ratio (5m)**: completed attempts and the share that succeeded.
- **Jobs by state**: the same counts over time.
- **Attempt outcomes / s**: `forge_jobs_finished_total` by outcome (succeeded, retried,
  dead, canceled). A rise in `retried` usually comes before a dead-letter spike.
- **Job duration by type**: `forge_job_duration_seconds`.
- **Queue wait (submit to first claim)**: `forge_job_queue_wait_seconds`.
- **API latency p95 by route**: `forge_http_request_duration_seconds`.
- **Scheduler**: `forge_scheduler_heap_size` for the `ready` and `delay` heaps.

## Metrics

| Metric | Type | Labels / meaning |
|---|---|---|
| `forge_jobs` | gauge | `state` |
| `forge_jobs_enqueued_total` | counter | `type` |
| `forge_jobs_finished_total` | counter | `type`, `outcome` |
| `forge_job_duration_seconds` | histogram | `type` |
| `forge_job_queue_wait_seconds` | histogram | `type` |
| `forge_http_request_duration_seconds` | histogram | `route`, `code` |
| `forge_scheduler_heap_size` | gauge | `heap` (ready, delay) |
| `forge_scheduler_leader` | gauge | 1 on the replica holding the lease |
| `forge_jobs_reaped_total` | counter | jobs reclaimed by the reaper |

## Alerts (deploy/observability/alerts.yml)

| Alert | Condition |
|---|---|
| `ForgeDeadLetterSpike` | more than 25 jobs dead-lettered in 15 minutes |
| `ForgeNoSchedulerLeader` | `sum(forge_scheduler_leader) == 0` for 1 minute |
| `ForgeQueueBacklog` | more than 1000 queued jobs for 10 minutes |
