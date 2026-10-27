"""CLI: python -m forge_agent.chat "how many jobs are dead?" --key demo-operator-key"""

import argparse
import asyncio
import json
import os

from forge_agent.graph import load_model, run_agent


def main() -> None:
    p = argparse.ArgumentParser(description="Ask the Forge agent a question")
    p.add_argument("question")
    p.add_argument("--key", default=os.getenv("FORGE_API_KEY", "demo-viewer-key"), help="Forge API key")
    p.add_argument("--model", default=None, help="init_chat_model id, e.g. groq:llama-3.3-70b-versatile")
    p.add_argument("-v", "--verbose", action="store_true", help="print tool trajectory and metrics")
    a = p.parse_args()
    out = asyncio.run(run_agent(a.question, a.key, load_model(a.model)))
    if out.get("error"):
        raise SystemExit(f"error: {out['error']}")
    print(out["answer"])
    if a.verbose:
        print(json.dumps({k: v for k, v in out.items() if k != "answer"}, indent=2, default=str))


if __name__ == "__main__":
    main()
