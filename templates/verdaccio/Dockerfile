# The published image reads /verdaccio/conf/config.yaml, and the one it
# ships lets anyone register and read every package. Cubeship cannot mount a
# file into a container, so this image is that one with config.yaml in its
# place, and a start command that first creates the admin account from the
# template's inputs, since registration is closed.
FROM verdaccio/verdaccio:6.10.5
COPY config.yaml /verdaccio/conf/config.yaml
COPY --chmod=755 htpasswd-add /opt/verdaccio/docker-bin/htpasswd-add
# The image's own command, after the admin account.
CMD ["/bin/sh", "-c", "htpasswd-add --seed && exec verdaccio --config /verdaccio/conf/config.yaml --listen $VERDACCIO_PROTOCOL://$VERDACCIO_ADDRESS:$VERDACCIO_PORT"]
