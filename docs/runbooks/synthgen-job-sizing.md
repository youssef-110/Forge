# Sizing synthgen jobs

Tags: synthgen, capacity, jobs

`synthgen` generates synthetic log-normal "latency" samples and returns summary
statistics. Payload: `{"rows": int, "seed": int}`. Result: `{"rows", "mean", "stddev", "p95"}`.

## Limits

- `rows` must be between **1** and **100000**. Anything outside is rejected with 400
  at submit time (`rows must be between 1 and 100000`).
- The same `seed` and `rows` always produce the same result, so a synthgen job is safe to
  retry and easy to verify.

## Sizing guide

- The handler is pure Go: it draws log-normal samples and finds p95 with quickselect
  instead of a full sort. A 100000-row job completes in well under a second on one
  worker core.
- For more than 100000 rows, split into several jobs with different seeds
  (for example seeds 1..10 with 100000 rows each for 1M rows) at priority 0-2.

## Example

```
POST /v1/jobs
{"type": "synthgen", "payload": {"rows": 20000, "seed": 42}, "priority": 5}
```
