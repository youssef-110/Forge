"""MCP server permission enforcement against mocked queue/search services."""

import json

import httpx
import pytest

from forge_agent import mcp_server

ROLES = {"demo-admin-key": ("alice", "admin"), "demo-operator-key": ("bob", "operator"),
         "demo-viewer-key": ("carol", "viewer")}


@pytest.fixture
def calls(monkeypatch):
    seen: list[httpx.Request] = []

    def handler(req: httpx.Request) -> httpx.Response:
        seen.append(req)
        if req.url.path == "/v1/auth/whoami":
            key = req.headers.get("X-API-Key")
            if key not in ROLES:
                return httpx.Response(401, json={"detail": "bad key"})
            user, role = ROLES[key]
            return httpx.Response(200, json={"user": user, "role": role, "team": "platform", "collections": ["runbooks"]})
        if req.url.path == "/v1/jobs" and req.method == "POST":
            return httpx.Response(201, json={"id": "01J1", "state": "queued", **json.loads(req.content)})
        if req.url.path.endswith("/cancel"):
            return httpx.Response(409, json={"error": "job already finished"})
        if req.url.path == "/v1/stats":
            return httpx.Response(200, json={"queued": 2, "dead": 1})
        if req.url.path == "/v1/search":
            return httpx.Response(200, json={"results": [{"id": "r1", "title": "Retries", "snippet": "cap 60s"}]})
        return httpx.Response(404, json={"error": "not found"})

    monkeypatch.setattr(mcp_server, "http", httpx.AsyncClient(transport=httpx.MockTransport(handler)))
    monkeypatch.setattr(mcp_server, "_roles", {})
    return seen


def _use(monkeypatch, key):
    monkeypatch.setenv("FORGE_API_KEY", key)


def _queue_calls(seen):
    return [r for r in seen if r.url.port == 8080]


async def test_viewer_cannot_submit(monkeypatch, calls):
    _use(monkeypatch, "demo-viewer-key")
    out = await mcp_server.submit_job("echo", {"message": "hi"})
    assert out.startswith("permission denied") and "viewer" in out
    assert not _queue_calls(calls)  # denied before reaching the queue


async def test_operator_priority_cap(monkeypatch, calls):
    _use(monkeypatch, "demo-operator-key")
    out = await mcp_server.submit_job("synthgen", {"rows": 10, "seed": 1}, priority=9)
    assert "permission denied" in out and "priority <= 5" in out
    ok = json.loads(await mcp_server.submit_job("synthgen", {"rows": 10, "seed": 1}, priority=5))
    assert ok["state"] == "queued" and ok["priority"] == 5


async def test_admin_priority_9_and_row_limit(monkeypatch, calls):
    _use(monkeypatch, "demo-admin-key")
    assert json.loads(await mcp_server.submit_job("synthgen", {"rows": 10, "seed": 1}, priority=9))["priority"] == 9
    assert "permission denied" in await mcp_server.submit_job("synthgen", {"rows": 500_000, "seed": 1})


async def test_reads_allowed_for_viewer_and_role_cached(monkeypatch, calls):
    _use(monkeypatch, "demo-viewer-key")
    assert json.loads(await mcp_server.queue_stats())["queued"] == 2
    assert "cap 60s" in await mcp_server.search_docs("retry backoff")
    assert json.loads(await mcp_server.whoami())["role"] == "viewer"
    assert sum(r.url.path == "/v1/auth/whoami" for r in calls) == 1
    assert all(r.headers["X-API-Key"] == "demo-viewer-key" for r in calls)


async def test_service_error_surfaces(monkeypatch, calls):
    _use(monkeypatch, "demo-operator-key")
    assert await mcp_server.cancel_job("01J1") == "error 409: job already finished"
    assert "error 404" in await mcp_server.get_job("missing")


async def test_bad_key(monkeypatch, calls):
    _use(monkeypatch, "nope")
    assert "authentication failed (401)" in await mcp_server.queue_stats()
