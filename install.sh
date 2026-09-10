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
# Empty means "the newest stable release", which is resolved to an exact
# version below rather than pulled as `latest`. See resolve_version.
VERSION="${CUBESHIP_VERSION:-}"

# Where the newest release is looked up. Its own variable so a fork, or a
# test, can point it somewhere else.
RELEASES_API="${CUBESHIP_RELEASES_API:-https://api.github.com/repos/cubeship/cubeship/releases/latest}"

# LOCAL builds from source instead of pulling. Set by --local.
LOCAL=0

# A worker is a second machine, managed by an instance that already
# exists. It runs the same image in a mode where it decides nothing: no
# database, no dashboard, no registry, no builder, and no published
# port at all — it dials its control plane and does what it is told.
#
# Both of these are needed together. The address alone would be a
# machine that dials and is refused forever; the credential alone would
# be a machine with nothing to dial.
CONTROL_PLANE="${CUBESHIP_CONTROL_PLANE:-}"
NODE_TOKEN="${CUBESHIP_NODE_TOKEN:-}"
WORKER=0

CONTAINER=cubeship-daemon
NETWORK=cubeship
DATA_DIR="${CUBESHIP_DATA_DIR:-/var/lib/cubeship}"
# Where the daemon writes the token that guards claiming an unclaimed
# instance. Must match setup.TokenFileName.
SETUP_TOKEN_FILE="setup-token"
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
		       install.sh --control-plane <url> --token <token> [--local]

		  --local   Build the image from the checkout this script is in,
		            instead of pulling a published one. Requires the
		            repository; the build itself runs inside Docker.
		  --domain  Where the instance answers. Must resolve to this box.
		            Without it, <public-ip>.sslip.io is used, which does.

		Joining an existing instance instead of being one:

		  --control-plane  The instance this machine will belong to, e.g.
		                   https://cube.example.com
		  --version        Install this exact release, e.g. 0.1.0, or a
		                   prerelease like 0.2.0-rc.1. Without it, the
		                   newest stable release is looked up and pinned,
		                   so installing again gives the same thing.

		  --token          The credential that instance minted when the
		                   server was added to it. Add the server there
		                   first; the token is shown once.

		  A worker holds no database and serves nothing. It publishes no
		  port: it dials the control plane, and nothing dials it.

		Environment:
		  CUBESHIP_IMAGE      daemon image to pull (default $IMAGE)
		  CUBESHIP_WEB_IMAGE  dashboard image to pull (default $WEB_IMAGE)
		  CUBESHIP_VERSION    same as --version
		  CUBESHIP_RELEASES_API where the newest release is looked up
		  CUBESHIP_DATA_DIR   where the instance keeps its state (default $DATA_DIR)
		  CUBESHIP_DOMAIN     same as --domain
		  CUBESHIP_ACME_EMAIL contact address for Let's Encrypt (optional)
	USAGE
}

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
			--local) LOCAL=1 ;;
			--version) shift; [ $# -gt 0 ] || die "--version needs a release, like 0.1.0"; VERSION="$1" ;;
			--version=*) VERSION="${1#--version=}" ;;
			--domain) shift; [ $# -gt 0 ] || die "--domain needs a name"; DOMAIN="$1" ;;
			--domain=*) DOMAIN="${1#--domain=}" ;;
			--control-plane) shift; [ $# -gt 0 ] || die "--control-plane needs a URL"; CONTROL_PLANE="$1" ;;
			--control-plane=*) CONTROL_PLANE="${1#--control-plane=}" ;;
			--token) shift; [ $# -gt 0 ] || die "--token needs the credential the control plane minted"; NODE_TOKEN="$1" ;;
			--token=*) NODE_TOKEN="${1#--token=}" ;;
			-h | --help) usage; exit 0 ;;
			*) usage >&2; die "unknown option: $1" ;;
		esac
		shift
	done

	# Half a worker is not a mode, and the daemon refuses to start on
	# one anyway. Saying so here means the machine is not touched at all.
	if [ -n "$CONTROL_PLANE" ] || [ -n "$NODE_TOKEN" ]; then
		[ -n "$CONTROL_PLANE" ] || die "--token was given without --control-plane: a worker needs the address of the instance it belongs to."
		[ -n "$NODE_TOKEN" ] || die "--control-plane was given without --token: add the server on that instance first, and it will show you the credential once."
		WORKER=1
	fi
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

	# A worker serves no dashboard, so it does not need the image and
	# should not spend a build on one.
	if [ "$WORKER" = 0 ]; then
		say "Building $WEB_IMAGE:$VERSION from $dir…"
		docker build -f "$dir/Dockerfile.web" -t "$WEB_IMAGE:$VERSION" "$dir" ||
			die "the dashboard image did not build. Nothing was changed."
	fi

	say "Building $IMAGE:$VERSION from $dir…"
	docker build --build-arg "VERSION=$VERSION" -t "$IMAGE:$VERSION" "$dir" ||
		die "the daemon image did not build. Nothing was changed."
}

