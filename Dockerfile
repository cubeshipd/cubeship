# The daemon, as it ships.
#
# It manages containers through the Docker socket and runs as one
# itself — a sibling of the registry, Traefik, BuildKit and every app,
# on the "cubeship" network they share.
#
# One thing about that is load-bearing and easy to get wrong: the paths
# this daemon passes to Docker for its siblings' bind mounts are
# resolved by the Engine on the HOST, not inside this container. The
# data directory therefore has to be mounted at the same path on both
# sides — see install.sh, and the note in AGENTS.md.
#
# The dashboard is NOT in here — it is its own image, built by
# Dockerfile.web, and the daemon starts a container from it. The two
# ship at the same version, and the daemon is told which one to use so
# that it never has to guess or derive it.

# --- the binary --------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src

# Dependencies first, so editing code does not re-download the module
# graph — which for this project is most of the build.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$VERSION" \
    -o /cubeshipd ./cmd/cubeshipd

# --- what runs ---------------------------------------------------------
FROM alpine:3.21

# ca-certificates: the daemon reaches GitHub and third-party registries
# over TLS, and an image with no roots fails every one of them with an
# error that names the wrong thing.
RUN apk add --no-cache ca-certificates

COPY --from=build /cubeshipd /usr/local/bin/cubeshipd

# Read by config.Load. It is set here rather than by whoever runs the
# image, because it describes the image rather than the deployment.
ENV CUBESHIP_IN_CONTAINER=1
ENV CUBESHIP_DATA_DIR=/var/lib/cubeship

# Which image the dashboard's container is started from. The default is
# the matching version of the published one, baked from the same build
# arg as the binary's — so an image pulled from the registry knows its
# own counterpart without being told. install.sh overrides it when it
# builds locally, where neither image is published.
ARG VERSION=dev
ENV CUBESHIP_WEB_IMAGE=ghcr.io/cubeship/cubeship-frontend:${VERSION}

EXPOSE 3000
ENTRYPOINT ["/usr/local/bin/cubeshipd"]
