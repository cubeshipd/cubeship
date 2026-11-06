# Honcho v3.1.2, pinned to its multi-platform manifest (amd64 and arm64).
# Its default command skips migrations and starts only the API. Cubeship
# has no command override, so the same wrapper selects API or deriver.
FROM ghcr.io/plastic-labs/honcho:v3.6.0@sha256:eb906a2d251aa48b22347e4465b40785ea7a84e2bfd1105780416bf1f0121099
COPY --chown=app:app start.py /app/cubeship-start.py
ENTRYPOINT ["/app/.venv/bin/python", "/app/cubeship-start.py"]
CMD []
