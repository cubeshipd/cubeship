# The published image runs `inngest` with no command, which prints the help
# and exits. Cubeship runs an image's own command, so this image is that one
# running `inngest start`, the self-hosted server. Its configuration is all
# INNGEST_* variables, set in template.yaml.
FROM inngest/inngest:v1.45.1
CMD ["inngest", "start"]
