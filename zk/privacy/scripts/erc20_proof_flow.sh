#!/usr/bin/env bash
set -euo pipefail

# End-to-end helper for ERC20Verifier proof flow
#
# Prereqs:
#   - cast, curl, jq installed
#   - PolygonID Issuer Node running and reachable
#   - Contracts deployed: Iden3 State (proxy), MTP v2 validator, ERC20Verifier
#
# Env vars:
#   RPC_URL                RPC endpoint
#   ADMIN_PRIVATE_KEY      Admin PK (to configure on-chain request)
#   ERC20V                 ERC20Verifier address
#   MTP_VALIDATOR          CredentialAtomicQueryMTPV2Validator address
#   REQUEST_ID             ZKP request id to use (default: 2 - MTP)
#   CIRCUIT_ID             default: credentialAtomicQueryMTPV2OnChain
#   QUERY_HASH             numeric query hash matching issuer query (default: 123)
#   ISSUER_URL             Issuer Node base URL (e.g. http://localhost:3001)
#   CALLBACK_URL           Public callback URL (your server) to receive wallet proof
#   SENDER                 EOA that will receive tokens and should be the challenge
#   PROOF_FILE             Path where callback server writes the proof (default: ./proof.json)
#
# Optional query fields (leave empty for permissive):
#   SCHEMA                 uint256 schema id
#   CLAIM_PATH_KEY         uint256 path key
#   OPERATOR               uint256 operator id
#   SLOT_INDEX             uint256 slot index
#   VALUE_HEX              hex-encoded bytes of value (not used by MTP v2 base)
#   ALLOWED_ISSUERS_CSV    comma-separated list of uint256 issuer IDs (empty: allow all)
#   SKIP_REVOCATION_CHECK  true|false (default: true)
#   CLAIM_PATH_NOT_EXISTS  0|1 (default: 0)

hr() { echo "============================================================"; }
die() { echo "[erc20-flow] $*" >&2; exit 1; }

: "${RPC_URL?RPC_URL not set}"
: "${ADMIN_PRIVATE_KEY?ADMIN_PRIVATE_KEY not set}"
: "${ERC20V?ERC20V not set}"
: "${MTP_VALIDATOR?MTP_VALIDATOR not set}"
: "${ISSUER_URL?ISSUER_URL not set}"
: "${CALLBACK_URL?CALLBACK_URL not set}"
: "${SENDER?SENDER not set}"

REQUEST_ID=${REQUEST_ID:-2}
CIRCUIT_ID=${CIRCUIT_ID:-credentialAtomicQueryMTPV2OnChain}
QUERY_HASH=${QUERY_HASH:-123}
PROOF_FILE=${PROOF_FILE:-./proof.json}

SCHEMA=${SCHEMA:-0}
CLAIM_PATH_KEY=${CLAIM_PATH_KEY:-0}
OPERATOR=${OPERATOR:-0}
SLOT_INDEX=${SLOT_INDEX:-0}
VALUE_HEX=${VALUE_HEX:-0x}
ALLOWED_ISSUERS_CSV=${ALLOWED_ISSUERS_CSV:-}
SKIP_REVOCATION_CHECK=${SKIP_REVOCATION_CHECK:-true}
CLAIM_PATH_NOT_EXISTS=${CLAIM_PATH_NOT_EXISTS:-0}

parse_allowed_issuers() {
  local csv="$1"
  if [[ -z "$csv" ]]; then
    echo "[]"; return
  fi
  # Convert CSV to JSON array of numbers
  local arr="["; IFS="," read -r -a parts <<< "$csv"; local first=1
  for p in "${parts[@]}"; do
    if [[ $first -eq 0 ]]; then arr+=" ,"; fi
    arr+="$p"; first=0
  done
  arr+="]"; echo "$arr"
}

ALLOWED_ISSUERS_JSON=$(parse_allowed_issuers "$ALLOWED_ISSUERS_CSV")

echo "[erc20-flow] Configuring on-chain ZKP request (REQUEST_ID=$REQUEST_ID)"

# Build abi-encoded data for CredentialAtomicQuery (v2)
# (uint256 schema,
#  uint256 claimPathKey,
#  uint256 operator,
#  uint256 slotIndex,
#  uint256[] value,
#  uint256 queryHash,
#  uint256[] allowedIssuers,
#  string[] circuitIds,
#  bool skipClaimRevocationCheck,
#  uint256 claimPathNotExists)

VALUE_JSON=${VALUE_JSON:-"[]"}
if [[ "$VALUE_HEX" != "0x" && -n "$VALUE_HEX" ]]; then
  # optional: include hex bytes value as uint256 array is circuit-specific; default empty
  VALUE_JSON="[]"
fi

DATA=$(cast abi-encode \
  "(uint256,uint256,uint256,uint256,uint256[],uint256,uint256[],string[],bool,uint256)" \
  "$SCHEMA" "$CLAIM_PATH_KEY" "$OPERATOR" "$SLOT_INDEX" \
  "$VALUE_JSON" "$QUERY_HASH" "$ALLOWED_ISSUERS_JSON" \
  "[\"$CIRCUIT_ID\"]" "$SKIP_REVOCATION_CHECK" "$CLAIM_PATH_NOT_EXISTS")

