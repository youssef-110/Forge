# Service contracts

Forge is three services plus an agent. This file is the source of truth for how they talk.

| Service | Lang | Port | Owns |
|---|---|---|---|
| `queue` | Go | 8080 | Jobs (Redis), priority queue + retry scheduler, workers |
| `search` | Python / FastAPI | 8090 | Documents (Elasticsearch), raw events (MongoDB), users + access (MySQL) |
| `agent` | Python / LangGraph | stdio MCP | MCP server wrapping queue + search; LangGraph agent; eval harness |

## Queue API (`http://queue:8080`)

Job object:

```json
{
  "id": "01J...",            // ULID-ish string
  "type": "synthgen",        // echo | sleep | flaky | synthgen
  "payload": {},             // type-specific
  "priority": 5,             // 0..9, higher runs first; FIFO within a priority
  "state": "queued",         // queued | running | retrying | succeeded | dead | canceled
  "attempts": 0,
  "max_attempts": 3,
  "last_error": "",
  "result": null,            // set on success
  "created_at": "RFC3339",
  "updated_at": "RFC3339",
  "next_run_at": "RFC3339"   // only while retrying
}
```

Job types:

- `echo` `{ "message": string }` -> result `{ "message": string }`
- `sleep` `{ "ms": int }` -> result `{ "slept_ms": int }`
- `flaky` `{ "fail_rate": float 0..1 }` -> fails randomly; exercises retries
- `synthgen` `{ "rows": int (1..100000), "seed": int }` -> generates synthetic records, result `{ "rows": int, "mean": float, "stddev": float, "p95": float }`

Endpoints:

| Method | Path | Body / query | Returns |
|---|---|---|---|
| POST | `/v1/jobs` | `{type, payload, priority?, max_attempts?}` | 201 job |
| GET | `/v1/jobs/{id}` | | 200 job / 404 |
| GET | `/v1/jobs` | `?state=dead&limit=20` | 200 `{ "jobs": [job] }` newest first |
| POST | `/v1/jobs/{id}/cancel` | | 200 job / 409 if already finished |
| POST | `/v1/jobs/{id}/retry` | | 200 job (only from `dead` or `canceled`) / 409 |
| GET | `/v1/stats` | | 200 `{ "queued": n, "running": n, "retrying": n, "succeeded": n, "dead": n, "canceled": n }` |
| GET | `/healthz`, `/readyz`, `/metrics` | | liveness, readiness (Redis ping), Prometheus |

Errors: `{ "error": "message" }` with 4xx/5xx.

## Search API (`http://search:8090`)

All `/v1` routes need header `X-API-Key`. Keys live in MySQL.

| Method | Path | Who | Notes |
|---|---|---|---|
| GET | `/v1/auth/whoami` | any | `{ "user", "role", "team", "collections": [..] }` |
| POST | `/v1/documents` | admin | `{ id?, title, body, collection, tags? }` -> indexed with BM25 text + dense vector |
| GET | `/v1/search` | any | `?q=&mode=bm25\|vector\|hybrid&k=5` -> `{ "results": [{id,title,snippet,collection,score}] }`, filtered to collections the caller's team may read |
| GET | `/v1/events` | admin | `?limit=50` raw events from MongoDB, newest first |
| GET | `/healthz` | none | |

Roles: `admin`, `operator`, `viewer`.

Seed users (demo only, created by `search/seed.py`):

| user | role | team | API key |
|---|---|---|---|
| alice | admin | platform | `demo-admin-key` |
| bob | operator | platform | `demo-operator-key` |
| carol | viewer | research | `demo-viewer-key` |

Collections: `runbooks` (platform + research can read), `incidents` (platform only).

## Agent tool permissions (enforced in the MCP server and again in the agent guard node)

| Tool | viewer | operator | admin |
|---|---|---|---|
| `search_docs`, `get_job`, `list_jobs`, `queue_stats`, `whoami` | yes | yes | yes |
| `submit_job` | no | priority <= 5 | yes |
| `cancel_job`, `retry_job` | no | yes | yes |
