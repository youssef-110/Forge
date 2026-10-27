# Postmortem 2026-09-09: priority-9 flood starved normal jobs

Tags: postmortem, priority, sev3

- **Date:** 2026-09-09
- **Severity:** SEV3
- **Duration:** 3 hours 30 minutes
- **Impact:** Priority 0-5 jobs waited up to 3 hours; no data loss.

## What happened

A script submitted 8,000 large synthgen jobs (100000 rows each) at priority 9. Because
higher priority always runs first, every priority 0-5 job waited behind them.

## Fix

- Canceled the remaining priority-9 synthgen jobs and resubmitted them at priority 2.
- Operators are now capped at priority **5**; priorities 6-9 require the admin role.
- Priority 9 is reserved for incident remediation.

## Lessons

Strict priority queues need guard rails. Bulk work belongs at priority 0-2.
