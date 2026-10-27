# Postmortem 2026-05-02: retry storm after downstream outage

Tags: postmortem, retries, sev2

- **Date:** 2026-05-02
- **Severity:** SEV2
- **Duration:** 1 hour 12 minutes (14:05 to 15:17 UTC)
- **Impact:** 3,400 jobs went to `dead`; queue latency p95 reached 9 minutes.

## What happened

A dependency used by `flaky`-style integration jobs was down for 6 minutes. At the time,
retries used plain exponential backoff without jitter. Thousands of jobs that failed in
the same second were all retried in the same second, overloading workers and Redis
again on every round.

## Fix

- Switched to exponential backoff with **full jitter** (base 1s, cap 60s).
- Retried the 3,400 dead jobs with a script that looped `POST /v1/jobs/{id}/retry` in batches of 50 (there is no bulk endpoint).
- Added the "Attempt outcomes / s" panel (`forge_jobs_finished_total` by outcome) to the "Forge job queue" dashboard.

## Lessons

Retries without jitter synchronize clients. Watch the `retried` outcome rate, not only the
dead-letter count.
