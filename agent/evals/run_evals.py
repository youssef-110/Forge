"""Run the golden set against the live stack.

python -m evals.run_evals [--category args] [--limit 5] [--concurrency 4] [--no-judge]
Writes evals/results/<timestamp>.json and appends a summary row to evals/history.jsonl.
"""

import argparse
import asyncio
import json
import os
import subprocess
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from evals.scorer import CATEGORIES, aggregate, judge_case, load_cases, score_case
from forge_agent.graph import DEFAULT_MODEL, load_model, run_agent

EVALS = Path(__file__).resolve().parent


def git_sha() -> str | None:
    try:
        return subprocess.run(["git", "rev-parse", "--short", "HEAD"], cwd=EVALS, capture_output=True,
                              text=True, timeout=5, check=True).stdout.strip() or None
    except (OSError, subprocess.SubprocessError):
        return None


async def run_case(case: dict[str, Any], model: Any, judge: Any, sem: asyncio.Semaphore) -> dict[str, Any]:
    async with sem:
        t0 = time.perf_counter()
        try:
            result = await asyncio.wait_for(run_agent(case["input"], case["api_key"], model), timeout=180)
        except Exception as e:  # one broken case must not abort the run
            result = {"error": f"{type(e).__name__}: {e}", "latency_s": round(time.perf_counter() - t0, 3)}
        verdict = await judge_case(judge, case, result) if judge and not result.get("error") else None
        scored = score_case(case, result, verdict)
        mark = "PASS" if scored["passed"] else "FAIL"
        print(f"{mark} {case['id']:<8} {case['category']:<22} {result.get('latency_s', 0):>6}s", flush=True)
        return {**scored, "input": case["input"], "result": result}


async def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--category", choices=CATEGORIES)
    p.add_argument("--limit", type=int)
    p.add_argument("--concurrency", type=int, default=4)
    p.add_argument("--no-judge", action="store_true")
    p.add_argument("--golden", default=str(EVALS / "golden.jsonl"))
    p.add_argument("--min-pass-rate", type=float, help="exit 1 if pass_rate is below this (CI gate)")
    a = p.parse_args()

    cases = [c for c in load_cases(a.golden) if not a.category or c["category"] == a.category][: a.limit]
    model_name = os.getenv("MODEL", DEFAULT_MODEL)
    judge_name = None if a.no_judge else os.getenv("JUDGE_MODEL", model_name)
    model = load_model(model_name)
    judge = load_model(judge_name) if judge_name else None
    sem = asyncio.Semaphore(a.concurrency)
    rows = await asyncio.gather(*(run_case(c, model, judge, sem) for c in cases))

    metrics = aggregate(rows)
    ts = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    meta = {"timestamp": ts, "git_sha": git_sha(), "model": model_name, "judge_model": judge_name,
            "filter": {"category": a.category, "limit": a.limit}}
    (EVALS / "results").mkdir(exist_ok=True)
    (EVALS / "results" / f"{ts}.json").write_text(json.dumps({**meta, "metrics": metrics, "cases": rows},
                                                             indent=2, default=str), encoding="utf-8")
    with open(EVALS / "history.jsonl", "a", encoding="utf-8") as f:
        f.write(json.dumps({**meta, **metrics}) + "\n")
    print(json.dumps(metrics, indent=2))
    if a.min_pass_rate is not None and (metrics["pass_rate"] or 0) < a.min_pass_rate:
        raise SystemExit(f"pass_rate {metrics['pass_rate']} < {a.min_pass_rate}")


if __name__ == "__main__":
    asyncio.run(main())
