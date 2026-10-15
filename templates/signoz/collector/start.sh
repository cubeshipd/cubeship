#!/bin/sh
# What SigNoz's Docker install does in its migrator container, then its
# collector's own command. Every migrate step is idempotent, so running them
# on each start is how an upgrade migrates too.
set -e

collector=/signoz-otel-collector

# Waits, up to SIGNOZ_OTEL_COLLECTOR_TIMEOUT, for ClickHouse and its Keeper.
$collector migrate ready
$collector migrate bootstrap
$collector migrate sync up

# Upstream runs these beside a collector that is already ingesting; so does
# this. A failure is logged here and retried on the next start.
$collector migrate async up &

# SigNoz pushes log pipelines to the collector over OpAMP. The file only
# names the server, and its address follows the install's names.
printf 'server_endpoint: ws://%s:4320/v1/opamp\n' "$SIGNOZ_HOST" > /var/tmp/opamp.yaml

exec $collector \
  --config=/etc/otel-collector-config.yaml \
  --manager-config=/var/tmp/opamp.yaml \
  --copy-path=/var/tmp/collector-config.yaml
