#!/usr/bin/env bash
set -euo pipefail

# Requirements: curl, jq; qrencode is optional (for terminal QR)
need() { command -v "$1" >/dev/null 2>&1 || { echo "Missing tool: $1"; exit 1; }; }
need curl
need jq
if ! command -v qrencode >/dev/null 2>&1; then
  QR_MISSING=1
else
  QR_MISSING=0
fi

# ---- Config ----
: "${RPC_URL:?Set RPC_URL (e.g. http://localhost:8545)}"

# Defaults (can be overridden via env or CLI)
TO="${TO:-}"
DATA="${DATA:-0x70a082310000000000000000000000000000000000000000000000000000000000000000}"
BLOCKTAG="${BLOCKTAG:-latest}"
ID="${ID:-1}"

# Allow CLI overrides: ./zk_build_verifier_link.sh --to 0x.. --data 0x.. --block latest
while [[ $# -gt 0 ]]; do
  case "$1" in
    --to) TO="$2"; shift 2 ;;
    --data) DATA="$2"; shift 2 ;;
    --block|--blocktag) BLOCKTAG="$2"; shift 2 ;;
    --id) ID="$2"; shift 2 ;;
    --rpc-url) RPC_URL="$2"; shift 2 ;;
    --help|-h)
      cat <<EOF
Usage: $0 [--rpc-url URL] [--to ADDR] [--data HEX] [--blocktag TAG] [--id N]

Env vars:
  RPC_URL   (required)
  TO        default: $TO
  DATA      default: $DATA
  BLOCKTAG  default: $BLOCKTAG
  ID        default: $ID

Examples:
  RPC_URL=http://localhost:8545 $0
  $0 --rpc-url http://localhost:8545 --to 0xabc... --data 0x70a08231... --blocktag latest
EOF
      exit 0 ;;
    *)
      echo "Unknown arg: $1" >&2; exit 2 ;;
  esac
done

# ---- Perform the eth_call ----
REQ=$(jq -n \
  --arg to "$TO" \
  --arg data "$DATA" \
  --arg tag "$BLOCKTAG" \
  --argjson id "$ID" \
  '{jsonrpc:"2.0", id:$id, method:"eth_call", params:[{to:$to, data:$data}, $tag] }')

RESP=$(curl -sS -X POST "$RPC_URL" -H 'content-type: application/json' -d "$REQ")

# Pretty-print response for debugging
echo "$RESP" | jq . || true

# Check for verification-required error (-32051) and extract fields
CODE=$(echo "$RESP" | jq -r '.error.code // empty')
if [[ "$CODE" != "-32051" ]]; then
  echo "No verification required (error.code=$CODE)."
  exit 0
fi

CHALLENGE_ID=$(echo "$RESP" | jq -r '.error.data.challengeId // empty')
EXPIRES_AT=$(echo "$RESP" | jq -r '.error.data.expiresAt // empty')
VERIFIER_REQ_URL=$(echo "$RESP" | jq -r '.error.data.url // empty')

if [[ -z "$VERIFIER_REQ_URL" || "$VERIFIER_REQ_URL" == "null" ]]; then
  echo "ERROR: Could not extract verifier request URL from response."
  exit 1
fi

export VERIFIER_REQ_URL
echo
echo "challengeId: $CHALLENGE_ID"
echo "expiresAt:   $EXPIRES_AT"
echo "verifierURL: $VERIFIER_REQ_URL"

# Build universal link for Privado wallet
ENC_REQ_URL=$(printf %s "$VERIFIER_REQ_URL" | jq -sRr @uri)
UNIVERSAL="https://wallet.privado.id#request_uri=$ENC_REQ_URL"
echo
echo "universalLink: $UNIVERSAL"

# Show QR if possible
if [[ "$QR_MISSING" -eq 0 ]]; then
  echo
  echo "== QR (scan in Privado / PolygonID wallet) =="
  printf '%s' "$UNIVERSAL" | qrencode -t ANSIUTF8
else
  echo "(qrencode not found; install it to render a terminal QR: e.g., brew install qrencode)"
fi
