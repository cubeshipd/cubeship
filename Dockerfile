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

# --- the dashboard -----------------------------------------------------
FROM node:22-alpine AS web
WORKDIR /src

RUN corepack enable

# pnpm-workspace.yaml travels with the manifest and the lockfile because
# it carries the settings the install itself is governed by, not just the
# workspace layout. Leaving it out made the install here run under
# different policy from the install everywhere else — which surfaced as a
# build that fails on a dependency published today and passes tomorrow.
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY web/ ./
RUN pnpm run build

# --- the binary --------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src

# Dependencies first, so editing code does not re-download the module
# graph — which for this project is most of the build.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# The dashboard is embedded, so it has to be in place before the compile
# rather than copied alongside the binary afterwards.
COPY --from=web /src/out ./internal/web/dist

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

EXPOSE 3000
ENTRYPOINT ["/usr/local/bin/cubeshipd"]
