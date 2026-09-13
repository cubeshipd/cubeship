# The template indexer, as it runs next to cubeship.dev.
#
# Built from the repository root, like every image here: the module
# replaces `cubeship` with ../../product, so the validator is compiled
# from the same checkout as the service.

FROM golang:1.27-alpine AS build
WORKDIR /src

COPY product/go.mod product/go.sum product/
COPY hosted/discovery/go.mod hosted/discovery/go.sum hosted/discovery/
RUN cd hosted/discovery && go mod download

COPY product/ product/
COPY hosted/discovery/ hosted/discovery/
RUN cd hosted/discovery && CGO_ENABLED=0 go build -ldflags "-s -w" -o /discovery ./cmd/discovery

FROM alpine:3.21
# GitHub and the bucket are both reached over TLS.
RUN apk add --no-cache ca-certificates
USER nobody
COPY --from=build /discovery /usr/local/bin/discovery
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/discovery"]
