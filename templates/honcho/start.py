"""Start Honcho only after its dependencies are ready; never log credentials."""

import os
import subprocess
import sys
import time
import urllib.error
import urllib.request


def wait_until_ready(check, description, timeout=180):
    deadline = time.monotonic() + timeout
    print(f"Waiting for {description}...", flush=True)
    while True:
        if check():
            return
        if time.monotonic() >= deadline:
            raise RuntimeError(f"Timed out waiting for {description}")
        time.sleep(2)


def database_ready():
    import psycopg

    # psycopg takes a Postgres URI; the application takes a SQLAlchemy URI.
    uri = os.environ["DB_CONNECTION_URI"].replace(
        "postgresql+psycopg://", "postgresql://", 1
    )
    try:
        with psycopg.connect(uri, connect_timeout=3) as connection:
            connection.execute("SELECT 1")
        return True
    except psycopg.OperationalError:
        return False


def api_ready():
    url = os.environ["CUBESHIP_HONCHO_API_URL"].rstrip("/") + "/health"
    try:
        with urllib.request.urlopen(url, timeout=3) as response:
            return response.status == 200
    except (urllib.error.URLError, TimeoutError):
        return False


def main():
    role = os.environ.get("CUBESHIP_HONCHO_ROLE", "api")
    if role not in ("api", "deriver"):
        raise ValueError("CUBESHIP_HONCHO_ROLE must be api or deriver")

    if role == "api":
        wait_until_ready(database_ready, "PostgreSQL")
        # Only the API migrates. A failed migration must prevent it serving.
        subprocess.run([sys.executable, "scripts/provision_db.py"], check=True)
        command = [
            "/app/.venv/bin/fastapi", "run", "--host", "0.0.0.0",
            "--port", "8000", "--workers", "1", "src/main.py",
        ]
    else:
        # /health becomes reachable after migrations and API lifespan checks.
        wait_until_ready(api_ready, "the Honcho API")
        command = [sys.executable, "-m", "src.deriver"]

    # Hand PID 1 to Honcho so container shutdown reaches the real process.
    os.execv(command[0], command)


if __name__ == "__main__":
    main()
