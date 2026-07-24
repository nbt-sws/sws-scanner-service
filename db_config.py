"""Shared database configuration for the TCG scripts.

Reads DATABASE_URL from the environment, falling back to the local
.gitignored `.env` file. Never hardcode credentials in source files.
"""
import os
from pathlib import Path


def get_db_url() -> str:
    url = os.environ.get("DATABASE_URL")
    if url:
        return url

    env_file = Path(__file__).resolve().parent / ".env"
    if env_file.exists():
        for line in env_file.read_text(encoding="utf-8").splitlines():
            line = line.strip()
            if line.startswith("DATABASE_URL="):
                return line.split("=", 1)[1].strip().strip('"').strip("'")

    raise RuntimeError(
        "DATABASE_URL is not set. Export it or add it to the local .env file."
    )
