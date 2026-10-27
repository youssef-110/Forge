"""Pure ES request builders + RRF fusion. Access control lives here: every query carries a collection filter."""

from typing import Any

SOURCE = ["title", "body", "collection", "tags"]
RRF_K = 60  # standard constant from Cormack et al.; dampens the weight of top ranks


def collection_filter(collections: list[str]) -> dict:
    return {"terms": {"collection": collections}}


def bm25_request(q: str, collections: list[str], size: int) -> dict[str, Any]:
    return {
        "size": size,
        "source": SOURCE,
        "query": {
            "bool": {
                "must": {"multi_match": {"query": q, "fields": ["title^2", "body"]}},
                "filter": [collection_filter(collections)],
            }
        },
        "highlight": {"fields": {"body": {"fragment_size": 160, "number_of_fragments": 1}}},
    }


def knn_request(vector: list[float], collections: list[str], size: int) -> dict[str, Any]:
    # filter inside knn is a pre-filter: restricted docs never enter the candidate set
    return {
        "size": size,
        "source": SOURCE,
        "knn": {
            "field": "embedding",
            "query_vector": vector,
            "k": size,
            "num_candidates": max(50, size * 10),
            "filter": [collection_filter(collections)],
        },
    }


def rrf(rankings: list[list[str]], k: int = RRF_K) -> list[tuple[str, float]]:
    """Reciprocal Rank Fusion: score(d) = sum over rankings of 1 / (k + rank). Done in Python so it works on the basic license."""
    scores: dict[str, float] = {}
    for ranking in rankings:
        for rank, doc_id in enumerate(ranking, start=1):
            scores[doc_id] = scores.get(doc_id, 0.0) + 1.0 / (k + rank)
    return sorted(scores.items(), key=lambda kv: (-kv[1], kv[0]))
