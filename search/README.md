# search

FastAPI service (port 8090) for document search, an event log, and access control. API: `docs/CONTRACTS.md`.

## Why three databases

| Store | Holds | Why this store |
|---|---|---|
| **Elasticsearch** | Documents (`documents` index): `title`/`body` as `text` with the english analyzer, `collection`/`tags` as `keyword`, `embedding` as `dense_vector` (384 dims, cosine, HNSW) | It is a search engine. BM25 ranking, stemming, highlighting and approximate kNN in one index. Access control is a `terms` filter inside the query (and a kNN pre-filter), so restricted docs never enter scoring. |
| **MongoDB** | Raw append-only events: `document_ingested`, `search_performed` (user, query, mode, latency_ms, result_ids), `auth_failed` | Schemaless, write-heavy, insert-only. Each event type can carry different fields without migrations. Indexed on `ts` for newest-first reads. |
| **MySQL** | `teams`, `users`, `api_keys` (SHA-256 hash only), `collection_access` (team -> collection) | Small relational data with foreign keys and unique constraints, joined on every request. Strong consistency matters here: a revoked key must stop working at once. |

## Search modes

- `bm25`: lexical; best for exact command names and error strings.
- `vector`: kNN over `BAAI/bge-small-en-v1.5` embeddings (fastembed, ONNX, CPU); best for paraphrased questions.
- `hybrid` (default): runs both and fuses them with Reciprocal Rank Fusion, `1/(60 + rank)`. RRF is computed in Python (`app/query.py`) so it works on the free basic license (ES's built-in `rrf` is a paid feature on 8.15).

## Layout

- `app/query.py`: pure ES request builders and RRF.
- `app/stores.py`: one small repository per database. Routes only use these, so tests swap in in-memory fakes.
- `app/main.py`: `create_app(docs, events, users, embedder)`. Run with `uvicorn app.main:create_app --factory`.
- `seed.py`: idempotent. Waits for all three stores, applies `schema.sql`, creates the demo users and keys, then indexes `docs/runbooks/*.md` (collection `runbooks`) and `docs/runbooks/incidents/*.md` (collection `incidents`).

## Run

```
# tests (no services needed)
py -3.13 -m venv .venv && .venv/Scripts/python -m pip install -r requirements-dev.txt
.venv/Scripts/python -m pytest -q

# image (build from the repo root so the corpus is baked in)
docker build -f search/Dockerfile -t forge-search .
docker run --rm <env> forge-search python seed.py
```

Env: `ES_URL`, `MONGO_URL`, `MONGO_DB`, `MYSQL_URL`, `PORT` (defaults in `app/config.py`). `DOCS_DIR` overrides the corpus path for `seed.py`.