# Build ZKPRequest tuple: (string metadata, address validator, bytes data)
META="ERC20 MTPv2"
cast send "$ERC20V" \
  "setZKPRequest(uint64,(string,address,bytes))" \
  "$REQUEST_ID" "$META" "$MTP_VALIDATOR" "$DATA" \
  --rpc-url "$RPC_URL" --private-key "$ADMIN_PRIVATE_KEY" -q

hr
echo "[erc20-flow] Creating verification request on Issuer Node"

# Issuer POST body (adjust path if your version differs)
REQ_BODY=$(jq -n --arg cid "$CIRCUIT_ID" \
             --arg cb "$CALLBACK_URL" \
             --argjson qh "$QUERY_HASH" \
             --arg challenge "$SENDER" \
             --argjson schema "$SCHEMA" \
             --argjson claimPathKey "$CLAIM_PATH_KEY" \
             --argjson operator "$OPERATOR" \
             --argjson slotIndex "$SLOT_INDEX" \
             --argjson value [] \
             --argjson allowedIssuers "$ALLOWED_ISSUERS_JSON" \
             --argjson skipRevocationCheck $( $SKIP_REVOCATION_CHECK && echo true || echo false) \
             --argjson claimPathNotExists "$CLAIM_PATH_NOT_EXISTS" \
             '{
                circuitId: $cid,
                callbackUrl: $cb,
                challenge: $challenge,
                query: {
                  schema: $schema,
                  claimPathKey: $claimPathKey,
                  operator: $operator,
                  slotIndex: $slotIndex,
                  value: $value,
                  queryHash: $qh,
                  allowedIssuers: $allowedIssuers,
                  skipClaimRevocationCheck: $skipRevocationCheck,
                  claimPathNotExists: $claimPathNotExists
                }
              }')

echo "[erc20-flow] Request body:"; echo "$REQ_BODY" | jq .

# NOTE: endpoint path may vary by Issuer Node release. Common patterns:
#   /api/v1/presentations/requests or /v1/verification/requests
PRESENTATION_PATH=${PRESENTATION_PATH:-/api/v1/presentations/requests}
RESP=$(curl -sS -X POST "$ISSUER_URL$PRESENTATION_PATH" \
  -H 'content-type: application/json' \
  -d "$REQ_BODY") || die "Issuer request failed"

echo "[erc20-flow] Issuer response:"; echo "$RESP" | jq . || true

DEEP_LINK=$(echo "$RESP" | jq -r '.deeplink // .url // empty')
QR_URL=$(echo "$RESP" | jq -r '.qrUrl // empty')
echo "[erc20-flow] Deep link (scan in PolygonID wallet): $DEEP_LINK"
[[ -n "$QR_URL" ]] && echo "[erc20-flow] QR URL: $QR_URL"

hr
echo "[erc20-flow] Waiting for proof JSON at $PROOF_FILE (from your callback server)"
echo "[erc20-flow] Tip: run: python3 zk/privacy/tools/callback_server.py --output $PROOF_FILE --port 8787"

while [[ ! -f "$PROOF_FILE" ]]; do sleep 2; done
echo "[erc20-flow] Proof file detected"

# Expecting V2 format: { requestId, zkProof: 0x..., data: 0x } or with arrays (inputs,a,b,c)
ZKPROOF_HEX=$(jq -r '.zkProof // empty' "$PROOF_FILE")
if [[ -n "$ZKPROOF_HEX" && "$ZKPROOF_HEX" != "null" ]]; then
  echo "[erc20-flow] Submitting V2 packed proof"
  RESP_TX=$(cast send "$ERC20V" \
    "submitZKPResponseV2((uint64,bytes,bytes)[],bytes)" \
    "[(\"$REQUEST_ID\",$ZKPROOF_HEX,0x)]" 0x \
    --rpc-url "$RPC_URL" --private-key "$ADMIN_PRIVATE_KEY")
  echo "$RESP_TX"
else
  echo "[erc20-flow] Parsing classic arrays from proof JSON"
  INPUTS=$(jq -c '.inputs' "$PROOF_FILE")
  A=$(jq -c '.a' "$PROOF_FILE")
  B=$(jq -c '.b' "$PROOF_FILE")
  C=$(jq -c '.c' "$PROOF_FILE")
  [[ -z "$INPUTS" || -z "$A" || -z "$B" || -z "$C" ]] && die "Proof JSON missing required fields"
  RESP_TX=$(cast send "$ERC20V" \
    "submitZKPResponse(uint64,uint256[],uint256[2],uint256[2][2],uint256[2])" \
    "$REQUEST_ID" "$INPUTS" "$A" "$B" "$C" \
    --rpc-url "$RPC_URL" --private-key "$ADMIN_PRIVATE_KEY")
  echo "$RESP_TX"
fi

hr
echo "[erc20-flow] Checking proof status and balance"
cast call "$ERC20V" "isProofVerified(address,uint64)(bool)" "$SENDER" "$REQUEST_ID" --rpc-url "$RPC_URL"
cast call "$ERC20V" "balanceOf(address)(uint256)" "$SENDER" --rpc-url "$RPC_URL"

echo "[erc20-flow] Done. If balance minted and transfer to $SENDER works, flow is successful."

