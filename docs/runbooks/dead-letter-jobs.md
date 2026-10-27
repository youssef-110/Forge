# Dead-letter jobs: inspect and retry

Tags: queue, retries, dead-letter

A job moves to the `dead` state when an attempt fails and `attempts` has reached
`max_attempts` (default **3**). Dead jobs are never retried automatically. They stay in
Redis until an operator retries them; finished jobs are not expired.

## Inspect

1. List the newest dead jobs: `GET /v1/jobs?state=dead&limit=20` (newest first, `limit` 1-100).
2. Read `last_error` on each job. Group by error before retrying anything.
3. Check `GET /v1/stats` for the overall `dead` count.

If every dead job shares the same `last_error` (for example a downstream timeout), fix the
cause first. Retrying into a broken dependency only produces more dead jobs.

## Retry

- Retry one job: `POST /v1/jobs/{id}/retry`.
- Retry is only allowed from `dead` or `canceled`. Any other state returns **409**.
- A retried job goes back to `queued` with `attempts` reset to 0, keeping its original priority.
- There is no bulk retry endpoint. To retry many jobs, loop over the ids and call
  `POST /v1/jobs/{id}/retry` for each, in batches of at most **50**, and watch the
  "Attempt outcomes / s" panel for 5 minutes before the next batch.

Who can retry: `operator` and `admin` roles (`retry_job` tool). Viewers cannot.

## Alert

`ForgeDeadLetterSpike` fires when more than **25** jobs are dead-lettered within
**15 minutes**:

```
sum(increase(forge_jobs_finished_total{outcome="dead"}[15m])) > 25
```
