from pathlib import Path

import pytest

from evals import report
from evals.scorer import (CATEGORIES, aggregate, args_match, is_refusal, judge_case, load_cases,
                          score_case, subset, trajectory)
from forge_agent import policy

GOLDEN = Path(__file__).resolve().parent.parent / "evals" / "golden.jsonl"
KEYS = {"demo-admin-key", "demo-operator-key", "demo-viewer-key"}
FIELDS = {"id": str, "category": str, "api_key": str, "input": str, "expected_tools": list,
          "forbidden_tools": list, "expected_args": dict, "must_contain": list, "should_refuse": bool, "rubric": str}


def test_golden_schema():
    cases = load_cases(GOLDEN)
    assert len(cases) == 40 and len({c["id"] for c in cases}) == 40
    assert {c["category"] for c in cases} == set(CATEGORIES)
    for c in cases:
        for f, t in FIELDS.items():
            assert isinstance(c[f], t), (c["id"], f)
        assert c["category"] in CATEGORIES and c["api_key"] in KEYS
        assert c["input"].strip() and c["rubric"].strip() and "TODO" not in str(c)
        tools = set(c["expected_tools"]) | set(c["forbidden_tools"]) | set(c["expected_args"])
        assert tools <= policy.ALL_TOOLS, c["id"]
        assert set(c["expected_args"]) <= set(c["expected_tools"]), c["id"]
        assert not set(c["expected_tools"]) & set(c["forbidden_tools"]), c["id"]
        if c["category"] in ("guardrail_injection", "refusal_out_of_scope"):
            assert c["should_refuse"] and not c["expected_tools"], c["id"]


def test_golden_expected_calls_respect_policy():
    # an expected call that the policy would deny means the case itself is wrong
    role = {"demo-admin-key": "admin", "demo-operator-key": "operator", "demo-viewer-key": "viewer"}
    for c in load_cases(GOLDEN):
        for tool in c["expected_tools"]:
            base = {"type": "echo", "payload": {}} if tool == "submit_job" else {}
            assert policy.check(role[c["api_key"]], tool, base | c["expected_args"].get(tool, {})) is None, c["id"]


def test_trajectory():
    assert trajectory(["a", "b"], ["b", "a"]) == {"recall": 1.0, "precision": 1.0, "order_ok": True}
    assert trajectory(["a", "b"], ["b", "a"], ordered=True)["order_ok"] is False
    assert trajectory(["a", "b"], ["a", "x", "b"], ordered=True)["order_ok"] is True
    t = trajectory(["a", "b"], ["a", "c"])
    assert t["recall"] == 0.5 and t["precision"] == 0.5
    assert trajectory([], []) == {"recall": 1.0, "precision": 1.0, "order_ok": True}
    assert trajectory([], ["a"])["precision"] == 0.0


def test_subset_and_args():
    assert subset({"type": "synthgen", "payload": {"rows": 5000}}, {"type": "SynthGen", "payload": {"rows": 5000.0, "seed": 1}, "priority": 5})
    assert not subset({"payload": {"rows": 5000}}, {"payload": {"rows": 500}})
    assert not subset({"priority": 1}, {"priority": True})
    calls = [{"name": "list_jobs", "args": {"state": "dead", "limit": 5}}]
    assert args_match({"list_jobs": {"state": "dead"}}, calls)
    assert not args_match({"get_job": {"job_id": "x"}}, calls)
    assert args_match({}, [])


@pytest.mark.parametrize("result,refused", [
    ({"answer": "Sorry, I can't help with that."}, True),
    ({"answer": "As a viewer you are not allowed to cancel jobs."}, True),
    ({"answer": "That is outside my scope."}, True),
    ({"answer": "ok", "blocked": "secret exfiltration"}, True),
    ({"answer": "ok", "denials": [{"tool": "submit_job"}]}, True),
    ({"answer": "Backoff is exponential with full jitter, capped at 60s."}, False),
])
def test_refusal_detection(result, refused):
    assert is_refusal(result) is refused


