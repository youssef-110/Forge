"""In-memory stand-ins for the three stores and the embedder."""

import hashlib
from typing import Any

from app.auth import Principal, hash_key

PRINCIPALS = {
    "demo-admin-key": Principal("alice", "admin", "platform", ["incidents", "runbooks"]),
    "demo-operator-key": Principal("bob", "operator", "platform", ["incidents", "runbooks"]),
    "demo-viewer-key": Principal("carol", "viewer", "research", ["runbooks"]),
}


class FakeUsers:
    def __init__(self) -> None:
        self.by_hash = {hash_key(k): p for k, p in PRINCIPALS.items()}

    def principal(self, key_hash: str) -> Principal | None:
        return self.by_hash.get(key_hash)


class FakeEvents:
    def __init__(self) -> None:
        self.items: list[dict[str, Any]] = []

    def append(self, event: dict[str, Any]) -> None:
        self.items.append({**event, "ts": f"t{len(self.items)}"})

    def recent(self, limit: int) -> list[dict[str, Any]]:
        return list(reversed(self.items))[:limit]


class FakeDocs:
    """Honours the collection terms filter and ranks by naive term overlap (bm25) or vector dot product (knn)."""

    def __init__(self) -> None:
        self.docs: dict[str, dict[str, Any]] = {}
        self.requests: list[dict[str, Any]] = []

    def put(self, doc_id: str, doc: dict[str, Any], refresh: bool = True) -> None:
        self.docs[doc_id] = doc

    def search(self, request: dict[str, Any]) -> list[dict[str, Any]]:
        self.requests.append(request)
        if "knn" in request:
            allowed = request["knn"]["filter"][0]["terms"]["collection"]
            qv = request["knn"]["query_vector"]
            score = lambda d: sum(a * b for a, b in zip(qv, d["embedding"]))
        else:
            allowed = request["query"]["bool"]["filter"][0]["terms"]["collection"]
            words = set(request["query"]["bool"]["must"]["multi_match"]["query"].lower().split())
            score = lambda d: len(words & set(f"{d['title']} {d['body']}".lower().split()))
        hits = [
            {"_id": i, "_score": float(score(d)), "_source": d}
            for i, d in self.docs.items()
            if d["collection"] in allowed and score(d) > 0
        ]
        return sorted(hits, key=lambda h: -h["_score"])[: request["size"]]


class FakeEmbedder:
    """Deterministic bag-of-words hashing into 384 dims, so similar texts get similar vectors."""

    def embed(self, texts: list[str]) -> list[list[float]]:
        out = []
        for t in texts:
            v = [0.0] * 384
            for w in t.lower().split():
                v[int(hashlib.md5(w.encode()).hexdigest(), 16) % 384] += 1.0
            out.append(v)
        return out
