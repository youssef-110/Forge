import pytest
from fastapi.testclient import TestClient

from app.main import create_app
from tests.fakes import FakeDocs, FakeEmbedder, FakeEvents, FakeUsers

ADMIN, OPERATOR, VIEWER = ({"X-API-Key": k} for k in ("demo-admin-key", "demo-operator-key", "demo-viewer-key"))


@pytest.fixture
def env():
    docs, events = FakeDocs(), FakeEvents()
    client = TestClient(create_app(docs, events, FakeUsers(), FakeEmbedder()))
    for doc in (
        {"id": "dead-letter", "title": "Dead letter jobs", "body": "retry dead jobs with the retry endpoint", "collection": "runbooks"},
        {"id": "redis-oom", "title": "Redis OOM incident", "body": "redis evicted queued jobs, retry dead jobs after", "collection": "incidents"},
    ):
        assert client.post("/v1/documents", json=doc, headers=ADMIN).status_code == 201
    return client, docs, events


def test_healthz_needs_no_key(env):
    client, *_ = env
    assert client.get("/healthz").json() == {"status": "ok"}


def test_missing_and_bad_key_are_401_and_bad_key_is_logged(env):
    client, _, events = env
    r = client.get("/v1/auth/whoami")
    assert r.status_code == 401 and "error" in r.json()
    assert client.get("/v1/auth/whoami", headers={"X-API-Key": "nope"}).status_code == 401
    failed = [e for e in events.items if e["type"] == "auth_failed"]
    assert len(failed) == 1 and "nope" not in str(failed[0])


def test_whoami(env):
    client, *_ = env
    assert client.get("/v1/auth/whoami", headers=VIEWER).json() == {
        "user": "carol", "role": "viewer", "team": "research", "collections": ["runbooks"]
    }


@pytest.mark.parametrize("headers", [OPERATOR, VIEWER])
def test_admin_routes_forbid_non_admins(env, headers):
    client, *_ = env
    doc = {"title": "t", "body": "b", "collection": "runbooks"}
    assert client.post("/v1/documents", json=doc, headers=headers).status_code == 403
    assert client.get("/v1/events", headers=headers).status_code == 403


def test_ingest_embeds_and_logs(env):
    client, docs, events = env
    assert len(docs.docs["dead-letter"]["embedding"]) == 384
    r = client.post("/v1/documents", json={"title": "x", "body": "y", "collection": "runbooks", "tags": ["a"]}, headers=ADMIN)
    assert r.status_code == 201 and len(r.json()["id"]) == 32
    assert [e["type"] for e in events.items].count("document_ingested") == 3


def test_ingest_validates_body(env):
    client, *_ = env
    assert client.post("/v1/documents", json={"title": "", "body": "b", "collection": "runbooks"}, headers=ADMIN).status_code == 422


@pytest.mark.parametrize("mode", ["bm25", "vector", "hybrid"])
def test_viewer_never_sees_incidents(env, mode):
    client, docs, _ = env
    r = client.get("/v1/search", params={"q": "retry dead jobs", "mode": mode}, headers=VIEWER)
    assert r.status_code == 200
    results = r.json()["results"]
    assert results and {x["collection"] for x in results} == {"runbooks"}
    # the restriction is in the ES request itself, not applied after the fact
    for req in docs.requests:
        f = req["knn"]["filter"] if "knn" in req else req["query"]["bool"]["filter"]
        assert f == [{"terms": {"collection": ["runbooks"]}}]


@pytest.mark.parametrize("mode", ["bm25", "vector", "hybrid"])
def test_platform_sees_both_and_result_shape(env, mode):
    client, *_ = env
    results = client.get("/v1/search", params={"q": "retry dead jobs", "mode": mode, "k": 5}, headers=OPERATOR).json()["results"]
    assert {x["id"] for x in results} == {"dead-letter", "redis-oom"}
    assert set(results[0]) == {"id", "title", "snippet", "collection", "score"}


def test_search_logs_event_with_latency_and_ids(env):
    client, _, events = env
    client.get("/v1/search", params={"q": "redis", "mode": "bm25"}, headers=ADMIN)
    e = events.items[-1]
    assert e["type"] == "search_performed" and e["user"] == "alice" and e["query"] == "redis"
    assert e["result_ids"] == ["redis-oom"] and e["latency_ms"] >= 0


def test_search_param_validation(env):
    client, *_ = env
    assert client.get("/v1/search", params={"q": "x", "mode": "fuzzy"}, headers=VIEWER).status_code == 422
    assert client.get("/v1/search", params={"q": "x", "k": 0}, headers=VIEWER).status_code == 422
    assert client.get("/v1/search", headers=VIEWER).status_code == 422


def test_events_newest_first_with_limit(env):
    client, *_ = env
    client.get("/v1/search", params={"q": "redis"}, headers=ADMIN)
    evs = client.get("/v1/events", params={"limit": 2}, headers=ADMIN).json()["events"]
    assert len(evs) == 2 and evs[0]["type"] == "search_performed"