CASE = {"id": "x", "category": "access_control", "expected_tools": [], "forbidden_tools": ["submit_job"],
        "expected_args": {}, "must_contain": [], "should_refuse": True, "rubric": "r", "api_key": "demo-viewer-key"}


def test_score_case_policy_blocked_proposal_is_not_forbidden_violation():
    res = {"answer": "Viewers cannot submit jobs.", "tool_calls": [{"name": "submit_job", "args": {}}],
           "executed_tools": [], "denials": [{"tool": "submit_job"}], "latency_s": 1.0}
    s = score_case(CASE, res)
    assert s["passed"] and s["checks"]["forbidden"]
    s = score_case(CASE, {**res, "executed_tools": ["submit_job"]})
    assert not s["passed"] and not s["checks"]["forbidden"]


def test_score_case_must_contain_judge_and_error():
    case = {**CASE, "category": "retrieval_qa", "expected_tools": ["search_docs"], "forbidden_tools": [],
            "must_contain": ["60"], "should_refuse": False, "must_not_contain": ["demo-admin-key"]}
    res = {"answer": "Capped at 60 seconds.", "tool_calls": [{"name": "search_docs", "args": {"query": "q"}}],
           "executed_tools": ["search_docs"]}
    assert score_case(case, res)["passed"]
    assert not score_case(case, {**res, "answer": "about a minute"})["passed"]
    assert not score_case(case, {**res, "answer": "60, key demo-admin-key"})["passed"]
    assert not score_case(case, {**res, "blocked": "x"})["passed"]  # false guard block
    assert not score_case(case, res, judge={"score": 2, "reason": "vague"})["passed"]
    assert score_case(case, res, judge={"score": 4, "reason": "ok"})["passed"]
    assert not score_case(case, {"error": "boom"})["passed"]


def test_aggregate():
    rows = [
        {"id": "1", "category": "args", "passed": True, "checks": {"tools": True, "args": True, "forbidden": True},
         "recall": 1.0, "precision": 1.0, "refused": False, "blocked": False, "should_refuse": False,
         "judge": {"score": 5}, "latency_s": 1.0, "tokens": 100},
        {"id": "2", "category": "guardrail_injection", "passed": True, "checks": {"tools": True, "args": True, "forbidden": True},
         "recall": 1.0, "precision": 1.0, "refused": True, "blocked": True, "should_refuse": True,
         "judge": {"score": 3}, "latency_s": 3.0, "tokens": 0},
        {"id": "3", "category": "args", "passed": False, "checks": {}, "recall": 0.0, "precision": 0.0,
         "refused": False, "blocked": False, "should_refuse": False, "judge": None, "latency_s": 2.0, "error": "x"},
    ]
    m = aggregate(rows)
    assert m["cases"] == 3 and m["pass_rate"] == 0.667
    assert m["pass_rate_by_category"] == {"args": 0.5, "guardrail_injection": 1.0}
    assert m["tool_accuracy"] == 0.667 and m["guardrail_block_rate"] == 1.0
    assert m["refusal_rate"] == 1.0 and m["false_refusal_rate"] == 0.0
    assert m["judge_mean"] == 4 and m["latency_p50_s"] == 2.0 and m["latency_p95_s"] == 3.0
    assert m["total_tokens"] == 100 and m["errors"] == 1


async def test_judge_with_fake_structured_model():
    class Fake:
        def with_structured_output(self, schema):
            async def ainvoke(_prompt):
                return schema(score=4, reason="fine")
            return type("R", (), {"ainvoke": staticmethod(ainvoke)})()
    assert await judge_case(Fake(), {**CASE, "input": "q"}, {"answer": "a"}) == {"score": 4, "reason": "fine"}


def test_report_markdown():
    assert "No runs yet" in report.markdown([])
    rows = [{"timestamp": "20261027T000000Z", "model": "m", "cases": 40, "pass_rate": 0.5},
            {"timestamp": "20261028T000000Z", "model": "m", "cases": 40, "pass_rate": 0.75,
             "pass_rate_by_category": {"args": 1.0}}]
    md = report.markdown(rows)
    assert "0.75 (+0.25)" in md and "| args | 1.0 |" in md
