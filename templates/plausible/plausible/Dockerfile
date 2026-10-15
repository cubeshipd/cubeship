# The published image's command starts the server and nothing else: the
# compose file Plausible ships creates the databases and migrates them first.
# Cubeship runs an image's own command, so this image is that one with those
# two steps before the server, as upstream runs them.
FROM ghcr.io/plausible/community-edition:v3.2.1
# Compose also raises the open-files limit to 65535, which Cubeship cannot set
# on a container; the shell raises it as far as the host's hard limit allows.
CMD ["sh", "-c", "ulimit -n 65535 2>/dev/null || true; /entrypoint.sh db createdb && /entrypoint.sh db migrate && exec /entrypoint.sh run"]
