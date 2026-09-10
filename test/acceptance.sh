#!/usr/bin/env bash
# End-to-end acceptance for the engine, run against a live instance.
#
# Proves what the README claims: a fresh Omarchy computer, driven entirely over
# HTTP, with nothing left behind afterwards. Run the engine first, then:
#
#   HYPERWAKE_HOME=... test/acceptance.sh
set -euo pipefail

BASE="${HYPERWAKE_BASE:-http://127.0.0.1:4141}"
HOME_DIR="${HYPERWAKE_HOME:?set HYPERWAKE_HOME to the engine state directory}"
TOKEN="$(cat "$HOME_DIR/token")"

api() {
  local method="$1" path="$2" body="${3:-}"
  if [ -n "$body" ]; then
    curl -sS -X "$method" "$BASE$path" -H "Authorization: Bearer $TOKEN" \
      -H 'content-type: application/json' -d "$body"
  else
    curl -sS -X "$method" "$BASE$path" -H "Authorization: Bearer $TOKEN"
  fi
}

field() { python3 -c "import sys,json;d=json.load(sys.stdin);print(d['data']$1)"; }
step()  { printf '  %-34s' "$1"; }
ok()    { printf 'ok  %s\n' "${1:-}"; }

echo
echo "Hyperwake engine acceptance"
echo

step "unauthenticated request refused"
code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/v1")
[ "$code" = "401" ] || { echo "FAILED (got $code)"; exit 1; }
ok

step "create a fresh Omarchy computer"
id=$(api POST /v1/machines '{"name":"acceptance"}' | field "['id']")
ok "$id"

step "reaches ready"
started=$(date +%s)
for _ in $(seq 1 60); do
  status=$(api GET "/v1/machines/$id" | field "['status']")
  [ "$status" = "ready" ] && break
  sleep 2
done
[ "$status" = "ready" ] || { echo "FAILED (status=$status)"; exit 1; }
ok "$(( $(date +%s) - started ))s"

step "it is really Omarchy"
out=$(api POST "/v1/machines/$id/actions" '{"action":"exec","command":"grep -o \"Arch Linux ARM\" /etc/os-release | head -1; pgrep -c Hyprland"}' | field "['stdout']")
echo "$out" | grep -q "Arch Linux ARM" || { echo "FAILED (no Arch)"; exit 1; }
ok "$(echo "$out" | tr '\n' ' ')"

step "write then read a file"
api POST "/v1/machines/$id/actions" '{"action":"write_file","path":"~/acceptance.txt","content":"round trip\n"}' > /dev/null
# Compared in Python: the shell strips trailing newlines, and the trailing
# newline is exactly the byte a file API most often loses.
api POST "/v1/machines/$id/actions" '{"action":"read_file","path":"~/acceptance.txt"}' \
  | python3 -c "
import sys,json,base64
got = base64.b64decode(json.load(sys.stdin)['data']['content_base64'])
assert got == b'round trip\n', 'FAILED, got %r' % got
" || { echo "FAILED (bytes differ)"; exit 1; }
ok "exact bytes preserved"

step "mints a desktop URL"
url=$(api POST "/v1/machines/$id/desktop" '' | field "['desktop_url']")
case "$url" in *"/desktop#t="*) ok "ticket in fragment";; *) echo "FAILED ($url)"; exit 1;; esac

step "desktop ticket is single use"
ticket="${url##*#t=}"
first=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Connection: Upgrade' -H 'Upgrade: websocket' \
  -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  "$BASE/desktop/socket?t=$ticket" || true)
second=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Connection: Upgrade' -H 'Upgrade: websocket' \
  -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' \
  "$BASE/desktop/socket?t=$ticket" || true)
[ "$second" = "401" ] || { echo "FAILED (replay returned $second)"; exit 1; }
ok "replay refused"

step "stop"
api POST "/v1/machines/$id/stop" '{"force":true}' > /dev/null
ok

step "delete"
# Asserted, not assumed. A delete whose response nobody reads is how a
# machine survives a test that claims to have removed it.
code=$(curl -sS -o /tmp/hyperwake-delete.json -w '%{http_code}' -X DELETE "$BASE/v1/machines/$id" -H "Authorization: Bearer $TOKEN")
[ "$code" = "200" ] || { echo "FAILED (HTTP $code: $(cat /tmp/hyperwake-delete.json))"; exit 1; }
ok

step "nothing left behind"
# Scoped to the machine this run created. A global count fails whenever the
# engine happens to hold anything else, which is a false alarm rather than a
# leak -- and a check that cries wolf is a check people learn to ignore.
still_registered=$(api GET /v1/machines | python3 -c "
import sys, json
print(sum(1 for m in json.load(sys.stdin)['data'] if m['id'] == '$id'))")
still_on_disk=$([ -d "$HOME_DIR/runtime/machines/$id" ] && echo 1 || echo 0)
others=$(api GET /v1/machines | python3 -c "import sys,json;print(len(json.load(sys.stdin)['data']))")
[ "$still_registered" = "0" ] && [ "$still_on_disk" = "0" ] \
  || { echo "FAILED (registry=$still_registered disk=$still_on_disk)"; exit 1; }
ok "gone from registry and disk${others:+ · $others other machine(s) untouched}"

echo
echo "  all checks passed"
echo
