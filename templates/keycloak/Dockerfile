# The published image's entrypoint is `kc.sh`, which starts no server when
# given no command. Cubeship runs an image's own command, so this image is
# Keycloak's optimized container: the build-time options applied once here,
# and `start --optimized` as the command.
FROM quay.io/keycloak/keycloak:26.8.0 AS builder
# Build-time options. Health answers on the management port, 9000, which
# stays inside the container's network.
ENV KC_DB=postgres
ENV KC_HEALTH_ENABLED=true
WORKDIR /opt/keycloak
RUN /opt/keycloak/bin/kc.sh build

FROM quay.io/keycloak/keycloak:26.8.0
COPY --from=builder /opt/keycloak/ /opt/keycloak/
# So `kc.sh` run inside the container, bootstrap-admin included, agrees with
# the build.
ENV KC_DB=postgres
CMD ["start", "--optimized"]
