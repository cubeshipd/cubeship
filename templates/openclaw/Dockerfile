# The published image starts a gateway that listens on loopback only, and
# refuses to start on an empty state directory. Cubeship runs an image's own
# command, so this image changes only that: a script that configures the
# gateway for a domain, then starts it.
FROM openclaw/openclaw:2026.10.2
COPY --chmod=755 cubeship-start.sh /usr/local/bin/cubeship-start
CMD ["cubeship-start"]
