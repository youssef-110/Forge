# Retry backoff policy

Tags: queue, retries, backoff

When a job attempt fails and `attempts < max_attempts`, the job enters the `retrying`
state and `next_run_at` is set. The scheduler leader promotes it back to `queued` on the
first scheduler tick after `next_run_at` has passed (`SCHEDULER_TICK`, default 100ms).

## Formula: exponential backoff with full jitter

```
delay = random_uniform(0, min(cap, base * 2^(attempt - 1)))
base  = 1s   (BACKOFF_BASE)
cap   = 60s  (BACKOFF_CAP)
```

`attempt` is the number of the attempt that just failed (1-based). The upper bound of the
delay doubles each time: 1s, 2s, 4s, 8s, 16s, 32s, then stays at 60s from attempt 7 on.

Full jitter (a random delay between 0 and the bound) spreads retries out so that many jobs
failing at the same moment do not all come back at the same moment. We adopted it after the
2026-05-02 retry storm incident.

## max_attempts

- Default `max_attempts` is **3** (one first try plus two retries).
- Allowed range is 1 to 10; anything else is rejected with 400.
- Set it per job in the submit body: `{"type": "flaky", "payload": {...}, "max_attempts": 5}`.
- After the last failed attempt the job goes to `dead`, not `retrying`.

## Testing retries

Submit a `flaky` job with `{"fail_rate": 0.7}` to watch a job move through
`running -> retrying -> queued -> running` in the dashboard.
