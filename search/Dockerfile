# Build from the repo root so the runbook corpus is baked in:
#   docker build -f search/Dockerfile -t forge-search .
FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 \
    FASTEMBED_CACHE_PATH=/models PORT=8090
WORKDIR /app

COPY search/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Download the embedding model at build time; runtime never touches the network.
RUN python -c "from fastembed import TextEmbedding; TextEmbedding('BAAI/bge-small-en-v1.5')" \
    && chmod -R a+rX /models
ENV HF_HUB_OFFLINE=1

COPY search/app ./app
COPY search/seed.py search/schema.sql ./
COPY docs/runbooks /docs/runbooks

RUN useradd -r -u 10001 app
USER app
EXPOSE 8090
CMD ["sh", "-c", "exec uvicorn app.main:create_app --factory --host 0.0.0.0 --port ${PORT}"]
