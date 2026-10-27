# Stuck running jobs and the visibility timeout

Tags: queue, reaper, workers

A worker that claims a job marks it `running` with a visibility deadline of
`VISIBILITY_TIMEOUT` (default **30s**) and sends a heartbeat every
`VISIBILITY_TIMEOUT / 3` (**10s** at the default) that pushes the deadline forward.
If the worker dies, the heartbeats stop and the reaper reclaims the job.

## How the reaper works

- The reaper runs on every scheduler tick (`SCHEDULER_TICK`, default **100ms**).
- It runs only on the scheduler leader: the `forge-api` replica holding the Redis lease
  `forge:leader` (TTL **5s**). If the leader dies, another API replica takes the lease
  within about 5s.
- A job whose deadline has passed is moved to `retrying` with normal backoff, or to
  `dead` if it was on its last attempt.
- A reaped run counts as an attempt, and `last_error` is set to
  `visibility timeout: worker stopped heartbeating`.
- Every reaped job increments `forge_jobs_reaped_total`.

## Diagnose

1. `GET /v1/jobs?state=running&limit=20` and compare `updated_at` with now.
2. Check that a leader exists: `sum(forge_scheduler_leader)` must be 1. The
   `ForgeNoSchedulerLeader` alert fires when it is 0 for 1 minute.
3. Check that `forge-api` has running pods: `kubectl -n forge get deploy forge-api`.
4. Look for `reaped job` warnings in the API logs:
   `kubectl -n forge logs deploy/forge-api | grep "reaped job"`.

## Fix

- No leader: make sure `forge-api` has at least one ready replica
  (`kubectl -n forge scale deployment/forge-api --replicas=2`) and that it can reach Redis.
- To stop a specific stuck job immediately: `POST /v1/jobs/{id}/cancel`.
  Cancel works from `queued`, `retrying` or `running` and returns **409** if the job
  already finished.

Jobs must be idempotent: the reaper can run the same job twice if a slow worker
finishes after its job was reclaimed.
