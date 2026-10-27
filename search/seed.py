"""One-shot, idempotent seed: MySQL schema + demo users/keys/access, ES index + runbook corpus, Mongo indexes.

Run: python seed.py   (DOCS_DIR defaults to <repo>/docs/runbooks, which is /docs/runbooks inside the image)
"""

import os
import re
import time
from pathlib import Path
from typing import Callable

from sqlalchemy import text

from app.auth import hash_key
from app.embedder import FastEmbedder, passage
from app.stores import DocStore, EventStore, UserStore

HERE = Path(__file__).resolve().parent
DOCS_DIR = Path(os.getenv("DOCS_DIR", HERE.parent / "docs" / "runbooks"))

TEAMS = ["platform", "research"]
USERS = [  # (name, role, team, demo key) from docs/CONTRACTS.md
    ("alice", "admin", "platform", "demo-admin-key"),
    ("bob", "operator", "platform", "demo-operator-key"),
    ("carol", "viewer", "research", "demo-viewer-key"),
]
ACCESS = [("platform", "runbooks"), ("platform", "incidents"), ("research", "runbooks")]


def wait(name: str, check: Callable[[], object], tries: int = 90, delay: float = 2.0) -> None:
    for i in range(1, tries + 1):
        try:
            check()
            print(f"{name}: ready")
            return
        except Exception as e:  # any connection/protocol error means "not up yet"
            print(f"{name}: waiting ({i}/{tries}) {type(e).__name__}")
            time.sleep(delay)
    raise SystemExit(f"{name} never became ready")


def es_ready(docs: DocStore) -> None:
    if docs.es.cluster.health(wait_for_status="yellow", timeout="5s")["timed_out"]:
        raise RuntimeError("cluster not yellow")


def seed_mysql(users: UserStore) -> None:
    sql = (HERE / "schema.sql").read_text()
    stmts = [s for s in ("\n".join(l for l in sql.splitlines() if not l.startswith("--"))).split(";") if s.strip()]
    with users.engine.begin() as c:
        for s in stmts:
            c.execute(text(s))
        for t in TEAMS:
            c.execute(text("INSERT IGNORE INTO teams (name) VALUES (:t)"), {"t": t})
        team_id = dict(c.execute(text("SELECT name, id FROM teams")).all())
        for name, role, team, key in USERS:
            c.execute(
                text("INSERT INTO users (name, role, team_id) VALUES (:n, :r, :t) AS new "
                     "ON DUPLICATE KEY UPDATE role = new.role, team_id = new.team_id"),
                {"n": name, "r": role, "t": team_id[team]},
            )
            c.execute(
                text("INSERT IGNORE INTO api_keys (user_id, key_hash) SELECT id, :h FROM users WHERE name = :n"),
                {"h": hash_key(key), "n": name},
            )
        for team, col in ACCESS:
            c.execute(text("INSERT IGNORE INTO collection_access (team_id, collection) VALUES (:t, :c)"),
                      {"t": team_id[team], "c": col})
    print(f"mysql: {len(USERS)} users, {len(ACCESS)} access rows")


def load_corpus() -> list[tuple[str, dict]]:
    out = []
    for collection, folder in (("runbooks", DOCS_DIR), ("incidents", DOCS_DIR / "incidents")):
        for path in sorted(folder.glob("*.md")):
            body = path.read_text(encoding="utf-8")
            title = next((l[2:].strip() for l in body.splitlines() if l.startswith("# ")), path.stem)
            m = re.search(r"^Tags:\s*(.+)$", body, re.MULTILINE)
            tags = [t.strip() for t in m.group(1).split(",")] if m else []
            out.append((path.stem, {"title": title, "body": body, "collection": collection, "tags": tags}))
    return out


def main() -> None:
    docs, events, users = DocStore(), EventStore(), UserStore()
    wait("elasticsearch", lambda: es_ready(docs))
    wait("mongo", lambda: events.col.database.client.admin.command("ping"))
    wait("mysql", lambda: users.engine.connect().close())

    seed_mysql(users)
    events.ensure_indexes()

    corpus = load_corpus()
    if not corpus:
        raise SystemExit(f"no markdown found under {DOCS_DIR}")
    vectors = FastEmbedder().embed([passage(d["title"], d["body"]) for _, d in corpus])
    docs.ensure_index()
    for (doc_id, d), vec in zip(corpus, vectors):
        docs.put(doc_id, {**d, "embedding": vec}, refresh=False)  # fixed ids: re-running overwrites, no dupes
    docs.refresh()
    print(f"elasticsearch: indexed {len(corpus)} docs into '{docs.index}'")


if __name__ == "__main__":
    main()
