# The configuration Plausible's compose file mounts into ClickHouse: logs to
# the console with the system logs Plausible never reads switched off, IPv4
# only, and the settings for a machine with little memory. Cubeship cannot
# mount a file into a container, so they are copied in. The build context is
# the repository root.
FROM clickhouse/clickhouse-server:24.12.6.70-alpine
COPY clickhouse/logs.xml clickhouse/ipv4-only.xml clickhouse/low-resources.xml /etc/clickhouse-server/config.d/
COPY clickhouse/default-profile-low-resources-overrides.xml /etc/clickhouse-server/users.d/
