import time
import uuid
from typing import Annotated, Any, Literal

from fastapi import Depends, FastAPI, Header, HTTPException, Query, Request
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field
from starlette.exceptions import HTTPException as StarletteHTTPException

from .auth import Principal, has_role, hash_key
from .embedder import Embedder, passage
from .query import bm25_request, knn_request, rrf


class DocIn(BaseModel):
    id: str | None = Field(default=None, pattern=r"^[A-Za-z0-9._:-]{1,128}$")
    title: str = Field(min_length=1, max_length=300)
    body: str = Field(min_length=1)
    collection: str = Field(pattern=r"^[a-z0-9_-]{1,64}$")
    tags: list[str] = []


def to_result(hit: dict[str, Any], score: float | None = None) -> dict[str, Any]:
    src = hit["_source"]
    hl = hit.get("highlight", {}).get("body")
    snippet = hl[0] if hl else src["body"][:200]
    return {
        "id": hit["_id"],
        "title": src["title"],
        "snippet": snippet,
        "collection": src["collection"],
        "score": round(hit["_score"] if score is None else score, 6),
    }


def create_app(docs=None, events=None, users=None, embedder: Embedder | None = None) -> FastAPI:
    """Pass fakes in tests. With no args (uvicorn --factory) the real stores and fastembed are used."""
    if docs is None:
        from .embedder import FastEmbedder
        from .stores import DocStore, EventStore, UserStore

        docs, events, users, embedder = DocStore(), EventStore(), UserStore(), FastEmbedder()

    app = FastAPI(title="forge-search")

    @app.exception_handler(StarletteHTTPException)  # base class: also catches router 404/405
    async def error_shape(_: Request, exc: StarletteHTTPException) -> JSONResponse:
        return JSONResponse({"error": exc.detail}, status_code=exc.status_code)

    def principal(x_api_key: Annotated[str | None, Header()] = None) -> Principal:
        if not x_api_key:
            raise HTTPException(401, "missing X-API-Key")
        p = users.principal(hash_key(x_api_key))
        if p is None:
            # log a hash prefix only, so the event log never holds usable key material
            events.append({"type": "auth_failed", "key_prefix": hash_key(x_api_key)[:8]})
            raise HTTPException(401, "invalid API key")
        return p

    def admin(p: Annotated[Principal, Depends(principal)]) -> Principal:
        if not has_role(p, "admin"):
            raise HTTPException(403, "admin role required")
        return p

    @app.get("/healthz")
    def healthz() -> dict:
        return {"status": "ok"}

    @app.get("/v1/auth/whoami")
    def whoami(p: Annotated[Principal, Depends(principal)]) -> dict:
        return {"user": p.user, "role": p.role, "team": p.team, "collections": p.collections}

    @app.post("/v1/documents", status_code=201)
    def ingest(doc: DocIn, p: Annotated[Principal, Depends(admin)]) -> dict:
        doc_id = doc.id or uuid.uuid4().hex
        vec = embedder.embed([passage(doc.title, doc.body)])[0]
        docs.put(doc_id, {"title": doc.title, "body": doc.body, "collection": doc.collection, "tags": doc.tags, "embedding": vec})
        events.append({"type": "document_ingested", "user": p.user, "doc_id": doc_id, "collection": doc.collection})
        return {"id": doc_id, "collection": doc.collection}

    @app.get("/v1/search")
    def search(
        p: Annotated[Principal, Depends(principal)],
        q: Annotated[str, Query(min_length=1, max_length=500)],
        mode: Literal["bm25", "vector", "hybrid"] = "hybrid",
        k: Annotated[int, Query(ge=1, le=50)] = 5,
    ) -> dict:
        start = time.perf_counter()
        results: list[dict[str, Any]] = []
        if p.collections:  # a team with no grants sees nothing; never send an empty terms filter
            if mode == "bm25":
                results = [to_result(h) for h in docs.search(bm25_request(q, p.collections, k))]
            elif mode == "vector":
                vec = embedder.embed([q])[0]
                results = [to_result(h) for h in docs.search(knn_request(vec, p.collections, k))]
            else:
                depth = max(k * 4, 20)  # fuse over a deeper candidate list than we return
                lex = docs.search(bm25_request(q, p.collections, depth))
                sem = docs.search(knn_request(embedder.embed([q])[0], p.collections, depth))
                by_id = {h["_id"]: h for h in sem} | {h["_id"]: h for h in lex}  # prefer lex hit: it has a highlight
                fused = rrf([[h["_id"] for h in lex], [h["_id"] for h in sem]])[:k]
                results = [to_result(by_id[i], s) for i, s in fused]
        latency_ms = round((time.perf_counter() - start) * 1000, 2)
        events.append({
            "type": "search_performed", "user": p.user, "team": p.team, "query": q, "mode": mode, "k": k,
            "latency_ms": latency_ms, "result_ids": [r["id"] for r in results],
        })
        return {"results": results}

    @app.get("/v1/events")
    def list_events(_: Annotated[Principal, Depends(admin)], limit: Annotated[int, Query(ge=1, le=500)] = 50) -> dict:
        return {"events": events.recent(limit)}

    return app
