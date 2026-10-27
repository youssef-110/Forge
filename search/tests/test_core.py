import hashlib

from app.auth import Principal, has_role, hash_key
from app.query import bm25_request, knn_request, rrf


def test_hash_key_is_sha256_hex_and_not_plaintext():
    h = hash_key("demo-admin-key")
    assert len(h) == 64 and h != "demo-admin-key"
    assert h == hashlib.sha256(b"demo-admin-key").hexdigest()
    assert hash_key("a") != hash_key("b")


def test_role_hierarchy():
    p = lambda r: Principal("u", r, "t")
    assert has_role(p("admin"), "admin")
    assert not has_role(p("operator"), "admin")
    assert not has_role(p("viewer"), "operator")
    assert has_role(p("operator"), "viewer")


def test_rrf_rewards_agreement_and_is_deterministic():
    fused = rrf([["a", "b", "c"], ["b", "c", "d"]], k=60)
    ids = [i for i, _ in fused]
    assert ids[0] == "b"  # rank 2 + rank 1 beats rank 1 + absent
    assert set(ids) == {"a", "b", "c", "d"}
    assert abs(dict(fused)["b"] - (1 / 62 + 1 / 61)) < 1e-12
    assert rrf([["x", "y"], ["y", "x"]]) == [("x", 1 / 61 + 1 / 62), ("y", 1 / 61 + 1 / 62)]  # ties break by id


def test_rrf_empty():
    assert rrf([[], []]) == []


def test_bm25_request_filters_collections_in_query():
    r = bm25_request("dead letter", ["runbooks"], 5)
    assert r["size"] == 5
    assert r["query"]["bool"]["filter"] == [{"terms": {"collection": ["runbooks"]}}]
    assert r["query"]["bool"]["must"]["multi_match"]["query"] == "dead letter"


def test_knn_request_prefilters_collections():
    r = knn_request([0.1] * 384, ["runbooks", "incidents"], 5)
    assert r["knn"]["filter"] == [{"terms": {"collection": ["runbooks", "incidents"]}}]
    assert r["knn"]["k"] == 5 and r["knn"]["num_candidates"] >= 5
    assert r["knn"]["field"] == "embedding"
    assert "query" not in r  # pure kNN; no post-filtered query clause
