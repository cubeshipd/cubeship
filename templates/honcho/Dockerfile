# Honcho v3.1.2, pinned to its multi-platform manifest (amd64 and arm64).
# Its default command skips migrations and starts only the API. Cubeship
# has no command override, so the same wrapper selects API or deriver.
FROM ghcr.io/plastic-labs/honcho:v3.7.0@sha256:c504f51e124ee225634251b3c99cdb324667d0e7355982dad0cc7a9fb75d8b81
COPY --chown=app:app start.py /app/cubeship-start.py
ENTRYPOINT ["/app/.venv/bin/python", "/app/cubeship-start.py"]
CMD []
