# The published image's entrypoint is `opencode`, which starts the terminal UI
# when given no command and cannot run without a terminal. Cubeship runs an
# image's own command, so this image changes that to the headless server, which
# also serves the web UI, and adds the git the agent works with.
FROM ghcr.io/anomalyco/opencode:2.0.27
RUN apk add --no-cache git openssh-client
# The directory the server works in when a request names none.
WORKDIR /workspace
CMD ["serve", "--hostname", "0.0.0.0", "--port", "4096"]