# resolve_version pins what is about to be installed to an exact release.
#
# **Not `latest`.** That tag moves, so a box installed today and the same
# command run tomorrow would be two different builds with no way to tell
# from the outside which is which — and re-running an install is what
# somebody does when something went wrong, which is the worst moment to
# change two variables at once.
#
# What it costs is one HTTPS call at install time, on a machine that is
# about to pull two images anyway. If it fails, the install stops and
# says to name a version: guessing `latest` after being unable to ask
# would be doing the thing this exists to avoid, quietly.
#
# A version somebody named is left exactly alone, prerelease or not:
# naming one is the whole way to install a release candidate.
resolve_version() {
	[ -z "$VERSION" ] || return 0
	[ "$LOCAL" = 0 ] || { VERSION="local"; return 0; }

	say "Looking up the newest release…"
	tag=$(
		curl -fsSL -H "Accept: application/vnd.github+json" "$RELEASES_API" 2>/dev/null |
			sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
			head -n 1
	) || tag=""
	[ -n "$tag" ] || die "could not ask $RELEASES_API which release is newest. Pass --version <release> to install a specific one."
	VERSION="${tag#v}"
	say "Installing $VERSION."
}

run_daemon() {
	if [ "$LOCAL" = 1 ]; then
		build_images
	else
		if [ "$WORKER" = 0 ]; then
			# Both, and the dashboard first: the daemon starts a
			# container from it the moment it comes up, and pulling it
			# there instead would be a pull with nobody watching it fail.
			say "Pulling $WEB_IMAGE:$VERSION…"
			docker pull "$WEB_IMAGE:$VERSION" >/dev/null ||
				die "could not pull $WEB_IMAGE:$VERSION"
		fi

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

	# The machine's own procfs, read-only, so the daemon can report what
	# the box is doing. /proc/stat and /proc/meminfo are not namespaced —
	# a container reading its own sees the machine's, which is why `top`
	# in one shows the host — but /proc/net is, so the interface counters
	# in here would be this container's veth. Mounted, PID 1's entry is
	# init's, which is in the machine's network namespace by definition.
	# Read-only, and it grants nothing: this daemon already has the
	# Docker socket, which is root on this box by another name.
	#
	# What the two modes share is everything about reaching this
	# machine; what they differ in is whether this machine is an
	# instance. Built as arguments rather than as two docker runs, so
	# the shared half cannot drift between them.
	set -- --name "$CONTAINER" \
		--network "$NETWORK" \
		--restart unless-stopped \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v "$DATA_DIR:$DATA_DIR" \
		-v /proc:/host/proc:ro \
		-e CUBESHIP_DATA_DIR="$DATA_DIR"

	if [ "$WORKER" = 1 ]; then
		# No port is published, and that is the point rather than an
		# omission: a worker's whole network presence is the call it
		# makes out to its control plane.
		set -- "$@" \
			-e CUBESHIP_CONTROL_PLANE="$CONTROL_PLANE" \
			-e CUBESHIP_NODE_TOKEN="$NODE_TOKEN"
	else
		set -- "$@" \
			-e CUBESHIP_WEB_IMAGE="$WEB_IMAGE:$VERSION" \
			-e CUBESHIP_DOMAIN="$DOMAIN" \
			-e CUBESHIP_ACME_EMAIL="$ACME_EMAIL" \
			-p "$PORT:$PORT"
	fi

	docker run -d "$@" "$IMAGE:$VERSION" >/dev/null ||
		die "could not start $CONTAINER. See: docker logs $CONTAINER"
}

