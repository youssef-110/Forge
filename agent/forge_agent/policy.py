"""Tool permission table from docs/CONTRACTS.md plus argument limits.

Shared by the MCP server (hard enforcement) and the LangGraph policy node
(defense in depth, and lets the model see a denial before any network call).
"""

from typing import Any

READ_TOOLS = {"whoami", "search_docs", "get_job", "list_jobs", "queue_stats"}
WRITE_TOOLS = {"submit_job", "cancel_job", "retry_job"}
ALL_TOOLS = READ_TOOLS | WRITE_TOOLS
ROLES = ("viewer", "operator", "admin")

JOB_TYPES = {"echo", "sleep", "flaky", "synthgen"}
OPERATOR_MAX_PRIORITY = 5
MAX_SYNTHGEN_ROWS = 100_000
MAX_ATTEMPTS = 10
MAX_K = 20
MAX_LIMIT = 100
SEARCH_MODES = {"bm25", "vector", "hybrid"}


def _int(v: Any) -> int | None:
    # bool is an int subclass; reject it so `priority=True` doesn't sneak through.
    return v if isinstance(v, int) and not isinstance(v, bool) else None


def check(role: str | None, tool: str, args: dict[str, Any]) -> str | None:
    """Return None if allowed, else a human-readable denial reason."""
    if role not in ROLES:
        return f"unknown role {role!r}; cannot authorize {tool}"
    if tool not in ALL_TOOLS:
        return f"unknown tool {tool!r}"
    if tool in WRITE_TOOLS and role == "viewer":
        return f"role 'viewer' may not call {tool} (read-only access)"

    if tool == "submit_job":
        jtype = args.get("type")
        if jtype not in JOB_TYPES:
            return f"job type must be one of {sorted(JOB_TYPES)}, got {jtype!r}"
        priority = _int(args.get("priority", 5))
        if priority is None or not 0 <= priority <= 9:
            return "priority must be an integer 0..9"
        if role == "operator" and priority > OPERATOR_MAX_PRIORITY:
            return f"role 'operator' may submit with priority <= {OPERATOR_MAX_PRIORITY}, got {priority}"
        attempts = _int(args.get("max_attempts", 3))
        if attempts is None or not 1 <= attempts <= MAX_ATTEMPTS:
            return f"max_attempts must be an integer 1..{MAX_ATTEMPTS}"
        payload = args.get("payload")
        if not isinstance(payload, dict):
            return "payload must be an object"
        if jtype == "synthgen":
            rows = _int(payload.get("rows"))
            if rows is None or not 1 <= rows <= MAX_SYNTHGEN_ROWS:
                return f"synthgen rows must be an integer 1..{MAX_SYNTHGEN_ROWS}"

    if tool == "search_docs":
        if args.get("mode", "hybrid") not in SEARCH_MODES:
            return f"mode must be one of {sorted(SEARCH_MODES)}"
        k = _int(args.get("k", 5))
        if k is None or not 1 <= k <= MAX_K:
            return f"k must be an integer 1..{MAX_K}"

    if tool == "list_jobs":
        limit = _int(args.get("limit", 10))
        if limit is None or not 1 <= limit <= MAX_LIMIT:
            return f"limit must be an integer 1..{MAX_LIMIT}"

    return None
