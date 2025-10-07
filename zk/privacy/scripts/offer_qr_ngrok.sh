#!/usr/bin/env bash
set -euo pipefail

# Rewrite Issuer offer links to use an ngrok public URL and print QR codes.
#
# Prereqs:
#   - ngrok running for port 3001 (or set NGROK_URL manually)
#   - jq, curl, python3, qrencode installed
#
# Env vars (required):
#   ISSUER_URL   e.g. http://localhost:3001
#   ISSUER_USER  basic auth user
#   ISSUER_PASS  basic auth password
#   IDENT        issuer identity DID (string)
#   LINK_ID      credentials link id (uuid)
#
# Optional:
#   NGROK_URL    override ngrok public base URL (e.g. https://abcd.ngrok-free.app)
#   NGROK_API    ngrok local API (default: http://127.0.0.1:4040)
#
# Usage:
#   export ISSUER_URL=http://localhost:3001
#   export ISSUER_USER=user-issuer
#   export ISSUER_PASS=password-issuer
#   export IDENT=<issuer_did>
#   export LINK_ID=<link_uuid>
#   # Ensure: ngrok http 3001 (and qrencode installed)
#   bash zk/privacy/scripts/offer_qr_ngrok.sh

die() { echo "[offer-ngrok] $*" >&2; exit 1; }

: "${ISSUER_URL?ISSUER_URL not set}"
: "${ISSUER_USER?ISSUER_USER not set}"
: "${ISSUER_PASS?ISSUER_PASS not set}"
: "${IDENT?IDENT not set}"
: "${LINK_ID?LINK_ID not set}"

NGROK_API=${NGROK_API:-http://127.0.0.1:4040}

have() { command -v "$1" >/dev/null 2>&1; }
for bin in jq curl python3; do have "$bin" || die "missing dependency: $bin"; done

# Resolve ngrok public URL if not provided
if [[ -z "${NGROK_URL:-}" ]]; then
  echo "[offer-ngrok] Discovering ngrok public URL from $NGROK_API"
  tunnels_json=$(curl -fsS "$NGROK_API/api/tunnels" || true)
  [[ -n "$tunnels_json" ]] || die "ngrok API not reachable. Start: ngrok http 3001"
  # Prefer https tunnel that forwards to localhost:3001
  NGROK_URL=$(echo "$tunnels_json" | jq -r '
    .tunnels
    | map(select((.config.addr|tostring|test("localhost:3001|127.0.0.1:3001"))))
    | (map(select(.public_url|startswith("https:"))) + .)
    | .[0].public_url // empty')
  [[ -n "$NGROK_URL" ]] || die "no ngrok tunnel forwarding to localhost:3001 found"
fi

echo "[offer-ngrok] Using NGROK_URL=$NGROK_URL"

# Request offer from Issuer Node
echo "[offer-ngrok] Fetching offer for LINK_ID=$LINK_ID"
offer=$(curl -fsS -u "$ISSUER_USER:$ISSUER_PASS" -X POST \
  "$ISSUER_URL/v2/identities/$IDENT/credentials/links/$LINK_ID/offer") || die "offer request failed"

# Try deepLink first for easier parsing; fall back to universalLink
deepLink=$(echo "$offer" | jq -r '.deepLink // empty')
universalLink=$(echo "$offer" | jq -r '.universalLink // empty')
[[ -n "$deepLink$universalLink" ]] || { echo "$offer" | jq . || true; die "offer missing deepLink/universalLink"; }

extract_request_uri_from_universal() {
  # universal link format: https://wallet.privado.id#request_uri=<ENCODED>
  python3 - "$1" << 'PY'
import sys, urllib.parse
link = sys.argv[1]
frag = urllib.parse.urlparse(link).fragment
qs = urllib.parse.parse_qs(frag, keep_blank_values=True)
vals = qs.get('request_uri', [])
print(vals[0] if vals else(''))
PY
}

extract_request_uri_from_deeplink() {
  # deeplink format: iden3comm://?request_uri=<ENCODED>
  python3 - "$1" << 'PY'
import sys, urllib.parse
link = sys.argv[1]
qs = urllib.parse.parse_qs(urllib.parse.urlparse(link).query, keep_blank_values=True)
vals = qs.get('request_uri', [])
print(vals[0] if vals else(''))
PY
}

url_decode() { python3 -c 'import sys,urllib.parse; print(urllib.parse.unquote(sys.stdin.read().strip()))'; }
url_encode() { python3 -c 'import sys,urllib.parse; print(urllib.parse.quote(sys.stdin.read(), safe=""))'; }

encoded_req_uri=""
if [[ -n "$deepLink" ]]; then
  encoded_req_uri=$(extract_request_uri_from_deeplink "$deepLink")
fi
if [[ -z "$encoded_req_uri" && -n "$universalLink" ]]; then
  encoded_req_uri=$(extract_request_uri_from_universal "$universalLink")
fi
[[ -n "$encoded_req_uri" ]] || die "could not extract request_uri from offer"

decoded_req_uri=$(printf "%s" "$encoded_req_uri" | url_decode)

# Replace localhost base with NGROK_URL
replaced_req_uri=$(printf "%s" "$decoded_req_uri" | sed -E \
  -e "s#https?://localhost:3001#$NGROK_URL#g" \
  -e "s#https?://127.0.0.1:3001#$NGROK_URL#g")

if [[ "$decoded_req_uri" == "$replaced_req_uri" ]]; then
  echo "[offer-ngrok] Warning: no localhost:3001 found in request_uri; leaving as is"
fi

encoded_replaced=$(printf "%s" "$replaced_req_uri" | url_encode)

new_universal="https://wallet.privado.id#request_uri=$encoded_replaced"
new_deep="iden3comm://?request_uri=$encoded_replaced"

echo "[offer-ngrok] New universalLink: $new_universal"
echo "[offer-ngrok] New deepLink     : $new_deep"

if have qrencode; then
  echo "[offer-ngrok] Rendering QR (universalLink)"
  qrencode -t ANSIUTF8 "$new_universal" || true
else
  echo "[offer-ngrok] qrencode not found. Install: brew install qrencode (mac) or apt-get install qrencode (linux)"
fi

echo "[offer-ngrok] Done. Open the universalLink on your phone running Privado ID Wallet."

