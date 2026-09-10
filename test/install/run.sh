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
	# curl stands in for two lookups: the public address, and which
	# release is newest. The container this runs in may have no network,
	# and the test wants a known answer to both.
	cat > /stub/curl <<-'EOF'
		#!/bin/sh
		echo "curl $*" >> /tmp/curl.log
		case "$*" in
		  *releases*) [ -f /tmp/no-releases ] && exit 1
		              echo '{"tag_name": "v9.9.9", "name": "9.9.9"}' ;;
		  *)          echo "203.0.113.7" ;;
		esac
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
	# Two images, at the same version: the daemon and the dashboard.
	check "pulls both images" "$(grep -c '^docker pull ' /tmp/docker.log)" "2"
	check "creates the shared network" \
		"$(grep -c '^docker network create cubeship' /tmp/docker.log)" "1"
	check "runs the daemon" "$(grep -c 'docker run .*--name cubeship-daemon' /tmp/docker.log)" "1"
	check "gives it the Docker socket" \
		"$(grep -c 'var/run/docker.sock:/var/run/docker.sock' /tmp/docker.log)" "1"
	# Read-only, and it is what makes the instance's own network figures
	# possible: a container's /proc/net is its own namespace.
	check "gives it the machine's procfs" \
		"$(grep -c '\-v /proc:/host/proc:ro' /tmp/docker.log)" "1"
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

	# The setup token is what stops whoever reaches the port first from
	# claiming the machine, and the installer is where the operator
	# reads it. A stubbed Docker starts no daemon to write one, so it is
	# planted here.
	printf 'a-planted-setup-token\n' > /var/lib/cubeship/setup-token
	out=$(main 2>&1) || { printf '%s\n' "$out"; exit 1; }
	check "prints the setup token" \
		"$(printf '%s' "$out" | grep -c 'a-planted-setup-token')" "1"

	# It is removed the moment the instance is claimed, and an upgrade
	# must not talk about a credential that no longer exists.
	rm -f /var/lib/cubeship/setup-token
	out=$(main 2>&1) || { printf '%s\n' "$out"; exit 1; }
	check "says nothing about a token once the instance is claimed" \
		"$(printf '%s' "$out" | grep -ci 'setup token')" "0"

	# --local builds instead of pulling. Running your own code on a box
	# is the normal case before anything is published, and the build has
	# to happen from the checkout the script is in.
	rm -f /tmp/docker.log
	touch /src/Dockerfile /src/Dockerfile.web
	LOCAL=1
	build_images
	# The dashboard first and the daemon last: the daemon starts
	# everything, and a half-built pair is better found before anything
	# is replaced.
	check "--local builds both images" "$(grep -c '^docker build ' /tmp/docker.log)" "2"
	check "--local does not pull" "$(grep -c '^docker pull ' /tmp/docker.log)" "0"
	check "--local builds from the checkout" "$(grep -c ' /src$' /tmp/docker.log)" "2"
	LOCAL=0

	# Piped from curl there is no checkout, and --local cannot serve
	# that. Saying so beats a build that fails on a missing Dockerfile.
	rm -f /src/Dockerfile /src/Dockerfile.web
	if (LOCAL=1; build_images) >/dev/null 2>&1; then
		printf '  FAIL --local built with no repository present\n'
		FAILURES=$((FAILURES + 1))
	else
		printf '  ok   --local refuses without a checkout\n'
	fi

	# A worker is the same script and a different machine: it belongs to
	# an instance that already exists, holds none of its own, and — the
	# part worth pinning — publishes no port at all.
	rm -f /tmp/docker.log /tmp/started
	wait_for_join() { :; }
	out=$(main --control-plane https://cube.example.com --token a-node-credential 2>&1) ||
		{ printf '%s\n' "$out"; exit 1; }
	check "a worker is told which instance it belongs to" \
		"$(grep -c 'CUBESHIP_CONTROL_PLANE=https://cube.example.com' /tmp/docker.log)" "1"
	check "and what to authenticate as" \
		"$(grep -c 'CUBESHIP_NODE_TOKEN=a-node-credential' /tmp/docker.log)" "1"
	check "a worker publishes no port" "$(grep -c '\-p 3000:3000' /tmp/docker.log)" "0"
	check "a worker pulls only the daemon" "$(grep -c '^docker pull ' /tmp/docker.log)" "1"
	check "a worker is told no domain" "$(grep -c 'CUBESHIP_DOMAIN' /tmp/docker.log)" "0"
	check "and no dashboard to start" "$(grep -c 'CUBESHIP_WEB_IMAGE' /tmp/docker.log)" "0"
	check "it still gets the Docker socket" \
		"$(grep -c 'var/run/docker.sock:/var/run/docker.sock' /tmp/docker.log)" "1"
	# The closing line specifically, not any mention: the progress line
	# above it names the control plane too, and counting both would pin
	# how many times the installer says a thing rather than that it does.
	check "and says where it is managed from" \
		"$(printf '%s' "$out" | grep -c 'It belongs to .*https://cube.example.com')" "1"
	WORKER=0; CONTROL_PLANE=""; NODE_TOKEN=""

	# Half a worker is not a mode. Refusing here means the machine is
	# not touched at all, rather than dialling forever with a credential
	# it does not have.
	if (parse_args --control-plane https://cube.example.com) >/dev/null 2>&1; then
		printf '  FAIL an address with no credential was accepted\n'
		FAILURES=$((FAILURES + 1))
	else
		printf '  ok   refuses --control-plane without --token\n'
	fi
	if (parse_args --token a-node-credential) >/dev/null 2>&1; then
		printf '  FAIL a credential with nothing to dial was accepted\n'
		FAILURES=$((FAILURES + 1))
	else
		printf '  ok   refuses --token without --control-plane\n'
	fi
	WORKER=0; CONTROL_PLANE=""; NODE_TOKEN=""

	# Running it again is how an upgrade happens: the container is
	# replaced, and nothing under the data directory is touched.
	rm -f /tmp/docker.log
	main >/dev/null 2>&1
	check "an upgrade replaces the container" \
		"$(grep -c '^docker rm -f cubeship-daemon' /tmp/docker.log)" "1"
	check "an upgrade does not touch the data directory" \
		"$(stat -c '%a' /var/lib/cubeship)" "700"

	# **What is installed is an exact release, not `latest`.** That tag
	# moves, so a box installed today and the same command run tomorrow
	# would be two different builds with no way to tell which is which
	# — and re-running an install is what somebody does when something
	# went wrong, which is the worst moment to change two things.
	unset CUBESHIP_VERSION
	rm -f /tmp/docker.log /tmp/started
	main >/dev/null 2>&1
	check "the newest release is pinned to a version" \
		"$(grep -c 'docker pull .*cubeshipd:9\.9\.9$' /tmp/docker.log)" "1"
	check "nothing is pulled as latest" \
		"$(grep -c 'docker pull .*:latest$' /tmp/docker.log)" "0"

	# A version somebody named is left exactly alone — naming one is the
	# whole way to install a release candidate.
	rm -f /tmp/docker.log /tmp/started
	CUBESHIP_VERSION=0.2.0-rc.1 main >/dev/null 2>&1
	check "a named release is installed as given" \
		"$(grep -c 'docker pull .*cubeshipd:0\.2\.0-rc\.1$' /tmp/docker.log)" "1"

	# Being unable to ask must stop the install rather than fall back to
	# `latest`, which is the thing pinning exists to avoid — quietly.
	touch /tmp/no-releases
	unset CUBESHIP_VERSION
	rm -f /tmp/started
	if main >/dev/null 2>&1; then
		printf '  FAIL installs anyway when it cannot look up a release\n'
		FAILURES=$((FAILURES + 1))
	else
		printf '  ok   refuses to guess when it cannot look up a release\n'
	fi
	rm -f /tmp/no-releases
	export CUBESHIP_VERSION="testing"

	printf '\n'
	[ "$FAILURES" = 0 ] || { printf '%d failure(s)\n\n' "$FAILURES"; exit 1; }
	printf 'install.sh ok\n\n'
}

run_tests "$@"
