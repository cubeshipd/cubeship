#!/bin/sh
#
# Runs uninstall.sh on a real Linux, with Docker replaced by a recording
# stub. Nothing in the Go suite can reach a shell script, and this one
# deletes things.
#
# Run it through `make test-uninstall`.

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

setup_stubs() {
	mkdir -p /stub
	cat > /stub/docker <<-'EOF'
		#!/bin/sh
		echo "docker $*" >> /tmp/docker.log
		case "$1 $2" in
		  "ps -aq") echo cubeship-daemon; echo cubeship-postgres ;;
		  "images -q") echo sha256:deadbeef ;;
		esac
		exit 0
	EOF
	chmod +x /stub/docker
	PATH="/stub:$PATH"
	export PATH
}

load() {
	sed '$d' /src/uninstall.sh > /tmp/uninstaller.sh
	# shellcheck disable=SC1091
	. /tmp/uninstaller.sh
}

reset() { rm -f /tmp/docker.log; mkdir -p "$DATA_DIR"; }

run_tests() {
	setup_stubs
	export CUBESHIP_DATA_DIR=/var/lib/cubeship
	load

	printf '\nuninstall.sh\n\n'

	# The default keeps the instance. Someone removing the software is
	# not thereby asking to lose their database.
	reset
	ASSUME_YES=1 PURGE=0
	out=$(main --yes 2>&1) || { printf '%s\n' "$out"; exit 1; }

	check "removes the containers" "$(grep -c '^docker rm -f ' /tmp/docker.log)" "1"
	check "removes the network" "$(grep -c '^docker network rm cubeship' /tmp/docker.log)" "1"
	check "keeps the data by default" "$([ -d "$DATA_DIR" ] && echo kept || echo gone)" "kept"
	check "removes no images by default" "$(grep -c '^docker rmi ' /tmp/docker.log)" "0"
	check "says the data was kept" "$(printf '%s' "$out" | grep -c "$DATA_DIR is kept")" "1"

	# --purge is the other thing, and it says so before it does it.
	reset
	out=$(main --purge --yes 2>&1) || { printf '%s\n' "$out"; exit 1; }

	check "purge deletes the data" "$([ -d "$DATA_DIR" ] && echo kept || echo gone)" "gone"
	check "purge removes built images" "$(grep -c '^docker rmi ' /tmp/docker.log)" "1"
	check "purge warns it is permanent" "$(printf '%s' "$out" | grep -ci 'not recoverable')" "1"

	# The damage is listed before the question, not after it. A list of
	# names is what turns "yes" into an informed one.
	reset
	out=$(main --purge --yes 2>&1)
	check "names what goes" "$(printf '%s' "$out" | grep -c 'This will remove')" "1"

	# Piped into a shell there is no terminal to confirm on, and a
	# destructive default must not proceed on silence.
	reset
	if (ASSUME_YES=0; main --purge < /dev/null) >/dev/null 2>&1; then
		printf '  FAIL purged with nothing to confirm on\n'
		FAILURES=$((FAILURES + 1))
	else
		printf '  ok   refuses to purge with no terminal\n'
	fi
	check "and deleted nothing when it refused" \
		"$([ -d "$DATA_DIR" ] && echo kept || echo gone)" "kept"

	# Confirming needs a real terminal, so these two run under one. A
	# pipe would trip the no-terminal guard above and prove nothing about
	# what the answer has to be.
	ask() {
		printf '%s\n' "$1" | script -qec \
			"sh -c '. /tmp/uninstaller.sh; PATH=/stub:\$PATH; main --purge'" /dev/null >/dev/null 2>&1
	}

	reset
	if ask y; then
		printf '  FAIL "y" was taken for confirmation\n'
		FAILURES=$((FAILURES + 1))
	else
		printf '  ok   takes the word, not a keypress\n'
	fi
	check "and deleted nothing for the wrong word" \
		"$([ -d "$DATA_DIR" ] && echo kept || echo gone)" "kept"

	reset
	ask delete || true
	check "the word does confirm" "$([ -d "$DATA_DIR" ] && echo kept || echo gone)" "gone"

	printf '\n'
	[ "$FAILURES" = 0 ] || { printf '%d failure(s)\n\n' "$FAILURES"; exit 1; }
	printf 'uninstall.sh ok\n\n'
}

run_tests "$@"
