"""Thin repositories, one per datastore. Routes depend only on these, so tests swap in in-memory fakes."""

from datetime import datetime, timezone
from typing import Any

from elasticsearch import Elasticsearch
from pymongo import DESCENDING, MongoClient
from sqlalchemy import create_engine, text

from . import config
from .auth import Principal

MAPPING = {
    "properties": {
        "title": {"type": "text", "analyzer": "english"},
        "body": {"type": "text", "analyzer": "english"},
        "collection": {"type": "keyword"},
        "tags": {"type": "keyword"},
        "embedding": {"type": "dense_vector", "dims": config.EMBED_DIMS, "index": True, "similarity": "cosine"},
        "indexed_at": {"type": "date"},
    }
}


def now() -> datetime:
    return datetime.now(timezone.utc)


class DocStore:
    """Elasticsearch: the document store. BM25 over english-analyzed text + HNSW kNN over `embedding`."""

    def __init__(self, url: str = config.ES_URL, index: str = config.INDEX) -> None:
        self.es = Elasticsearch(url)
        self.index = index

    def ensure_index(self) -> None:
        if not self.es.indices.exists(index=self.index):
            self.es.indices.create(index=self.index, mappings=MAPPING, settings={"number_of_replicas": 0})

    def put(self, doc_id: str, doc: dict[str, Any], refresh: bool = True) -> None:
        self.es.index(index=self.index, id=doc_id, document={**doc, "indexed_at": now()}, refresh=refresh)

    def refresh(self) -> None:
        self.es.indices.refresh(index=self.index)

    def search(self, request: dict[str, Any]) -> list[dict[str, Any]]:
        return self.es.search(index=self.index, **request)["hits"]["hits"]


class EventStore:
    """MongoDB: raw append-only event log. Schemaless, write-heavy, never updated."""

    def __init__(self, url: str = config.MONGO_URL, db: str = config.MONGO_DB) -> None:
        self.col = MongoClient(url, serverSelectionTimeoutMS=3000)[db]["events"]

    def ensure_indexes(self) -> None:
        self.col.create_index([("ts", DESCENDING)])

    def append(self, event: dict[str, Any]) -> None:
        self.col.insert_one({**event, "ts": now()})

    def recent(self, limit: int) -> list[dict[str, Any]]:
        out = []
        for e in self.col.find({}, {"_id": 0}).sort("ts", DESCENDING).limit(limit):
            e["ts"] = e["ts"].replace(tzinfo=timezone.utc).isoformat()
            out.append(e)
        return out


class UserStore:
    """MySQL: relational identity + access data (users, teams, api_keys, collection_access)."""

    def __init__(self, url: str = config.MYSQL_URL) -> None:
        self.engine = create_engine(url, pool_pre_ping=True)

    def principal(self, key_hash: str) -> Principal | None:
        with self.engine.connect() as c:
            row = c.execute(
                text(
                    "SELECT u.name, u.role, t.name, t.id FROM api_keys k "
                    "JOIN users u ON u.id = k.user_id JOIN teams t ON t.id = u.team_id "
                    "WHERE k.key_hash = :h AND k.revoked_at IS NULL"
                ),
                {"h": key_hash},
            ).first()
            if row is None:
                return None
            cols = c.execute(
                text("SELECT collection FROM collection_access WHERE team_id = :t ORDER BY collection"), {"t": row[3]}
            ).scalars().all()
        return Principal(user=row[0], role=row[1], team=row[2], collections=list(cols))
