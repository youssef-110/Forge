# Postmortem 2026-03-14: Redis eviction deleted queued jobs

Tags: postmortem, redis, sev1

- **Date:** 2026-03-14
- **Severity:** SEV1
- **Duration:** 47 minutes (09:12 to 09:59 UTC)
- **Impact:** 1,240 queued jobs were deleted and never ran.

## What happened

A bulk backfill submitted about 30,000 synthgen jobs at priority 1. Redis reached
`maxmemory`. The production Redis had `maxmemory-policy allkeys-lru`, so Redis evicted
keys instead of rejecting writes. Evicted keys included 1,240 queued jobs.

## Detection

Users reported jobs returning 404 on `GET /v1/jobs/{id}`. There was no Redis memory alert.

## Fix

- Set `maxmemory-policy noeviction` in docker-compose and the Helm chart, so a full
  Redis rejects new jobs with an error instead of deleting existing ones.
- Moved bulk backfills to smaller batches.

## Follow-ups

- Add a Redis memory alert (not done yet).
- Add cleanup of old finished jobs, which are currently kept forever (not done yet).

## Lessons

Losing a write loudly is better than losing data silently. The 1,240 jobs were
resubmitted from the backfill script logs.
