#!/bin/sh
# Configures the gateway for the domain Cubeship gives it, then starts it.
#
# OPENCLAW_GATEWAY_TOKEN  the shared secret every client signs in with
# OPENCLAW_PUBLIC_URL     https://<domain>, the one origin the Control UI
#                         may connect from
# PAIR_NEXT_BROWSER=1     approve the next browser even though one is
#                         already paired; remove it once you are back in
set -eu
cd /app

: "${OPENCLAW_GATEWAY_TOKEN:?OPENCLAW_GATEWAY_TOKEN is not set}"
: "${OPENCLAW_PUBLIC_URL:?OPENCLAW_PUBLIC_URL is not set}"

# Written on every start, so these keys always match the app's variables.
# Everything else in openclaw.json is left as the Control UI saved it.
batch=$(node -e 'process.stdout.write(JSON.stringify([
  { path: "gateway.mode", value: "local" },
  { path: "gateway.bind", value: "lan" },
  { path: "gateway.auth.mode", value: "token" },
  { path: "gateway.controlUi.allowedOrigins", value: [process.env.OPENCLAW_PUBLIC_URL] },
]))')
node openclaw.mjs config set --batch-json "$batch"

# A browser that signs in with the token still waits for a one-time device
# approval, normally given from a shell on the gateway host. Cubeship gives
# no shell, so this approves the first Control UI browser that asks — which
# it can only do after that browser presented the token — and then stops.
# Every later browser is approved from Devices in the first one.
approve_first_browser() {
  set +e
  while :; do
    sleep 5
    list=$(node openclaw.mjs devices list --json --token "$OPENCLAW_GATEWAY_TOKEN" 2>/dev/null) || continue
    next=$(printf '%s' "$list" | node -e '
      let s = "";
      process.stdin.on("data", (d) => (s += d)).on("end", () => {
        const l = JSON.parse(s);
        const ui = (d) => d.clientId === "openclaw-control-ui";
        const paired = (l.paired || []).some(ui);
        if (paired && process.env.PAIR_NEXT_BROWSER !== "1") return process.stdout.write("done");
        const request = (l.pending || []).find(ui);
        if (request) process.stdout.write(request.requestId);
      });
    ') || continue
    case "$next" in
      done) return ;;
      "") ;;
      *)
        if node openclaw.mjs devices approve "$next" --token "$OPENCLAW_GATEWAY_TOKEN" >/dev/null 2>&1; then
          echo "cubeship-start: approved the Control UI browser in request $next"
          return
        fi
        ;;
    esac
  done
}
approve_first_browser &

exec node openclaw.mjs gateway
