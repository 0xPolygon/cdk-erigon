#!/usr/bin/env bash
set -euo pipefail

# ===== Required env =====
: "${ISSUER_URL:?Set ISSUER_URL (e.g. http://192.168.1.134:3001)}"
: "${ISSUER_USER:?Set ISSUER_USER}"
: "${ISSUER_PASS:?Set ISSUER_PASS}"

# ===== Optional env =====
DISPLAY_NAME="${DISPLAY_NAME:-Dev Issuer}"

SCHEMA_URL="${SCHEMA_URL:-https://raw.githubusercontent.com/iden3/claim-schema-vocab/main/schemas/json/non-zero-balance.json}"
SCHEMA_TYPE="${SCHEMA_TYPE:-Balance}"
SCHEMA_VERSION="${SCHEMA_VERSION:-1.0}"

EOA_ADDRESS="${EOA_ADDRESS:-}"
BALANCE_VALUE="${BALANCE_VALUE:-1}"

OUTDIR="${OUTDIR:-/tmp/iden3_flow_exact}"
mkdir -p "$OUTDIR"

IDENT_LIST_JSON="$OUTDIR/identities.json"
IDENT_JSON="$OUTDIR/identity_chosen.json"
KEY_JSON="$OUTDIR/key_create_resp.json"
KEYS_LIST_JSON="$OUTDIR/keys_list.json"
AUTHCRED_JSON="$OUTDIR/issuer_auth_cred_resp.json"
SCHEMA_RESP_JSON="$OUTDIR/schema_resp.json"
SCHEMA_LIST_JSON="$OUTDIR/schemas_list.json"
LINK_RESP_JSON="$OUTDIR/link_resp.json"

need() { command -v "$1" >/dev/null 2>&1 || { echo "Missing tool: $1"; exit 1; }; }
need curl
need jq
command -v qrencode >/dev/null 2>&1 || echo "Note: install 'qrencode' to render terminal QR (brew install qrencode)."

urlencode() {
  python3 - "$1" <<'PY'
import sys, urllib.parse
print(urllib.parse.quote(sys.argv[1], safe=''))
PY
}

echo "== Issuer Node: $ISSUER_URL"
echo "== Identity displayName: $DISPLAY_NAME"
echo

# ===== A) Ensure / resolve identity =====
curl -s -u "$ISSUER_USER:$ISSUER_PASS" "$ISSUER_URL/v2/identities" \
  | tee "$IDENT_LIST_JSON" >/dev/null

FOUND=$(jq -r --arg name "$DISPLAY_NAME" '.[] | select(.displayName==$name) | @base64' "$IDENT_LIST_JSON" | head -n1 || true)
if [ -n "$FOUND" ]; then
  echo "Found existing identity \"$DISPLAY_NAME\""
  echo "$FOUND" | base64 --decode | tee "$IDENT_JSON" | jq .
else
  echo "Creating identity \"$DISPLAY_NAME\""
  REQ=$(jq -n \
    --arg method "polygonid" --arg blockchain "polygon" --arg network "cardona" --arg type "BJJ" \
    --arg credStat "Iden3commRevocationStatusV1.0" --arg name "$DISPLAY_NAME" \
    '{ didMetadata:{method:$method,blockchain:$blockchain,network:$network,type:$type},
       credentialStatusType:$credStat, displayName:$name }')
  curl -sS -u "$ISSUER_USER:$ISSUER_PASS" -H 'content-type: application/json' \
    -d "$REQ" "$ISSUER_URL/v2/identities" | tee "$IDENT_JSON" | jq .
fi

IDENT="$(jq -r '.identifier // empty' "$IDENT_JSON")"
if [ -z "$IDENT" ] || [ "$IDENT" = "null" ]; then
  echo "ERROR: could not resolve issuer DID (.identifier)"; exit 1
fi
echo "DID: $IDENT"
echo

# ===== B) Ensure a BabyJubJub auth key =====
echo "== Ensure BJJ key =="
KEY_NAME="${KEY_NAME:-auth-key-1}"

KEY_ID=$(curl -sS -u "$ISSUER_USER:$ISSUER_PASS" -H 'content-type: application/json' \
  -d '{"keyType":"babyjubJub","name":"'"$KEY_NAME"'"}' \
  "$ISSUER_URL/v2/identities/$IDENT/keys" | tee "$KEY_JSON" | jq -r '.id // empty')

