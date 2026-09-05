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
# With --local it builds the image from the checkout it is sitting in
# instead of pulling a published one. That is how you run your own code
# on a box: push, pull on the server, and install from the source that
# is now there. The build happens inside Docker, so the server needs no
# Go and no Node.
#
# Everything Cubeship runs is a container, the daemon included: it is a
# sibling of the registry, Traefik, BuildKit and every app, on one
# network they share.
#
# Everything is inside main(), called on the last line, so a download cut
# short cannot execute half an installer.

set -eu

# Where releases are pulled from. Point these somewhere else to install a
# build of your own.
IMAGE="${CUBESHIP_IMAGE:-ghcr.io/cubeship/cubeshipd}"
# The dashboard is its own image and its own container, started by the
# daemon rather than by this script — so all that happens here is making
# sure it is on the box and telling the daemon its name.
WEB_IMAGE="${CUBESHIP_WEB_IMAGE:-ghcr.io/cubeship/cubeship-frontend}"
VERSION="${CUBESHIP_VERSION:-latest}"

# LOCAL builds from source instead of pulling. Set by --local.
LOCAL=0

CONTAINER=cubeship-daemon
NETWORK=cubeship
DATA_DIR="${CUBESHIP_DATA_DIR:-/var/lib/cubeship}"
PORT=3000

# DOMAIN is where the instance answers over HTTPS. Left empty, one is
# made from the box's public address under sslip.io — a wildcard DNS
# service that resolves <a-b-c-d>.sslip.io to a.b.c.d — so a fresh
# install has a name and a certificate before anyone owns a domain.
# ACME_EMAIL is the contact Let's Encrypt registers, and is optional.
DOMAIN="${CUBESHIP_DOMAIN:-}"
ACME_EMAIL="${CUBESHIP_ACME_EMAIL:-}"

say() { printf '  %s\n' "$*"; }
die() { printf '\nerror: %s\n' "$*" >&2; exit 1; }

require_root() {
	[ "$(id -u)" = 0 ] || die "run this as root: the daemon needs the Docker socket and a directory under /var/lib."
}

require_linux() {
	[ "$(uname -s)" = Linux ] || die "Cubeship runs on Linux."
}

usage() {
	cat <<-USAGE
		Usage: install.sh [--local] [--domain <name>]

		  --local   Build the image from the checkout this script is in,
		            instead of pulling a published one. Requires the
		            repository; the build itself runs inside Docker.
		  --domain  Where the instance answers. Must resolve to this box.
		            Without it, <public-ip>.sslip.io is used, which does.

		Environment:
		  CUBESHIP_IMAGE      daemon image to pull (default $IMAGE)
		  CUBESHIP_WEB_IMAGE  dashboard image to pull (default $WEB_IMAGE)
		  CUBESHIP_VERSION    tag to pull (default $VERSION)
		  CUBESHIP_DATA_DIR   where the instance keeps its state (default $DATA_DIR)
		  CUBESHIP_DOMAIN     same as --domain
		  CUBESHIP_ACME_EMAIL contact address for Let's Encrypt (optional)
	USAGE
}

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
			--local) LOCAL=1 ;;
			--domain) shift; [ $# -gt 0 ] || die "--domain needs a name"; DOMAIN="$1" ;;
			--domain=*) DOMAIN="${1#--domain=}" ;;
			-h | --help) usage; exit 0 ;;
			*) usage >&2; die "unknown option: $1" ;;
		esac
		shift
	done
}

