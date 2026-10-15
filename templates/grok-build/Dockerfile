# Grok Build publishes no container image, only a static binary per platform
# at x.ai/cli. This image downloads one pinned release, checks it against the
# checksum below, and runs `grok agent serve`, the long-running WebSocket server,
# instead of the interactive terminal UI.
FROM debian:trixie-20260824-slim

# git and ripgrep are what its tools shell out to; ssh for cloning over SSH.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl git ripgrep openssh-client \
 && rm -rf /var/lib/apt/lists/*

ARG GROK_VERSION=1.0.30
ARG TARGETARCH
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) arch=x86_64;  sum=504dd6546ab991b75d36698242875ce461489cd1f8cd84285873cb55bd5c7d54 ;; \
      arm64) arch=aarch64; sum=aad8c3c8a8b294c21377df60d368507f4bcd3530c6c30af10ee02a54cec188cc ;; \
      *) echo "Grok Build has no Linux binary for $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /usr/local/bin/grok "https://x.ai/cli/grok-${GROK_VERSION}-linux-${arch}"; \
    echo "$sum  /usr/local/bin/grok" | sha256sum -c -; \
    chmod 755 /usr/local/bin/grok

# The volume is this user's home: ~/.grok (sessions, memory, config) and the
# repositories the agent works in.
RUN useradd --create-home --home-dir /home/grok --shell /bin/bash --uid 1000 grok
USER grok
ENV HOME=/home/grok
WORKDIR /home/grok

EXPOSE 2419
# The secret comes from GROK_AGENT_SECRET.
CMD ["grok", "agent", "serve", "--bind", "0.0.0.0:2419"]
