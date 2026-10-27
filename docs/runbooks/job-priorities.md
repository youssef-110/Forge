# Job priority levels

Tags: queue, priority, permissions

Priority is an integer from **0 to 9**. Higher numbers run first. Within the same
priority, jobs run in FIFO order (oldest `created_at` first). When `priority` is omitted
on submit, the job gets priority **5**.

## Who may submit what

| Role | submit_job | Max priority |
|---|---|---|
| viewer | no | - |
| operator | yes | **5** |
| admin | yes | 9 |

An operator submitting priority 6 or higher is rejected by the agent guard and the MCP
server before the request reaches the queue.

## Conventions

- **9**: incident remediation only. Must be mentioned in the incident channel.
- **7-8**: time-critical admin work.
- **5**: default for normal work.
- **0-2**: backfills and bulk synthgen that can wait.

High priority starves lower priorities: a steady stream of priority-9 jobs means
priority-0 jobs never run. See the 2026-09-09 incident.

Priority cannot be changed after submit. Cancel and resubmit instead.