if [ -z "$KEY_ID" ]; then
  echo "Create didn't return id; listing keys to reuse by name=$KEY_NAME"
  curl -sS -u "$ISSUER_USER:$ISSUER_PASS" \
    "$ISSUER_URL/v2/identities/$IDENT/keys" | tee "$KEYS_LIST_JSON" >/dev/null || true

  KEY_ID=$(jq -r --arg n "$KEY_NAME" '
    if type=="object" and .items then
      .items[] | select(.name==$n) | .id
    else
      .[] | select(.name==$n) | .id
    end
  ' "$KEYS_LIST_JSON" | head -n1 || true)
fi

if [ -z "$KEY_ID" ]; then
  echo "No key named $KEY_NAME; reusing any existing BJJ auth key"
  KEY_ID=$(jq -r '
    if type=="object" and .items then
      .items[] | select((.keyType|ascii_downcase)=="babyjubjub" and (.isAuthCredential==true)) | .id
    else
      .[] | select((.keyType|ascii_downcase)=="babyjubjub" and (.isAuthCredential==true)) | .id
    end
  ' "$KEYS_LIST_JSON" | head -n1 || true)
fi

if [ -z "$KEY_ID" ]; then
  echo "ERROR: could not determine KEY_ID"; exit 1
fi
echo "KEY_ID: $KEY_ID"
echo

# ===== C) Ensure issuer-auth credential =====
echo "== Ensure issuer-auth credential =="
HAS_AUTH=$(jq -r '
  if type=="object" and .items then
    any(.items[]; .isAuthCredential==true)
  else
    any(.[]; .isAuthCredential==true)
  end
' "$KEYS_LIST_JSON" 2>/dev/null || echo "false")

if [ "$HAS_AUTH" = "true" ]; then
  echo "Auth credential already present — skipping create-auth-credential."
else
  curl -sS -u "$ISSUER_USER:$ISSUER_PASS" -H 'content-type: application/json' \
    -d '{"keyID":"'"$KEY_ID"'","credentialStatusType":"Iden3commRevocationStatusV1.0"}' \
    "$ISSUER_URL/v2/identities/$IDENT/create-auth-credential" | tee "$AUTHCRED_JSON" | jq . || true
fi
echo

# ===== D) Get schema ID (match by .url/.type/.version) =====
echo "== Get schema ID =="
# First try POST (some nodes return {id}, others just say "already imported")
SCHEMA_ID=$(curl -sS -u "$ISSUER_USER:$ISSUER_PASS" -H 'content-type: application/json' \
  -d '{"url":"'"$SCHEMA_URL"'","schemaType":"'"$SCHEMA_TYPE"'","version":"'"$SCHEMA_VERSION"'"}' \
  "$ISSUER_URL/v2/identities/$IDENT/schemas" | tee "$SCHEMA_RESP_JSON" | jq -r '.id // empty')

if [ -z "$SCHEMA_ID" ]; then
  echo "Schema id not returned; trying list match."
  curl -sS -u "$ISSUER_USER:$ISSUER_PASS" "$ISSUER_URL/v2/identities/$IDENT/schemas" \
    | tee "$SCHEMA_LIST_JSON" >/dev/null || true

  echo "Full schema list:"
  cat "$SCHEMA_LIST_JSON" | jq .

  # Match either bare array or {items:[...]}; fields are .url/.type/.version per your output
  SCHEMA_ID=$(jq -r --arg url "$SCHEMA_URL" --arg t "$SCHEMA_TYPE" --arg v "$SCHEMA_VERSION" '
    def pick(it): it | select((.url==$url) or (.type==$t and .version==$v)) | .id;
    if type=="object" and .items then
      (.items[] | pick(.)) // empty
    else
      (.[] | pick(.)) // empty
    end
  ' "$SCHEMA_LIST_JSON" | head -n1 || true)
fi

if [ -z "$SCHEMA_ID" ]; then
  echo "ERROR: could not resolve SCHEMA_ID"; exit 1
fi
echo "SCHEMA_ID: $SCHEMA_ID"
echo

# ===== E) Create credential link =====
echo "== Create credential link =="
LINK_ID=$(curl -sS -u "$ISSUER_USER:$ISSUER_PASS" -H 'content-type: application/json' \
  -d '{"schemaID":"'"$SCHEMA_ID"'","signatureProof":true,"mtProof":false,"limitedClaims":1,"credentialSubject":{"address":"'"$EOA_ADDRESS"'","balance":"'"$BALANCE_VALUE"'"}}' \
  "$ISSUER_URL/v2/identities/$IDENT/credentials/links" | tee "$LINK_RESP_JSON" | jq -r '.id // empty')

if [ -z "$LINK_ID" ]; then
  echo "ERROR: could not get LINK_ID from response"; exit 1
fi
echo "LINK_ID: $LINK_ID"
echo

# ===== F) Build request_uri and universal link; print QR =====
REQ_URI="$ISSUER_URL/v2/qr-store?id=$LINK_ID&issuer=$IDENT"
REQ_URI_ENC="$(urlencode "$REQ_URI")"
UNIVERSAL="https://wallet.privado.id#request_uri=$REQ_URI_ENC"
IDEN3COMM="iden3comm://?request_uri=$REQ_URI_ENC"

echo "request_uri:   $REQ_URI"
echo "universalLink: $UNIVERSAL"
echo "iden3comm:     $IDEN3COMM"

if command -v qrencode >/dev/null 2>&1; then
  echo
  echo "== QR (scan with Privado / PolygonID) =="
  printf '%s' "$UNIVERSAL" | qrencode -t ANSIUTF8
fi

echo
echo "Saved:"
echo "  Identity list: $IDENT_LIST_JSON"
echo "  Identity:      $IDENT_JSON"
echo "  Keys list:     $KEYS_LIST_JSON"
echo "  Key resp:      $KEY_JSON"
echo "  Auth cred:     $AUTHCRED_JSON"
echo "  Schema Resp:   $SCHEMA_RESP_JSON"
echo "  Schema List:   $SCHEMA_LIST_JSON"
echo "  Link resp:     $LINK_RESP_JSON"
