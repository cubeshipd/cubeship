# The published image starts the interactive chat when given no command,
# and exits without a terminal. Cubeship runs an image's own command, so
# this image changes only that: the gateway, supervised, which keeps the
# container up and brings the dashboard with it.
FROM nousresearch/hermes-agent:v2026.9.24
CMD ["gateway", "run"]
