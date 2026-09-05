# Cubeship. Run `make` for the target list.

GO      ?= go
BINDIR  ?= bin
COVER   ?= coverage.out
WEBDIR  ?= web
PNPM    ?= pnpm
RELEASEDIR ?= dist

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
build: web ## Build cubeship and cubeshipd for this machine into bin/
	$(GO) build -o $(BINDIR)/ ./cmd/...

.PHONY: daemon-linux
daemon-linux: ## Cross-compile the daemon for the VPS (linux/amd64 unless overridden)
	GOOS=$(DAEMON_GOOS) GOARCH=$(DAEMON_GOARCH) $(GO) build -o $(DAEMON_BIN) ./cmd/cubeshipd

# The dashboard is its own image and its own container now, so the Go
# build no longer waits on it. This target is here to fail a broken
# dashboard on your machine rather than in the image build.
.PHONY: web
web: ## Build the dashboard, the way its image does
	cd $(WEBDIR) && $(PNPM) install --frozen-lockfile && $(PNPM) run build

# The data directory a dev daemon keeps its state in. Override it to run
# against a different instance; the default is yours, outside the repo,
# so it survives a `make clean` and a fresh checkout.
DEV_DATA_DIR ?= $(HOME)/.cubeship-dev

# A dev daemon is pointed at the Postgres the tests already run, rather
# than managing one of its own.
#
# Two reasons. It is one less container to wait for on every restart, and
# — the reason it is not merely a preference — the managed one cannot
# work on a Mac: its data directory is a host bind mount, and under
# /Users the Docker Desktop share synthesizes ownership, so Postgres
# refuses to start on a directory it does not own. On Linux, which is
# where Cubeship runs, the managed container is fine and is what an
# install uses.
DEV_DATABASE_URL ?= postgres://cubeship:cubeship@127.0.0.1:$(PG_PORT)/cubeship_dev?sslmode=disable

.PHONY: dev
dev: db-up ## Run the daemon with live reload, rebuilding on every Go change
	@mkdir -p $(DEV_DATA_DIR)
	@docker exec $(PG_CONTAINER) psql -U cubeship -d postgres -tc \
		"SELECT 1 FROM pg_database WHERE datname = 'cubeship_dev'" | grep -q 1 || \
		docker exec $(PG_CONTAINER) createdb -U cubeship cubeship_dev
	CUBESHIP_DATA_DIR=$(DEV_DATA_DIR) CUBESHIP_DATABASE_URL="$(DEV_DATABASE_URL)" $(GO) tool air

# The daemon proxies to :3001 when it runs on the host — see
# bootstrap.FrontendAddress — so this is the dashboard for `make dev`
# rather than a second thing to open. Reach the instance at :3000 either
# way; :3001 works too, and rewrites /api to the daemon itself.
.PHONY: web-dev
web-dev: ## Run the dashboard for `make dev`, with hot reload
	cd $(WEBDIR) && $(PNPM) run dev

.PHONY: install
install: ## Install the CLI into GOBIN
	$(GO) install ./cmd/cubeship

# Deliberately not `systemctl restart` on a host you haven't named: pass
# HOST explicitly, every time.
# The release is two images — the daemon and the dashboard — at the same
# version, and install.sh pulls both. Nothing else has to be hosted
# anywhere, and an upgrade is still a pull.
VERSION ?= dev
IMAGE   ?= ghcr.io/cubeship/cubeshipd

.PHONY: image
image: ## Build the daemon's image, dashboard included
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .

.PHONY: release
release: ## Build and push the image for both architectures
	docker buildx build --push \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

.PHONY: ship
ship: daemon-linux ## Upload the daemon to a VPS and restart it (HOST=user@vps)
	@test -n "$(HOST)" || { echo "usage: make ship HOST=user@vps"; exit 1; }
	scp $(DAEMON_BIN) $(HOST):/tmp/cubeshipd.new
	ssh $(HOST) 'sudo install -m 0755 /tmp/cubeshipd.new /usr/local/bin/cubeshipd \
		&& rm -f /tmp/cubeshipd.new \
		&& sudo systemctl restart cubeshipd \
		&& sudo systemctl --no-pager status cubeshipd'

.PHONY: check
check: fmt-check vet sh-check test ## Everything that must pass before a commit

