"""Trend report from evals/history.jsonl -> stdout + evals/REPORT.md (+ evals/trend.png if matplotlib).

python -m evals.report [--last 10]
"""

import argparse
import json
from pathlib import Path
from typing import Any

EVALS = Path(__file__).resolve().parent
COLUMNS = [("pass_rate", "pass"), ("tool_accuracy", "tool acc"), ("refusal_rate", "refusal"),
           ("guardrail_block_rate", "guard block"), ("judge_mean", "judge"), ("latency_p95_s", "p95 s")]


def load_history(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]


def _cell(cur: Any, prev: Any) -> str:
    if cur is None:
        return "-"
    if isinstance(prev, (int, float)) and cur != prev:
        return f"{cur} ({cur - prev:+.3g})"
    return str(cur)


def markdown(rows: list[dict[str, Any]]) -> str:
    if not rows:
        return "# Eval trend\n\nNo runs yet. Run `python -m evals.run_evals` against the live stack.\n"
    head = "| run | sha | model | cases | " + " | ".join(label for _, label in COLUMNS) + " |"
    lines = ["# Eval trend", "", head, "|" + "---|" * (4 + len(COLUMNS))]
    for i, r in enumerate(rows):
        prev = rows[i - 1] if i else {}
        cells = [_cell(r.get(k), prev.get(k)) for k, _ in COLUMNS]
        lines.append(f"| {r['timestamp']} | {r.get('git_sha') or '-'} | {r.get('model')} | {r.get('cases')} | "
                     + " | ".join(cells) + " |")
    latest = rows[-1].get("pass_rate_by_category") or {}
    if latest:
        lines += ["", "Latest run, pass rate by category:", "", "| category | pass |", "|---|---|"]
        lines += [f"| {c} | {v} |" for c, v in latest.items()]
    return "\n".join(lines) + "\n"


def chart(rows: list[dict[str, Any]], out: Path) -> bool:
    try:
        import matplotlib
        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
    except ImportError:
        return False
    fig, ax = plt.subplots(figsize=(8, 4))
    x = [r["timestamp"][4:13] for r in rows]
    for key, label in COLUMNS[:4]:
        ax.plot(x, [r.get(key) for r in rows], marker="o", label=label)
    ax.set_ylim(0, 1.05)
    ax.set_ylabel("rate")
    ax.legend(loc="lower left")
    ax.tick_params(axis="x", rotation=45)
    fig.tight_layout()
    fig.savefig(out, dpi=120)
    plt.close(fig)
    return True


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--last", type=int, default=10)
    rows = load_history(EVALS / "history.jsonl")[-p.parse_args().last:]
    md = markdown(rows)
    print(md)
    (EVALS / "REPORT.md").write_text(md, encoding="utf-8")
    if rows and chart(rows, EVALS / "trend.png"):
        print(f"wrote {EVALS / 'trend.png'}")


if __name__ == "__main__":
    main()
