# The template catalog: the indexer and the API cubeship.dev proxies.
#
# Built from the repository root, like every image here: the module
# replaces `cubeship` with ../../product, so the validator is compiled
# from the same checkout as the service.

FROM golang:1.27-alpine AS build
WORKDIR /src

COPY product/go.mod product/go.sum product/
COPY hosted/catalog/go.mod hosted/catalog/go.sum hosted/catalog/
RUN cd hosted/catalog && go mod download

COPY product/ product/
COPY hosted/catalog/ hosted/catalog/
RUN cd hosted/catalog && CGO_ENABLED=0 go build -ldflags "-s -w" -o /catalog ./cmd/catalog

FROM alpine:3.21
# GitHub is reached over TLS.
RUN apk add --no-cache ca-certificates
USER nobody
COPY --from=build /catalog /usr/local/bin/catalog
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/catalog"]
