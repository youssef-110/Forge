# Forge

An internal job platform with an AI agent operating it. You submit jobs to a Go service. It
queues them in Redis, schedules them by priority with a hand-written heap, retries failures
with backoff, and runs them on a pool of workers. A LangGraph agent, connected over MCP, can
search the runbooks and operate the queue within the limits of its role. An eval harness
measures how well the agent does that.

```
                 ┌──────────────── agent (Python) ────────────────┐
  user ──▶ LangGraph: input guard ─▶ model ─▶ policy guard ─▶ tools │──MCP──▶ forge MCP server
                 └────────────────────────────────────────────────┘            │      │
                                                                     REST      │      │ REST
                    ┌──────────────────────── queue (Go) ─────────────────┐◀───┘      ▼
  POST /v1/jobs ──▶ │ API ─▶ Redis ◀─ scheduler (leader lease)            │   search (FastAPI)
                    │         ▲        ready heap: priority, then FIFO    │   ├ Elasticsearch: BM25 + vectors
                    │         │        delay heap: retries by due time    │   ├ MongoDB: raw events
                    │         └── workers ×N (claim, heartbeat, run)      │   └ MySQL: users, keys, ACLs
                    └─────────────── OTel traces ─▶ Jaeger ; /metrics ─▶ Prometheus ─▶ Grafana ┘
```

| Part | Stack | What it shows |
|---|---|---|
| [`queue/`](queue) | Go, Redis, Lua | REST API, worker pool, **hand-written binary heap** priority queue, **retry scheduler** (delay heap + full-jitter exponential backoff), dead-lettering, visibility-timeout reaper, leader election, quickselect for p95 |
| [`deploy/observability/`](deploy/observability) | OpenTelemetry, Prometheus, Grafana, Jaeger | Traces from the API request through Redis into the worker span. 9 custom metrics, a provisioned dashboard, 3 alert rules |
| [`deploy/helm/`](deploy/helm/forge), [`deploy/kind/`](deploy/kind) | Kubernetes, Helm, kind | 2 API replicas (only one schedules), 2+ worker replicas, probes, PDB, optional HPA, non-root read-only containers |
| [`.github/workflows/`](.github/workflows) | GitHub Actions | Tests for all three services, Helm lint, a real **kind e2e deploy**, and images pushed to GHCR |
| [`search/`](search) | FastAPI, Elasticsearch, MongoDB, MySQL | BM25, kNN vector and hybrid (RRF) retrieval, with access control applied inside the ES query |
| [`agent/`](agent) | LangGraph, MCP, LLM-as-judge | Tool-calling agent, role-based guardrails, prompt-injection guard, a 40-case golden dataset, a scorer, and score history over time |
| [`deploy/cloudrun/`](deploy/cloudrun) | Google Cloud Run, Upstash Redis | Free-tier deploy with keyless GitHub to GCP auth (Workload Identity Federation) |

## Run it

```bash
docker compose up -d --build            # ~3 min first time
cd queue && go run ./cmd/loadgen -rate 20 -duration 1m
```

- Queue API: http://localhost:8080
- Grafana: http://localhost:3000 (dashboard "Forge job queue")
- Jaeger: http://localhost:16686
- Prometheus: http://localhost:9091
- Search API: http://localhost:8090 (demo keys are listed in [docs/CONTRACTS.md](docs/CONTRACTS.md))
- MCP server: http://localhost:8000/mcp

```bash
curl -X POST localhost:8080/v1/jobs -d '{"type":"synthgen","payload":{"rows":50000,"seed":1},"priority":8}'
curl localhost:8080/v1/jobs/<id>
curl "localhost:8080/v1/jobs?state=dead"
curl -H "X-API-Key: demo-viewer-key" "localhost:8090/v1/search?q=retry+backoff&mode=hybrid"
```

Kubernetes (needs kind, helm, kubectl): `bash deploy/kind/up.sh` creates a 3-node cluster, installs the chart and runs a smoke test.

Agent and evals (needs an LLM key; Groq's free tier works):

```bash
cd agent && python -m venv .venv && .venv/Scripts/pip install -e ".[dev]"   # bin/ on Linux/macOS
export GROQ_API_KEY=...
python -m forge_agent.chat --key demo-operator-key "Run a synthgen job with 20000 rows, seed 4, then tell me its p95"
python -m evals.run_evals && python -m evals.report     # appends to evals/history.jsonl, writes evals/REPORT.md
```

## Design notes

**Why a scheduler with heaps instead of just Redis lists?** Workers pull from a short shared
`ready` list. The leader keeps every other waiting job in an in-memory binary heap ordered by
priority, then age. It tops the ready list up to `READY_TARGET` (default 8) each 100ms tick, so
a priority-9 job submitted behind 10,000 priority-1 jobs runs next instead of last.

**Retries.** A failed attempt moves the job to `retrying`, with `next_run_at` set by full-jitter
backoff (`rand(0, min(60s, 1s·2^(n-1)))`). The scheduler holds those jobs in a second heap keyed
by due time and promotes them when due. After `max_attempts` (default 3) the job goes to `dead`
and keeps its `last_error`. `POST /v1/jobs/{id}/retry` revives it.

**Crash safety.**
- Redis is the source of truth. Every state change is one Lua script, so a job is always in
  exactly one state index.
- Workers heartbeat every `VISIBILITY_TIMEOUT/3`. A job whose worker disappears is reaped and
  retried.
- The scheduler runs only on the replica holding a Redis lease. A new leader rebuilds its heaps
  from Redis.
- Duplicate dispatch during a leader handover is harmless, because the claim script only runs
  jobs still in `queued`.
- Canceled jobs are dropped lazily at claim time, not searched for inside the heap.

**Tested:** `go test -race ./...` covers heap ordering against `sort`, quickselect against `sort`,
backoff bounds, priority dispatch order, retry then dead-letter, cancel, reaper, and leader
failover. These run against in-memory Redis (miniredis).

**Agent safety.**
- The same permission table is enforced twice: in the MCP server, so clients can't bypass it,
  and in the graph's policy node, so the model sees the denial and can recover.
  - viewer: read only
  - operator: priority ≤ 5
  - admin: everything
- An input guard blocks common injection and secret-exfiltration patterns before the model runs.
- Search results are filtered by the caller's team inside the Elasticsearch query, so a viewer
  can't retrieve incident postmortems even through the agent.

## Layout

```
queue/      Go service: cmd/forge (api|worker), cmd/loadgen, internal/{pq,backoff,store,scheduler,worker,jobs,api,telemetry}
search/     FastAPI search + seed (indexes docs/runbooks)
agent/      MCP server, LangGraph agent, evals/
deploy/     helm/, kind/, observability/, cloudrun/
docs/       CONTRACTS.md (APIs, roles), runbooks/ (the agent's knowledge base)
```
