"""MCP server exposing the Forge queue + search services as tools.

Run: python -m forge_agent.mcp_server            (stdio)
     python -m forge_agent.mcp_server --http     (streamable HTTP on :8000/mcp)
"""

import argparse
import json
import os
from typing import Any

import httpx
from mcp.server.fastmcp import FastMCP

from forge_agent import policy

mcp = FastMCP(
    "forge",
    instructions="Tools for the Forge job queue and runbook search. Access is role-based.",
    host=os.getenv("MCP_HOST", "127.0.0.1"),
    port=int(os.getenv("MCP_PORT", "8000")),
)

# Module-level so tests can swap in an httpx.MockTransport client.
http = httpx.AsyncClient(timeout=10.0)
_roles: dict[str, dict[str, Any]] = {}


def _queue() -> str:
    return os.getenv("QUEUE_URL", "http://localhost:8080").rstrip("/")


def _search() -> str:
    return os.getenv("SEARCH_URL", "http://localhost:8090").rstrip("/")


def _key() -> str:
    # shortcut: one key per server process (from env); in --http mode every client shares it.
    # Upgrade: read X-API-Key from the HTTP request context per call.
    return os.getenv("FORGE_API_KEY", "")


async def resolve_identity() -> dict[str, Any]:
    """whoami via the search service, cached per API key (roles change rarely)."""
    key = _key()
    if key not in _roles:
        r = await http.get(f"{_search()}/v1/auth/whoami", headers={"X-API-Key": key})
        r.raise_for_status()
        _roles[key] = r.json()
    return _roles[key]


def _dump(data: Any) -> str:
    return json.dumps(data, indent=2, default=str)


async def _guarded(tool: str, args: dict[str, Any], method: str, url: str, **kw: Any) -> str:
    try:
        role = (await resolve_identity()).get("role")
    except httpx.HTTPStatusError as e:
        return f"error: authentication failed ({e.response.status_code}); check FORGE_API_KEY"
    except httpx.HTTPError as e:
        return f"error: search service unreachable: {e}"
    if reason := policy.check(role, tool, args):
        return f"permission denied: {reason}"
    try:
        headers = {"X-API-Key": _key()}
        if token := os.getenv("QUEUE_TOKEN"):  # queue API_TOKEN on public deploys; search ignores it
            headers["Authorization"] = f"Bearer {token}"
        r = await http.request(method, url, headers=headers, **kw)
    except httpx.HTTPError as e:
        return f"error: service unreachable: {e}"
    if r.is_error:
        try:
            msg = r.json().get("error") or r.json().get("detail") or r.text
        except ValueError:
            msg = r.text
        return f"error {r.status_code}: {msg}"
    return _dump(r.json())


@mcp.tool()
async def whoami() -> str:
    """Return the current caller's user, role, team and readable collections."""
    try:
        return _dump(await resolve_identity())
    except httpx.HTTPError as e:
        return f"error: could not resolve identity: {e}"


@mcp.tool()
async def search_docs(query: str, mode: str = "hybrid", k: int = 5) -> str:
    """Search runbooks/incidents. mode is bm25, vector or hybrid; k is result count (1-20)."""
    args = {"query": query, "mode": mode, "k": k}
    return await _guarded("search_docs", args, "GET", f"{_search()}/v1/search",
                          params={"q": query, "mode": mode, "k": k})


@mcp.tool()
async def submit_job(type: str, payload: dict[str, Any], priority: int = 5, max_attempts: int = 3) -> str:
    """Submit a job. type: echo {message} | sleep {ms} | flaky {fail_rate} | synthgen {rows 1..100000, seed}.
    priority 0-9 (higher runs first; operators max 5)."""
    body = {"type": type, "payload": payload, "priority": priority, "max_attempts": max_attempts}
    return await _guarded("submit_job", body, "POST", f"{_queue()}/v1/jobs", json=body)


@mcp.tool()
async def get_job(job_id: str) -> str:
    """Get one job by id (state, attempts, last_error, result)."""
    return await _guarded("get_job", {"job_id": job_id}, "GET", f"{_queue()}/v1/jobs/{job_id}")


@mcp.tool()
async def list_jobs(state: str | None = None, limit: int = 10) -> str:
    """List jobs newest first, optionally filtered by state
    (queued, running, retrying, succeeded, dead, canceled)."""
    params: dict[str, Any] = {"limit": limit} | ({"state": state} if state else {})
    return await _guarded("list_jobs", {"state": state, "limit": limit}, "GET",
                          f"{_queue()}/v1/jobs", params=params)


@mcp.tool()
async def queue_stats() -> str:
    """Counts of jobs per state."""
    return await _guarded("queue_stats", {}, "GET", f"{_queue()}/v1/stats")


@mcp.tool()
async def cancel_job(job_id: str) -> str:
    """Cancel a queued/running/retrying job."""
    return await _guarded("cancel_job", {"job_id": job_id}, "POST", f"{_queue()}/v1/jobs/{job_id}/cancel")


@mcp.tool()
async def retry_job(job_id: str) -> str:
    """Re-queue a dead or canceled job."""
    return await _guarded("retry_job", {"job_id": job_id}, "POST", f"{_queue()}/v1/jobs/{job_id}/retry")


def main() -> None:
    p = argparse.ArgumentParser(description="Forge MCP server")
    p.add_argument("--http", action="store_true", help="serve streamable HTTP instead of stdio")
    mcp.run(transport="streamable-http" if p.parse_args().http else "stdio")


if __name__ == "__main__":
    main()
