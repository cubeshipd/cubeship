# Cubeship. Run `make` for the target list.

GO      ?= go
BINDIR  ?= bin
COVER   ?= coverage.out

# The daemon runs on the VPS, which is Linux; the CLI runs wherever you
# are. `make daemon-linux` cross-compiles the former from the latter.
DAEMON_GOOS   ?= linux
DAEMON_GOARCH ?= amd64
DAEMON_BIN    := $(BINDIR)/cubeshipd-$(DAEMON_GOOS)-$(DAEMON_GOARCH)

.DEFAULT_GOAL := help

.PHONY: help
help: ## List targets
	@grep -hE '^[a-z][a-z0-9-]*:.*## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*## "}{printf "  %-18s %s\n", $$1, $$2}'

.PHONY: build
build: ## Build cubeship and cubeshipd for this machine into bin/
	$(GO) build -o $(BINDIR)/ ./cmd/...

.PHONY: daemon-linux
daemon-linux: ## Cross-compile the daemon for the VPS (linux/amd64 unless overridden)
	GOOS=$(DAEMON_GOOS) GOARCH=$(DAEMON_GOARCH) $(GO) build -o $(DAEMON_BIN) ./cmd/cubeshipd

.PHONY: install
install: ## Install the CLI into GOBIN
	$(GO) install ./cmd/cubeship

# Deliberately not `systemctl restart` on a host you haven't named: pass
# HOST explicitly, every time.
.PHONY: ship
ship: daemon-linux ## Upload the daemon to a VPS and restart it (HOST=user@vps)
	@test -n "$(HOST)" || { echo "usage: make ship HOST=user@vps"; exit 1; }
	scp $(DAEMON_BIN) $(HOST):/tmp/cubeshipd.new
	ssh $(HOST) 'sudo install -m 0755 /tmp/cubeshipd.new /usr/local/bin/cubeshipd \
		&& rm -f /tmp/cubeshipd.new \
		&& sudo systemctl restart cubeshipd \
		&& sudo systemctl --no-pager status cubeshipd'

.PHONY: check
check: fmt-check vet test ## Everything that must pass before a commit

.PHONY: test
test: ## Unit tests, race detector on, no Docker needed
	$(GO) test -race -count=1 ./...

.PHONY: test-integration
test-integration: ## End-to-end test against a real Docker daemon (needs Linux)
	$(GO) test -tags integration -count=1 -v -timeout 15m ./test/integration/...

.PHONY: cover
cover: ## Unit test coverage, opened as HTML
	$(GO) test -coverprofile=$(COVER) ./...
	$(GO) tool cover -html=$(COVER)

# The integration test sits behind a build tag, so a plain `go vet ./...`
# never compiles it. Vet it explicitly or it rots.
.PHONY: vet
vet: ## go vet, including the build-tagged integration test
	$(GO) vet ./...
	$(GO) vet -tags integration ./test/integration/...

.PHONY: fmt
fmt: ## Rewrite badly formatted files in place
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if anything needs gofmt
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

.PHONY: tidy
tidy: ## Sync go.mod/go.sum with the imports
	$(GO) mod tidy

.PHONY: clean
clean: ## Remove build output
	rm -rf $(BINDIR) $(COVER)
