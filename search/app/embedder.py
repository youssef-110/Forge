from typing import Protocol

from .config import EMBED_MODEL


class Embedder(Protocol):
    def embed(self, texts: list[str]) -> list[list[float]]: ...


class FastEmbedder:
    """ONNX bge-small via fastembed. Model files come from FASTEMBED_CACHE_PATH (baked into the image)."""

    def __init__(self, model: str = EMBED_MODEL) -> None:
        from fastembed import TextEmbedding  # lazy: tests never load onnxruntime

        self._model = TextEmbedding(model)

    def embed(self, texts: list[str]) -> list[list[float]]:
        return [v.tolist() for v in self._model.embed(texts)]


def passage(title: str, body: str) -> str:
    """The text we embed for a document. Shared by the API and seed so vectors are comparable."""
    return f"{title}\n{body}"
