# On-call escalation

Tags: on-call, incidents, process

The Forge platform team runs a weekly primary and secondary on-call rotation that
hands over on **Mondays at 10:00 UTC**.

## Severity

| Severity | Meaning | Ack within |
|---|---|---|
| SEV1 | Jobs lost or queue fully down | **5 minutes** |
| SEV2 | Queue degraded (backlog, retry storm), no data loss | 15 minutes |
| SEV3 | Single job type failing, dead-letter spike | next business day |

## Escalation path

1. Page goes to the primary on-call.
2. No ack within **15 minutes**: escalate to the secondary on-call.
3. No ack within **30 minutes**, or any SEV1: page the platform engineering manager.

## During an incident

- Open a thread in **#forge-incidents** and name an incident commander.
- Post an update at least every **30 minutes** for SEV1 and SEV2.
- Priority-9 jobs are allowed for remediation; mention each one in the thread.

## After

A blameless postmortem is required for SEV1 and SEV2 within **5 business days**,
filed under `docs/runbooks/incidents/` with the date in the file name.
