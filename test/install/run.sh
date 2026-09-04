#!/bin/sh
#
# Runs install.sh on a real Linux, against a release served from disk.
#
# The installer is the first thing every user runs and nothing in the Go
# suite can reach it, so it is exercised here: the whole of main(), with
# Docker and systemd replaced by recording stubs and the daemon's health
# check stood down — there is no daemon to be healthy in a container.
#
# Run it through `make test-install`, which builds the release it needs.

set -eu

FAILURES=0

check() {
	if [ "$2" = "$3" ]; then
		printf '  ok   %s\n' "$1"
	else
		printf '  FAIL %s\n       got %s, want %s\n' "$1" "$2" "$3"
		FAILURES=$((FAILURES + 1))
	fi
}

# Stubs stand in for what a container has no way to provide. Each one
# records that it was called, so the test can assert the installer got
# as far as using it.
setup_stubs() {
	mkdir -p /stub
	cat > /stub/systemctl <<-'EOF'
		#!/bin/sh
		echo "systemctl $*" >> /tmp/systemctl.log
	EOF
	cat > /stub/docker <<-'EOF'
		#!/bin/sh
		echo "docker $*" >> /tmp/docker.log
	EOF
	chmod +x /stub/systemctl /stub/docker
	PATH="/stub:$PATH"
	export PATH
}

# The installer ends in `main "$@"`. Dropping that line leaves its
# functions to be sourced and driven one at a time, with no hook in the
# script itself for a test to trip over.
load_installer() {
	sed '$d' /src/install.sh > /tmp/installer.sh
	# shellcheck disable=SC1091
	. /tmp/installer.sh
}

# Named for what it is: once the installer is sourced, `main` is the
# installer's, and that is what the assertions below drive.
run_tests() {
	setup_stubs

	# The release is mounted read-only; copy it where the test can
	# corrupt a checksum later without touching what was built.
	mkdir -p /work/releases
	cp -R /dist/testing /work/releases/testing

	export CUBESHIP_BASE_URL="file:///work"
	export CUBESHIP_VERSION="testing"
	load_installer

	# There is no daemon to answer in here, and waiting sixty seconds to
	# find that out is the one step worth standing down.
	wait_for_health() { :; }

	printf '\ninstall.sh on %s\n\n' "$(uname -m)"

	check "detects this architecture" "$(detect_arch)" "amd64"

	# A full install, exactly as a first-time user gets it.
	out=$(main 2>&1) || { printf '%s\n' "$out"; exit 1; }

	check "installs the binary" "$([ -x /usr/local/bin/cubeshipd ] && echo yes)" "yes"
	check "the installed binary runs" \
		"$(/usr/local/bin/cubeshipd -version | cut -d' ' -f1)" "cubeshipd"
	check "creates the data directory" "$(stat -c '%a' /var/lib/cubeship)" "700"
	check "writes the unit" \
		"$(grep -c 'ExecStart=/usr/local/bin/cubeshipd' /etc/systemd/system/cubeshipd.service)" "1"
	check "the unit sets the data directory" \
		"$(grep -c 'CUBESHIP_DATA_DIR=/var/lib/cubeship' /etc/systemd/system/cubeshipd.service)" "1"
	check "enables the service" "$(grep -c 'systemctl enable cubeshipd' /tmp/systemctl.log)" "1"
	check "starts the service" "$(grep -c 'systemctl restart cubeshipd' /tmp/systemctl.log)" "1"
	check "tells you where to open it" \
		"$(printf '%s' "$out" | grep -c ':3000')" "1"

	# Running it again is how an upgrade happens.
	rm -f /tmp/systemctl.log
	main >/dev/null 2>&1
	check "re-running restarts rather than failing" \
		"$(grep -c 'systemctl restart cubeshipd' /tmp/systemctl.log)" "1"

	# A binary about to be run as root must not be installed when it does
	# not match what was published.
	rm -f /usr/local/bin/cubeshipd
	sed 's/^./0/' /work/releases/testing/checksums.txt > /tmp/bad.txt
	cp /tmp/bad.txt /work/releases/testing/checksums.txt
	# In a subshell: the installer refuses by exiting, and this test is
	# not finished yet.
	if (install_daemon amd64) >/dev/null 2>&1; then
		printf '  FAIL a corrupted download was installed anyway\n'
		FAILURES=$((FAILURES + 1))
	else
		printf '  ok   refuses a checksum mismatch\n'
	fi
	check "and installs nothing when it refuses" \
		"$([ -e /usr/local/bin/cubeshipd ] && echo installed || echo absent)" "absent"

	printf '\n'
	[ "$FAILURES" = 0 ] || { printf '%d failure(s)\n\n' "$FAILURES"; exit 1; }
	printf 'install.sh ok\n\n'
}

run_tests "$@"
