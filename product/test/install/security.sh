#!/bin/sh
# Portable installer security regression: every external operation is stubbed.
set -eu
LC_ALL=C
export LC_ALL
repo=$(CDPATH='' cd -- "$(dirname -- "$0")/../../.." && pwd)
workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT HUP INT TERM
sed '$d' "$repo/install.sh" > "$workspace/installer.sh"
sed '$d' "$repo/uninstall.sh" > "$workspace/uninstaller.sh"
export CUBESHIP_DOMAIN="" CUBESHIP_CONTROL_PLANE="" CUBESHIP_NODE_TOKEN=""
export CUBESHIP_DATA_DIR="$workspace/data" CUBESHIP_VERSION=testing
export RECORDING="$workspace/docker.log"
(
	. "$workspace/installer.sh"
	docker() { printf '%s\n' "$*" >> "$RECORDING"; }
	require_root() { :; }
	require_linux() { :; }
	ensure_docker() { :; }
	wait_for_health() { :; }
	wait_for_join() { :; }
	public_ip() { printf '203.0.113.7\n'; }
	address() { printf '203.0.113.7\n'; }
	main > "$workspace/output"
	grep -qx 'network create cubeship' "$RECORDING"
	grep -qx 'network create cubeship-management' "$RECORDING"
	grep -q -- '--network cubeship-management ' "$RECORDING"
	grep -q -- '-p 127.0.0.1:3000:3000 ' "$RECORDING"
	if grep -q -- '--network cubeship ' "$RECORDING"; then exit 1; fi
	grep -q 'https://203-0-113-7.sslip.io' "$workspace/output"
	grep -q 'ssh -L 3000:127.0.0.1:3000 root@203.0.113.7' "$workspace/output"
	grep -q 'http://localhost:3000' "$workspace/output"
	if grep -q 'http://203.0.113.7:3000' "$workspace/output"; then exit 1; fi
	public_ip() { return 1; }
	DOMAIN=''
	main > "$workspace/output"
	grep -q 'ssh -L 3000:127.0.0.1:3000 root@203.0.113.7' "$workspace/output"
	grep -q 'http://localhost:3000' "$workspace/output"
	: > "$RECORDING"
	main --control-plane https://cube.example.com --token test-token > "$workspace/output"
	grep -q -- '--network cubeship-management ' "$RECORDING"
	if grep -q -- ' -p ' "$RECORDING"; then exit 1; fi
)
(
	. "$workspace/uninstaller.sh"
	docker() { printf '%s\n' "$*" >> "$RECORDING"; }
	: > "$RECORDING"
	remove_network
	grep -qx 'network rm cubeship' "$RECORDING"
	grep -qx 'network rm cubeship-management' "$RECORDING"
)
printf 'installer network and recovery checks passed\n'
