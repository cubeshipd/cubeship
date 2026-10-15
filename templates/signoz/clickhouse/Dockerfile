# ClickHouse as SigNoz's Docker install runs it, in one container: its
# configuration is a file, Cubeship cannot mount one, and upstream fetches the
# histogramQuantile function in a container that runs once and exits, which
# Cubeship has no such thing as. So both are built in here.
FROM clickhouse/clickhouse-server:25.12.11

RUN arch="$(uname -m | sed s/aarch64/arm64/ | sed s/x86_64/amd64/)" \
    && mkdir -p /opt/clickhouse/user_scripts \
    && cd /tmp \
    && wget -q -O histogram-quantile.tar.gz "https://github.com/SigNoz/signoz/releases/download/histogram-quantile%2Fv0.0.1/histogram-quantile_linux_${arch}.tar.gz" \
    && tar -xzf histogram-quantile.tar.gz \
    && mv histogram-quantile /opt/clickhouse/user_scripts/histogramQuantile \
    && chmod 755 /opt/clickhouse/user_scripts/histogramQuantile \
    && rm -rf /tmp/*

COPY clickhouse/config.xml /etc/clickhouse-server/signoz/config.xml
COPY clickhouse/functions.yaml /etc/clickhouse-server/signoz/functions.yaml

ENV CLICKHOUSE_CONFIG=/etc/clickhouse-server/signoz/config.xml
# The users are in config.xml; the entrypoint must not write its own.
ENV CLICKHOUSE_SKIP_USER_SETUP=1
