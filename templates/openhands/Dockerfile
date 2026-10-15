# The published image writes its API key into the page it serves, which is
# meant for a machine only you can reach: anyone who opens the domain would be
# signed in. This image changes only that, the way `agent-canvas --public`
# does: the page asks for the key instead. The build fails if the entrypoint
# no longer reads the way this expects.
FROM ghcr.io/openhands/agent-canvas:1.24.0
USER root
RUN sed -i 's|--session-api-key "\$EFFECTIVE_SESSION_KEY"|--auth-required|' /opt/agent-canvas/entrypoint.sh \
 && ! grep -q -- '--session-api-key' /opt/agent-canvas/entrypoint.sh
USER openhands
