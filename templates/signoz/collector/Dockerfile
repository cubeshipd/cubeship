# SigNoz's collector, with the configuration its Docker install writes, and
# the schema migrations that install runs in a container of their own run
# first, by start.sh. Cubeship cannot mount a file or run a one-off container.
FROM signoz/signoz-otel-collector:v0.144.12
COPY collector/config.yaml /etc/otel-collector-config.yaml
COPY collector/start.sh /start.sh
ENTRYPOINT ["/bin/sh", "/start.sh"]
CMD []
