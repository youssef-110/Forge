# Operating the search service

Tags: search, elasticsearch, mongodb, mysql

The search service (port 8090) answers `/v1/search` for the agent and humans.

## Data stores

- **Elasticsearch** index `documents`: runbook text (BM25, english analyzer) plus a
  384-dimension `embedding` (bge-small-en-v1.5) for kNN.
- **MongoDB** collection `events`: append-only log of `document_ingested`,
  `search_performed` and `auth_failed` events.
- **MySQL**: users, teams, API keys (stored as SHA-256 hashes) and collection access.

## Access

- `runbooks` collection: readable by the platform and research teams.
- `incidents` collection: platform team only.
- Access is enforced inside the Elasticsearch query, so a restricted document is never
  returned or counted.

## Common tasks

- Re-index the runbooks after editing them: run `python seed.py` (idempotent).
- Check who is calling: `GET /v1/events?limit=50` with an admin key.
- Repeated `auth_failed` events from one key prefix: the caller is using a revoked or
  mistyped key.

## Search modes

`bm25` for exact terms and command names, `vector` for paraphrased questions,
`hybrid` (default) fuses both with Reciprocal Rank Fusion (k=60).
