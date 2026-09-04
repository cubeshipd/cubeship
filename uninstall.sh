#!/bin/sh
#
# Removes Cubeship from this host.
#
#   sudo ./uninstall.sh            stop and remove the containers
#   sudo ./uninstall.sh --purge    also delete the data, permanently
#
# By default the data directory is left alone, because it is the whole
# instance — the database, the pushed images, the certificates. Running
# install.sh again on a host uninstalled this way brings the same
# instance back, with its accounts and apps.
#
# --purge is the other thing, and it is not recoverable.
#
# Everything is inside main(), called on the last line, so a download cut
# short cannot execute half of it.

set -eu

CONTAINER=cubeship-daemon
NETWORK=cubeship
DATA_DIR="${CUBESHIP_DATA_DIR:-/var/lib/cubeship}"

PURGE=0
ASSUME_YES=0

say() { printf '  %s\n' "$*"; }
die() { printf '\nerror: %s\n' "$*" >&2; exit 1; }

usage() {
	cat <<-USAGE
		Usage: uninstall.sh [--purge] [--yes]

		  --purge   Also delete $DATA_DIR: the database, the pushed
		            images and the certificates. Not recoverable.
		  --yes     Do not ask. Required when there is no terminal to
		            ask on, which is what piping this into a shell means.

		Docker itself is left installed, and so is anything else on this
		host that is not Cubeship's.
	USAGE
}

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
			--purge) PURGE=1 ;;
			--yes | -y) ASSUME_YES=1 ;;
			-h | --help) usage; exit 0 ;;
			*) usage >&2; die "unknown option: $1" ;;
		esac
		shift
	done
}

require_root() {
	[ "$(id -u)" = 0 ] || die "run this as root: the containers and $DATA_DIR belong to it."
}

require_docker() {
	command -v docker >/dev/null 2>&1 || die "no docker on this host, so there is nothing of Cubeship's to remove."
}

# containers lists everything Cubeship named, which is everything it
# made: the daemon, the infrastructure, and one per app.
containers() {
	docker ps -aq --filter "name=^/cubeship-" 2>/dev/null || true
}

# confirm shows what is about to go and waits for the word. A second
# button is no obstacle to a misclick, and this is not undoable.
confirm() {
	[ "$ASSUME_YES" = 1 ] && return 0

	if [ ! -t 0 ]; then
		die "this needs a terminal to confirm on. Run it from one, or pass --yes if you mean it."
	fi

	printf '\nType "delete" to confirm: '
	read -r answer
	[ "$answer" = "delete" ] || die "nothing was removed."
}

remove_containers() {
	ids=$(containers)
	if [ -z "$ids" ]; then
		say "No Cubeship containers on this host."
		return 0
	fi
	say "Removing $(echo "$ids" | wc -l | tr -d ' ') container(s)…"
	# shellcheck disable=SC2086 # one argument per id is what is wanted.
	docker rm -f $ids >/dev/null 2>&1 || true
}

remove_network() {
	docker network rm "$NETWORK" >/dev/null 2>&1 || true
}

# remove_images drops what Cubeship built here. Images someone pushed to
# the embedded registry went with its data directory; these are the ones
# in the Engine's own store.
remove_images() {
	ids=$(docker images -q "cubeship-build/*" 2>/dev/null || true)
	[ -z "$ids" ] || {
		# shellcheck disable=SC2086
		docker rmi -f $ids >/dev/null 2>&1 || true
	}
}

purge_data() {
	[ -d "$DATA_DIR" ] || return 0
	say "Deleting $DATA_DIR…"
	rm -rf "$DATA_DIR"
}

# what_goes prints the damage before anything is done, because a list of
# names is the only thing that turns "yes" into an informed one.
what_goes() {
	printf '\nThis will remove:\n\n'

	ids=$(containers)
	if [ -n "$ids" ]; then
		docker ps -a --filter "name=^/cubeship-" --format '  container  {{.Names}}' 2>/dev/null || true
	else
		printf '  (no containers)\n'
	fi
	printf '  network    %s\n' "$NETWORK"

	if [ "$PURGE" = 1 ]; then
		printf '\n  and %s, permanently: the database, the pushed images\n' "$DATA_DIR"
		printf '  and the certificates. This is not recoverable.\n'
	else
		printf '\n%s is kept. Installing again brings this instance back.\n' "$DATA_DIR"
		printf 'Pass --purge to delete it too.\n'
	fi
}

main() {
	parse_args "$@"
	require_root
	require_docker

	what_goes
	confirm

	printf '\n'
	remove_containers
	remove_network
	[ "$PURGE" = 1 ] && { remove_images; purge_data; }

	cat <<-DONE

		Cubeship is removed.

		Docker is still installed, and so is anything else here that was
		not Cubeship's.

	DONE
}

main "$@"
