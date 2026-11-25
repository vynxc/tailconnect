#!/usr/bin/env bash
# End-to-end smoke test using the official Tailcat DERP relays.
# Requires: bin/tailconnect (make build), python3, curl.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BIN="$ROOT/bin/tailconnect"
HTTP_PORT=${HTTP_PORT:-28080}
FORWARD_PORT=${FORWARD_PORT:-28081}
SOCKS_PORT=${SOCKS_PORT:-21080}
TMP=$(mktemp -d)
HTTP_PID=""
SRV_PID=""
AGT_PID=""

cleanup() {
  [ -n "$AGT_PID" ] && kill "$AGT_PID" 2>/dev/null || true
  [ -n "$SRV_PID" ] && kill "$SRV_PID" 2>/dev/null || true
  [ -n "$HTTP_PID" ] && kill "$HTTP_PID" 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

[ -x "$BIN" ] || { echo "build first: make build" >&2; exit 1; }

python3 -m http.server "$HTTP_PORT" --bind 127.0.0.1 >"$TMP/http.log" 2>&1 &
HTTP_PID=$!

"$BIN" serve --allow-loopback --json >"$TMP/serve.log" 2>&1 &
SRV_PID=$!

for _ in $(seq 1 40); do
  CODE=$(python3 - "$TMP/serve.log" <<'PY'
import json, sys
for line in open(sys.argv[1]):
    try:
        o = json.loads(line)
    except Exception:
        continue
    if o.get("type") == "started":
        print(o["code"])
        break
PY
)
  [ -n "${CODE:-}" ] && break
  sleep 1
done
[ -n "${CODE:-}" ] || { echo "server did not print a code" >&2; exit 1; }
echo "code: $CODE"

"$BIN" agent --code "$CODE" --name e2e \
  --socks "127.0.0.1:$SOCKS_PORT" \
  --forward "$FORWARD_PORT=127.0.0.1:$HTTP_PORT" \
  --expose web="127.0.0.1:$HTTP_PORT" >"$TMP/agent.log" 2>&1 &
AGT_PID=$!

for _ in $(seq 1 40); do
  grep -q 'connected=true' "$TMP/agent.log" && break
  sleep 1
done

REVERSE_PORT=""
for _ in $(seq 1 30); do
  REVERSE_PORT=$(python3 - "$TMP/serve.log" <<'PY'
import json, sys
for line in open(sys.argv[1]):
    try:
        o = json.loads(line)
    except Exception:
        continue
    if o.get("type") == "state":
        services = o.get("state", {}).get("services") or []
        if services:
            print(services[0]["port"])
            break
PY
)
  [ -n "$REVERSE_PORT" ] && break
  sleep 1
done
[ -n "$REVERSE_PORT" ] || { echo "agent did not publish a service" >&2; exit 1; }
echo "reverse port: $REVERSE_PORT"

curl -fsS --max-time 10 "http://127.0.0.1:$FORWARD_PORT/" -o "$TMP/forward.html"
curl -fsS --max-time 10 --socks5-hostname "127.0.0.1:$SOCKS_PORT" "http://127.0.0.1:$HTTP_PORT/" -o "$TMP/socks.html"
curl -fsS --max-time 10 "http://127.0.0.1:$REVERSE_PORT/" -o "$TMP/reverse.html"
echo -n "forward: "; head -1 "$TMP/forward.html"
echo -n "socks:   "; head -1 "$TMP/socks.html"
echo -n "reverse: "; head -1 "$TMP/reverse.html"
echo "ok"
