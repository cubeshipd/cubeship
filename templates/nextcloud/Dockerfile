# The published image runs Apache and nothing else, and Nextcloud's background
# jobs need cron every 5 minutes. Cubeship runs no sidecar and no cron, so this
# is the image's own example (.examples/dockerfiles/cron in nextcloud/docker):
# supervisord running Apache and the image's /cron.sh side by side.
FROM nextcloud:35.0.1-apache

RUN set -ex; \
    apt-get update; \
    apt-get install -y --no-install-recommends supervisor; \
    rm -rf /var/lib/apt/lists/*; \
    mkdir -p /var/log/supervisord /var/run/supervisord

COPY supervisord.conf /

# The entrypoint installs and upgrades Nextcloud only before apache2-foreground,
# unless this is set.
ENV NEXTCLOUD_UPDATE=1

CMD ["/usr/bin/supervisord", "-c", "/supervisord.conf"]
