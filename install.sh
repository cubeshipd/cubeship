#!/bin/sh
#
# Cubeship installer.
#
#   curl -sSL https://cubeship.dev/install.sh | sh
#
# Installs Docker if it is missing and runs Cubeship as a container.
# Running it again upgrades in place; nothing under CUBESHIP_DATA_DIR is
# touched.
#
# Everything Cubeship runs is a container, the daemon included: it is a
# sibling of the registry, Traefik, BuildKit and every app, on one
# network they share.
#
# Everything is inside main(), called on the last line, so a download cut
# short cannot execute half an installer.

set -eu

# Where releases are served from. Point these somewhere else to install a
# build of your own.
IMAGE="${CUBESHIP_IMAGE:-ghcr.io/cubeship/cubeshipd}"
VERSION="${CUBESHIP_VERSION:-latest}"

CONTAINER=cubeship-daemon
NETWORK=cubeship
DATA_DIR="${CUBESHIP_DATA_DIR:-/var/lib/cubeship}"
PORT=3000

say() { printf '  %s\n' "$*"; }
die() { printf '\nerror: %s\n' "$*" >&2; exit 1; }

require_root() {
	[ "$(id -u)" = 0 ] || die "run this as root: the daemon needs the Docker socket and a directory under /var/lib."
}

require_linux() {
	[ "$(uname -s)" = Linux ] || die "Cubeship runs on Linux."
}

# The port has to be free before anything is installed: finding out after
# systemd has the unit means a failed service and a confusing journal.
check_port() {
	if command -v ss >/dev/null 2>&1; then
		listening=$(ss -ltnH "sport = :$PORT" 2>/dev/null || true)
	elif command -v netstat >/dev/null 2>&1; then
		listening=$(netstat -ltn 2>/dev/null | grep -E "[:.]$PORT[[:space:]]" || true)
	else
		return 0
	fi
	[ -z "$listening" ] || die "something is already listening on port $PORT. Stop it, or install on a host that has it free."
}

ensure_docker() {
	if command -v docker >/dev/null 2>&1; then
		say "Docker is already installed."
	else
		say "Installing Docker from get.docker.com…"
		curl -fsSL https://get.docker.com | sh >/dev/null ||
			die "Docker install failed. Install it yourself and run this again."
	fi

	systemctl enable --now docker >/dev/null 2>&1 || true
	docker info >/dev/null 2>&1 || die "Docker is installed but not responding. Start it and run this again."
}

# run_daemon pulls the image and starts the daemon as a container.
#
# The data directory is mounted at the SAME path inside as outside, and
# that is not cosmetic: the daemon passes paths to Docker when it creates
# Postgres, the registry and Traefik, and those are resolved by the
# Engine on the host. A different path inside would make every one of
# them bind a directory that does not exist.
run_daemon() {
	say "Pulling $IMAGE:$VERSION…"
	docker pull "$IMAGE:$VERSION" >/dev/null || die "could not pull $IMAGE:$VERSION"

	mkdir -p "$DATA_DIR"
	chmod 0700 "$DATA_DIR"

	# The network has to exist before the daemon joins it; the daemon
	# creates it for its own children, but cannot put itself on one that
	# is not there yet.
	docker network create "$NETWORK" >/dev/null 2>&1 || true

	# An upgrade replaces the container. Its state is all in the data
	# directory, so there is nothing in the container to keep.
	docker rm -f "$CONTAINER" >/dev/null 2>&1 || true

	docker run -d \
		--name "$CONTAINER" \
		--network "$NETWORK" \
		--restart unless-stopped \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v "$DATA_DIR:$DATA_DIR" \
		-e CUBESHIP_DATA_DIR="$DATA_DIR" \
		-p "$PORT:$PORT" \
		"$IMAGE:$VERSION" >/dev/null ||
		die "could not start $CONTAINER. See: docker logs $CONTAINER"
}

wait_for_health() {
	i=0
	while [ "$i" -lt 60 ]; do
		if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then
			return 0
		fi
		i=$((i + 1))
		sleep 2
	done
	die "the daemon did not come up. See: docker logs $CONTAINER"
}

# address is where to tell the operator to point their browser. It is the
# host's own routable address, not a public one looked up over the
# network — an installer should not phone anywhere.
address() {
	ip route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<NF;i++) if ($i=="src") {print $(i+1); exit}}' ||
		hostname -I 2>/dev/null | awk '{print $1}'
}

main() {
	printf '\nInstalling Cubeship\n\n'

	require_root
	require_linux

	# Only guard the port on a first install: on an upgrade the thing
	# holding it is the daemon being replaced.
	docker inspect "$CONTAINER" >/dev/null 2>&1 || check_port

	ensure_docker
	run_daemon

	say "Waiting for the daemon…"
	wait_for_health

	host=$(address)
	[ -n "$host" ] || host="<this host's address>"

	cat <<-DONE

		Cubeship is running.

		  Open  http://$host:$PORT

		The first person to open it creates the account — until someone
		does, anyone who can reach that port can claim this instance. Set
		your domain from the dashboard afterwards, and close port $PORT at
		the firewall once HTTPS is up.

		  docker ps
		  docker logs -f $CONTAINER

	DONE
}

main "$@"
