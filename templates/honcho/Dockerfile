# Honcho v3.1.2, pinned to its multi-platform manifest (amd64 and arm64).
# Its default command skips migrations and starts only the API. Cubeship
# has no command override, so the same wrapper selects API or deriver.
FROM ghcr.io/plastic-labs/honcho:v3.5.0@sha256:23ff96bf62ab2ca14928e6766db1a30a7729f5b7c9e2285af0cd4a179c0d393d
COPY --chown=app:app start.py /app/cubeship-start.py
ENTRYPOINT ["/app/.venv/bin/python", "/app/cubeship-start.py"]
CMD []
