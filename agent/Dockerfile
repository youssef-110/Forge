# Default: MCP server over streamable HTTP on :8000/mcp.
# Evals / chat reuse the image:  docker run --entrypoint python <img> -m evals.run_evals --no-judge
FROM python:3.12-slim
WORKDIR /app
ENV PYTHONUNBUFFERED=1 MCP_HOST=0.0.0.0 MCP_PORT=8000 \
    QUEUE_URL=http://queue:8080 SEARCH_URL=http://search:8090
COPY pyproject.toml ./
COPY forge_agent ./forge_agent
COPY evals ./evals
RUN pip install --no-cache-dir . && useradd -m forge && chown -R forge /app
USER forge
EXPOSE 8000
ENTRYPOINT ["python", "-m", "forge_agent.mcp_server", "--http"]
