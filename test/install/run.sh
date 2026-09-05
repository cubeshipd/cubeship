#!/bin/sh
#
# Runs install.sh on a real Linux.
#
# The installer is the first thing every user runs and nothing in the Go
# suite can reach it, so it is exercised here: the whole of main(), with
# Docker replaced by a recording stub and the daemon's health check stood
# down — there is no daemon to be healthy in a container running a fake
# docker.
#
# Run it through `make test-install`.

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
	# `docker inspect` must fail the first time, so the installer treats
	# it as a first install; after `docker run` it succeeds, which is how
	# the upgrade path is exercised.
	cat > /stub/docker <<-'EOF'
		#!/bin/sh
		echo "docker $*" >> /tmp/docker.log
		case "$1" in
		  inspect) [ -f /tmp/started ] || exit 1 ;;
		  run)     touch /tmp/started ;;
		  info)    exit 0 ;;
		esac
		exit 0
	EOF
	# curl stands in for the public-address lookup: the container this
	# runs in may have no network, and the test wants a known answer.
	cat > /stub/curl <<-'EOF'
		#!/bin/sh
		echo "curl $*" >> /tmp/curl.log
		echo "203.0.113.7"
	EOF
	chmod +x /stub/systemctl /stub/docker /stub/curl
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
	export CUBESHIP_VERSION="testing"
	load_installer

	# There is no daemon to answer in here, and waiting sixty seconds to
	# find that out is the one step worth standing down.
	wait_for_health() { :; }

	printf '\ninstall.sh on %s\n\n' "$(uname -m)"

	# A full install, exactly as a first-time user gets it.
	out=$(main 2>&1) || { printf '%s\n' "$out"; exit 1; }

	check "creates the data directory" "$(stat -c '%a' /var/lib/cubeship)" "700"
	check "pulls the image" "$(grep -c '^docker pull ' /tmp/docker.log)" "1"
	check "creates the shared network" \
		"$(grep -c '^docker network create cubeship' /tmp/docker.log)" "1"
	check "runs the daemon" "$(grep -c 'docker run .*--name cubeship-daemon' /tmp/docker.log)" "1"
	check "gives it the Docker socket" \
		"$(grep -c 'var/run/docker.sock:/var/run/docker.sock' /tmp/docker.log)" "1"
	check "publishes the port" "$(grep -c '\-p 3000:3000' /tmp/docker.log)" "1"
	check "restarts it with the host" \
		"$(grep -c '\-\-restart unless-stopped' /tmp/docker.log)" "1"
	check "ends with the name in lights" "$(printf '%s' "$out" | grep -c '██████╗██╗')" "1"
	check "tells you where to open it" \
		"$(printf '%s' "$out" | grep -c 'https://203-0-113-7.sslip.io')" "1"
	check "makes a domain from the public address" \
		"$(grep -c 'CUBESHIP_DOMAIN=203-0-113-7.sslip.io' /tmp/docker.log)" "1"
	check "keeps the port as the way in until the certificate is up" \
		"$(printf '%s' "$out" | grep -c ':3000')" "1"

	# A domain given wins over the one made up, and is passed as is.
	rm -f /tmp/docker.log
	DOMAIN=cube.example.com main >/dev/null 2>&1
	check "--domain is passed through" \
		"$(grep -c 'CUBESHIP_DOMAIN=cube.example.com' /tmp/docker.log)" "1"
	DOMAIN=""

	# A lookup that fails is an install with no domain, not a failed one.
	rm -f /tmp/docker.log
	printf '#!/bin/sh\nexit 22\n' > /stub/curl
	out=$(main 2>&1) || { printf '%s\n' "$out"; exit 1; }
	check "no public address means no domain" \
		"$(grep -c 'CUBESHIP_DOMAIN= ' /tmp/docker.log)" "1"
	check "and says where to open instead" \
		"$(printf '%s' "$out" | grep -c 'http://.*:3000')" "1"
	printf '#!/bin/sh\necho 203.0.113.7\n' > /stub/curl

	# The data directory has to be mounted at the same path inside as
	# outside: the daemon hands these paths to the Engine for its
	# siblings' binds, and the Engine resolves them on the host.
	check "mounts the data directory at the same path" \
		"$(grep -c '\-v /var/lib/cubeship:/var/lib/cubeship' /tmp/docker.log)" "1"

	# --local builds instead of pulling. Running your own code on a box
	# is the normal case before anything is published, and the build has
	# to happen from the checkout the script is in.
	rm -f /tmp/docker.log
	touch /src/Dockerfile
	LOCAL=1
	build_image
	check "--local builds the image" "$(grep -c '^docker build ' /tmp/docker.log)" "1"
	check "--local does not pull" "$(grep -c '^docker pull ' /tmp/docker.log)" "0"
	check "--local builds from the checkout" "$(grep -c ' /src$' /tmp/docker.log)" "1"
	LOCAL=0

	# Piped from curl there is no checkout, and --local cannot serve
	# that. Saying so beats a build that fails on a missing Dockerfile.
	rm -f /src/Dockerfile
	if (LOCAL=1; build_image) >/dev/null 2>&1; then
		printf '  FAIL --local built with no repository present\n'
		FAILURES=$((FAILURES + 1))
	else
		printf '  ok   --local refuses without a checkout\n'
	fi

	# Running it again is how an upgrade happens: the container is
	# replaced, and nothing under the data directory is touched.
	rm -f /tmp/docker.log
	main >/dev/null 2>&1
	check "an upgrade replaces the container" \
		"$(grep -c '^docker rm -f cubeship-daemon' /tmp/docker.log)" "1"
	check "an upgrade does not touch the data directory" \
		"$(stat -c '%a' /var/lib/cubeship)" "700"

	printf '\n'
	[ "$FAILURES" = 0 ] || { printf '%d failure(s)\n\n' "$FAILURES"; exit 1; }
	printf 'install.sh ok\n\n'
}

run_tests "$@"
