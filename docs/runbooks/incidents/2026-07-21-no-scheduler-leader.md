# Postmortem 2026-07-21: no scheduler leader, jobs stuck

Tags: postmortem, scheduler, reaper, sev2

- **Date:** 2026-07-21
- **Severity:** SEV2
- **Duration:** 2 hours 4 minutes
- **Impact:** 312 jobs stayed in `running` with no live worker, and new jobs were not
  dispatched.

## What happened

During node maintenance an engineer scaled `deployment/forge-api` to 0 replicas, meaning
to scale `forge-worker`. The scheduler (dispatch, retry promotion and the
visibility-timeout reaper) runs only inside `forge-api`, on the replica holding the Redis
lease `forge:leader`. With no API replica the lease was never acquired:

- Workers kept running but the ready list was never refilled, so new jobs stayed `queued`.
- Jobs whose worker pods were drained stayed `running`, because no reaper ran to reclaim
  them after the 30s visibility timeout.

## Detection

The on-call noticed `running` flat at 312 on the "Forge job queue" dashboard while
throughput was zero.

## Fix

- Scaled `forge-api` back to 2 replicas. A replica took the lease within seconds and the
  reaper reclaimed all 312 jobs on its first ticks.
- Added the `ForgeNoSchedulerLeader` alert: fires when `sum(forge_scheduler_leader) == 0`
  for 1 minute.

## Lessons

The scheduler is a single leader-elected role. Losing every candidate stops the whole
queue even when workers look healthy, so it needs its own alert.
