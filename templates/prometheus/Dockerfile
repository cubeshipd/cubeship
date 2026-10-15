# The published image scrapes only itself, from a configuration file, and
# Cubeship cannot mount a file into a container. So this image is that one
# with prometheus.yml in place of the example, run with upstream's own flags.
FROM prom/prometheus:v3.15.0
COPY prometheus.yml /etc/prometheus/prometheus.yml
CMD ["--config.file=/etc/prometheus/prometheus.yml", "--storage.tsdb.path=/prometheus"]