# A worker has no port to poll, so what is waited on is the line the
# agent writes when its first pass is answered. That line is the whole
# of joining — there is no separate handshake — so seeing it means the
# control plane has this machine in its cluster.
#
# The one refusal worth failing fast on is a credential the control
# plane does not know: waiting five minutes to be told the token was
# wrong is five minutes nobody has to spend.
wait_for_join() {
	i=0
	while [ "$i" -lt 90 ]; do
		if docker logs "$CONTAINER" 2>&1 | grep -q 'agent: joined'; then
			return 0
		fi
		if docker logs "$CONTAINER" 2>&1 | grep -q 'does not recognise this machine'; then
			die "$CONTROL_PLANE refused this machine's credential. Add the server there and use the token it shows once."
		fi
		i=$((i + 1))
		sleep 2
	done
	die "this machine did not reach $CONTROL_PLANE. See: docker logs $CONTAINER"
}

# The daemon only listens once its siblings are up, and on a first
# install that is four images to pull — minutes on a slow box, with
# nothing to show for it. So its log is streamed here while we wait:
# the `bootstrap:` lines say which image is being pulled and which
# container just started, and that is exactly what the wait is.
wait_for_health() {
	progress=$(mktemp)
	docker logs -f "$CONTAINER" >"$progress" 2>&1 &
	logs=$!
	shown=0

	i=0
	while [ "$i" -lt 300 ]; do
		report_progress
		if curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1; then
			report_progress
			kill "$logs" 2>/dev/null
			rm -f "$progress"
			return 0
		fi
		i=$((i + 1))
		sleep 2
	done
	kill "$logs" 2>/dev/null
	rm -f "$progress"
	die "the daemon did not come up. See: docker logs $CONTAINER"
}

# report_progress prints the daemon's `bootstrap:` lines that arrived
# since the last call. The log is a file rather than a pipe so that the
# one process to stop afterwards is `docker logs` itself — a pipeline in
# the background would outlive the script and keep writing to the
# terminal after it.
report_progress() {
	total=$(wc -l <"$progress" | tr -d ' ')
	[ "$total" -gt "$shown" ] || return 0
	sed -n "$((shown + 1)),${total}p" "$progress" | sed -n 's/.*bootstrap: /    /p'
	shown=$total
}

banner() {
	cat <<-'ART'

		   ██████╗██╗   ██╗██████╗ ███████╗███████╗██╗  ██╗██╗██████╗
		  ██╔════╝██║   ██║██╔══██╗██╔════╝██╔════╝██║  ██║██║██╔══██╗
		  ██║     ██║   ██║██████╔╝█████╗  ███████╗███████║██║██████╔╝
		  ██║     ██║   ██║██╔══██╗██╔══╝  ╚════██║██╔══██║██║██╔═══╝
		  ╚██████╗╚██████╔╝██████╔╝███████╗███████║██║  ██║██║██║
		   ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝╚══════╝╚═╝  ╚═╝╚═╝╚═╝
	ART
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

	# A worker publishes nothing, so there is no port to guard and no
	# domain to make up. Only guard the port on a first install: on an
	# upgrade the thing holding it is the daemon being replaced.
	if [ "$WORKER" = 0 ]; then
		docker inspect "$CONTAINER" >/dev/null 2>&1 || check_port
	fi

	ensure_docker
	[ "$WORKER" = 1 ] || default_domain
	resolve_version
	run_daemon

	if [ "$WORKER" = 1 ]; then
		say "Joining $CONTROL_PLANE…"
		wait_for_join
		banner
		cat <<-DONE

			This machine is a Cubeship worker.

			  It belongs to  $CONTROL_PLANE

			It holds no database and serves nothing of its own: it dials
			the control plane, and nothing dials it. Manage it there.

			  docker logs -f $CONTAINER

		DONE
		return 0
	fi

	say "Waiting for the daemon…"
	wait_for_health

	host=$(address)
	[ -n "$host" ] || host="<this host's address>"

	banner
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

	# The setup token is what keeps whoever reaches the port first from
	# claiming the machine: the daemon writes it where only root can read
	# it, and creating the first account asks for it. It is gone from
	# here the moment somebody does, so an upgrade prints nothing.
	if [ -f "$DATA_DIR/$SETUP_TOKEN_FILE" ]; then
		cat <<-DONE
			Creating the first account needs this token:

			  $(cat "$DATA_DIR/$SETUP_TOKEN_FILE")

			It is stored in $DATA_DIR/$SETUP_TOKEN_FILE and is removed once
			the account exists. Anyone who can read it can claim this
			instance, so treat it as a password.

		DONE
	fi

	cat <<-DONE

		  docker ps
		  docker logs -f $CONTAINER

	DONE
}

main "$@"
