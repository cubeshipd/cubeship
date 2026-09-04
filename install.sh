#!/bin/sh
#
# Cubeship installer.
#
#   curl -sSL https://cubeship.dev/install.sh | sh
#
# Installs Docker if it is missing, puts the cubeshipd binary in
# /usr/local/bin, registers it with systemd and starts it. Running it
# again upgrades an existing install in place; nothing under
# CUBESHIP_DATA_DIR is touched.
#
# Everything is inside main(), called on the last line, so a download cut
# short cannot execute half an installer.

set -eu

# Where releases are served from. Point these somewhere else to install a
# build of your own.
BASE_URL="${CUBESHIP_BASE_URL:-https://cubeship.dev}"
VERSION="${CUBESHIP_VERSION:-latest}"

BIN=/usr/local/bin/cubeshipd
UNIT=/etc/systemd/system/cubeshipd.service
DATA_DIR="${CUBESHIP_DATA_DIR:-/var/lib/cubeship}"
PORT=3000

say() { printf '  %s\n' "$*"; }
die() { printf '\nerror: %s\n' "$*" >&2; exit 1; }

require_root() {
	[ "$(id -u)" = 0 ] || die "run this as root: the daemon needs the Docker socket and a systemd unit."
}

require_linux() {
	[ "$(uname -s)" = Linux ] || die "Cubeship runs on Linux. Docker Desktop's VM does not bridge host networking, which Traefik needs."
	command -v systemctl >/dev/null 2>&1 || die "no systemd on this host, so there is nothing to register the daemon with."
}

detect_arch() {
	case "$(uname -m)" in
		x86_64 | amd64) echo amd64 ;;
		aarch64 | arm64) echo arm64 ;;
		*) die "unsupported architecture $(uname -m). Cubeship ships amd64 and arm64." ;;
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

# fetch downloads to a file, failing loudly rather than leaving a
# half-written binary behind.
fetch() {
	curl -fsSL "$1" -o "$2" || die "could not download $1"
}

install_daemon() {
	arch="$1"
	tmp=$(mktemp -d)
	# shellcheck disable=SC2064 # expand tmp now: it is what we want removed.
	trap "rm -rf '$tmp'" EXIT

	name="cubeshipd-linux-$arch"
	say "Downloading $name ($VERSION)…"
	fetch "$BASE_URL/releases/$VERSION/$name" "$tmp/$name"
	fetch "$BASE_URL/releases/$VERSION/checksums.txt" "$tmp/checksums.txt"

	# A binary this script is about to run as root is worth checking. The
	# checksum comes from the same host over the same TLS, so this
	# catches corruption and truncation rather than a hostile mirror.
	say "Verifying checksum…"
	(cd "$tmp" && grep " $name\$" checksums.txt | sha256sum -c - >/dev/null) ||
		die "checksum mismatch on $name. Nothing was installed."

	install -m 0755 "$tmp/$name" "$BIN"
	mkdir -p "$DATA_DIR"
	chmod 0700 "$DATA_DIR"
}

write_unit() {
	cat > "$UNIT" <<-UNITFILE
		[Unit]
		Description=Cubeship deploy daemon
		# The daemon manages containers and cannot start before Docker is up.
		After=network-online.target docker.service
		Wants=network-online.target
		Requires=docker.service

		[Service]
		Type=simple
		ExecStart=$BIN

		# The domain and the Let's Encrypt contact address are NOT set here:
		# the daemon starts without them, and they are configured afterwards
		# from the dashboard.
		#
		# Persistent state: the Postgres data directory, the daemon's
		# secrets, registry config and image storage, Traefik's acme.json,
		# and the build cache.
		Environment=CUBESHIP_DATA_DIR=$DATA_DIR

		# The daemon talks to the Docker socket, so it runs as root.
		User=root
		Restart=always
		RestartSec=5s

		# NOTE: the daemon listens on 0.0.0.0:$PORT in plaintext. That is the
		# only way in until a domain is set, after which everything is
		# reachable over HTTPS at api.<domain> and this port should be
		# closed at the host firewall.

		[Install]
		WantedBy=multi-user.target
	UNITFILE

	systemctl daemon-reload
	systemctl enable cubeshipd >/dev/null 2>&1
	systemctl restart cubeshipd
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
	die "the daemon did not come up. See: journalctl -u cubeshipd -n 50"
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
	arch=$(detect_arch)

	# Only guard the port on a first install: on an upgrade the thing
	# holding it is the daemon being replaced.
	[ -f "$UNIT" ] || check_port

	ensure_docker
	install_daemon "$arch"
	write_unit

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

		  systemctl status cubeshipd
		  journalctl -u cubeshipd -f

	DONE
}

main "$@"
