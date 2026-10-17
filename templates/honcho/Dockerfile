# Honcho v3.1.2, pinned to its multi-platform manifest (amd64 and arm64).
# Its default command skips migrations and starts only the API. Cubeship
# has no command override, so the same wrapper selects API or deriver.
FROM ghcr.io/plastic-labs/honcho:v3.3.0@sha256:5bb8cf7a60845e08f83fed66f01907afa0969dd1d02e414ed576102f3b12dfe8
COPY --chown=app:app start.py /app/cubeship-start.py
ENTRYPOINT ["/app/.venv/bin/python", "/app/cubeship-start.py"]
CMD []