# Postgres has no in-memory mode, so the unit tests need a real server.
# Each test gets its own schema in this one container (see
# internal/storetest), which is why one shared instance is enough.
# Port 5433, not 5432, so it never collides with a Postgres you already
# run.
PG_CONTAINER ?= cubeship-test-db
PG_PORT      ?= 5433
PG_IMAGE     ?= postgres:16-alpine

.PHONY: db-up
db-up: ## Start the Postgres the tests run against (idempotent)
	@if [ -n "$$(docker ps -q -f name=^/$(PG_CONTAINER)$$)" ]; then exit 0; fi; \
	if [ -n "$$(docker ps -aq -f name=^/$(PG_CONTAINER)$$)" ]; then \
		docker start $(PG_CONTAINER) >/dev/null; \
	else \
		docker run -d --name $(PG_CONTAINER) \
			-e POSTGRES_USER=cubeship -e POSTGRES_PASSWORD=cubeship -e POSTGRES_DB=cubeship_test \
			-p 127.0.0.1:$(PG_PORT):5432 $(PG_IMAGE) >/dev/null; \
	fi; \
	printf 'waiting for postgres'; \
	for i in $$(seq 1 30); do \
		if docker exec $(PG_CONTAINER) pg_isready -U cubeship -q 2>/dev/null; then echo " ready"; exit 0; fi; \
		printf '.'; sleep 1; \
	done; \
	echo; echo "postgres did not become ready; try: docker logs $(PG_CONTAINER)"; exit 1

.PHONY: db-down
db-down: ## Stop and remove the test Postgres, discarding its data
	-docker rm -f $(PG_CONTAINER)

# `make test` needs nothing installed, and that is the whole point: the
# edit-run loop should not wait on a Postgres, a builder container or a
# git clone.
#
# Two mechanisms draw that line. Anything that boots a container lives in
# test/integration behind a build tag, so it is not in ./... at all. The
# DB-backed tests are spread through the modules and cannot move, so they
# skip on -short — see dbtest.RequireDatabase, which fails rather than
# skipping when -short is absent.
.PHONY: test
test: ## Unit tests that need nothing but this repository, race detector on
	$(GO) test -short -race -count=1 ./...

# The same tests with the database they want. This is what CI runs.
.PHONY: test-db
test-db: db-up ## Unit tests including the DB-backed ones (starts Postgres)
	$(GO) test -race -count=1 ./...

# The installer is the first thing every user runs, and no Go test can
# reach it. This runs it on a real Linux against a release built here.
.PHONY: test-install
test-install: ## Run install.sh end to end in a Linux container
	docker run --rm \
		-v "$(CURDIR)/install.sh:/src/install.sh:ro" \
		-v "$(CURDIR)/test/install/run.sh:/src/run.sh:ro" \
		--platform linux/amd64 debian:bookworm-slim \
		sh -c 'apt-get -qq update && apt-get -qq install -y curl > /dev/null && sh /src/run.sh'

# uninstall.sh deletes things. A destructive script that is wrong is the
# worst kind, so it is run for real, on a Linux, with Docker stubbed.
.PHONY: test-uninstall
test-uninstall: ## Run uninstall.sh end to end in a Linux container
	docker run --rm \
		-v "$(CURDIR)/uninstall.sh:/src/uninstall.sh:ro" \
		-v "$(CURDIR)/test/install/uninstall.sh:/src/run.sh:ro" \
		--platform linux/amd64 debian:bookworm-slim \
		sh /src/run.sh

.PHONY: test-integration
test-integration: ## End-to-end test against a real Docker daemon (needs Linux)
	$(GO) test -tags integration -count=1 -v -timeout 15m ./test/integration/...

.PHONY: cover
cover: ## Unit test coverage, opened as HTML
	$(GO) test -coverprofile=$(COVER) ./...
	$(GO) tool cover -html=$(COVER)

# The integration test sits behind a build tag, so a plain `go vet ./...`
# never compiles it. Vet it explicitly or it rots.
.PHONY: sh-check
sh-check: ## Syntax-check the shell scripts
	@for f in install.sh uninstall.sh test/install/run.sh test/install/uninstall.sh; do \
		sh -n $$f || exit 1; \
	done

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
	rm -rf $(BINDIR) $(COVER) $(RELEASEDIR) $(WEBDIR)/.next