# source_dir is the checkout this script is in, which only exists when it
# was run as a file. Piped from curl there is nothing to build, and that
# is exactly the case --local cannot serve.
source_dir() {
	case "$0" in
		*/*) dirname "$0" ;;
		*) echo "." ;;
	esac
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
# build_images makes both images from the source next to this script.
# Each Dockerfile is multi-stage, so the compiler and the front-end
# toolchain live in the builds rather than on the host.
#
# The daemon is built second and last on purpose: it is the one that
# starts everything, and a half-built pair is better discovered before
# anything is replaced.
build_images() {
	dir=$(source_dir)
	[ -f "$dir/Dockerfile" ] ||
		die "--local needs the repository. Run it from a checkout: git clone, then sudo ./install.sh --local"

	IMAGE="${CUBESHIP_IMAGE:-cubeship/cubeshipd}"
	WEB_IMAGE="${CUBESHIP_WEB_IMAGE:-cubeship/cubeship-frontend}"
	VERSION="${CUBESHIP_VERSION:-local}"

	say "Building $WEB_IMAGE:$VERSION from $dir…"
	docker build -f "$dir/Dockerfile.web" -t "$WEB_IMAGE:$VERSION" "$dir" ||
		die "the dashboard image did not build. Nothing was changed."

	say "Building $IMAGE:$VERSION from $dir…"
	docker build --build-arg "VERSION=$VERSION" -t "$IMAGE:$VERSION" "$dir" ||
		die "the daemon image did not build. Nothing was changed."
}

run_daemon() {
	if [ "$LOCAL" = 1 ]; then
		build_images
	else
		# Both, and the dashboard first: the daemon starts a container
		# from it the moment it comes up, and pulling it there instead
		# would be a pull with nobody watching it fail.
		say "Pulling $WEB_IMAGE:$VERSION…"
		docker pull "$WEB_IMAGE:$VERSION" >/dev/null ||
			die "could not pull $WEB_IMAGE:$VERSION"

		say "Pulling $IMAGE:$VERSION…"
		docker pull "$IMAGE:$VERSION" >/dev/null || die "could not pull $IMAGE:$VERSION"
	fi

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
		-e CUBESHIP_WEB_IMAGE="$WEB_IMAGE:$VERSION" \
		-e CUBESHIP_DOMAIN="$DOMAIN" \
		-e CUBESHIP_ACME_EMAIL="$ACME_EMAIL" \
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

# address is the host's own routable address, the fallback for telling
# the operator where to open when no domain could be made.
address() {
	ip route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<NF;i++) if ($i=="src") {print $(i+1); exit}}' ||
		hostname -I 2>/dev/null | awk '{print $1}'
}

# public_ip asks the outside world, because that is the only place the
# answer is: behind a cloud NAT the routable address is private. Three
# services, the first that answers with an IPv4 wins; none answering is
# not an error, it is an install with no domain.
public_ip() {
	for url in https://api.ipify.org https://ifconfig.me/ip https://icanhazip.com; do
		ip=$(curl -fsS --max-time 5 "$url" 2>/dev/null | tr -d '[:space:]')
		case "$ip" in
			*[!0-9.]* | "") continue ;;
		esac
		echo "$ip"
		return 0
	done
	return 1
}

# default_domain fills DOMAIN when nothing was given, or says why it
# could not.
default_domain() {
	[ -z "$DOMAIN" ] || return 0
	ip=$(public_ip) || return 0
	DOMAIN="$(printf '%s' "$ip" | tr . -).sslip.io"
	say "No domain given; using $DOMAIN"
}

main() {
	parse_args "$@"
	printf '\nInstalling Cubeship\n\n'

	require_root
	require_linux

	# Only guard the port on a first install: on an upgrade the thing
	# holding it is the daemon being replaced.
	docker inspect "$CONTAINER" >/dev/null 2>&1 || check_port

	ensure_docker
	default_domain
	run_daemon

	say "Waiting for the daemon…"
	wait_for_health

	host=$(address)
	[ -n "$host" ] || host="<this host's address>"

	if [ -n "$DOMAIN" ]; then
		cat <<-DONE

			Cubeship is running.

			  Open  https://$DOMAIN

			Ports 80 and 443 must be open; the certificate is issued on the
			first visit, which can take a minute. Until then, or if $DOMAIN
			does not reach this box, http://$host:$PORT is the way in.

		DONE
	else
		cat <<-DONE

			Cubeship is running.

			  Open  http://$host:$PORT

			No public address could be found, so there is no domain yet. Set
			one from the dashboard, and close port $PORT at the firewall once
			HTTPS is up.

		DONE
	fi

	cat <<-DONE
		The first person to open it creates the account — until someone
		does, anyone who can reach this instance can claim it.

		  docker ps
		  docker logs -f $CONTAINER

	DONE
}

main "$@"
