"""Forge agent: MCP server, LangGraph agent and guards."""

from dotenv import find_dotenv, load_dotenv

# Picks up the repo-root .env (GROQ_API_KEY, MODEL, ...) when run from agent/ or the root.
# Real environment variables win over the file.
load_dotenv(find_dotenv(usecwd=True))
