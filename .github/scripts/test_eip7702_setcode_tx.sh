#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<EOF
Usage: $0 \
  --rpc-url <url> \
  --private-key-auth-list <hex> \
  --private-key-sender <hex> \
  --contract <path:ContractName> \
  --value <uint> \
  [--constructor-args <arg1,arg2,...>]

Options:
  --rpc-url                   L2 RPC endpoint URL
  --private-key-auth-list     Hex key for auth-list signing (0x...)
  --private-key-sender        Hex key for tx sender (0x...)
  --contract                  Fully-qualified contract (e.g., "contracts/storage.sol:Storage")
  --value                     New uint256 value for setValue()
  --constructor-args          Comma-separated constructor args (optional)
  -h, --help                  Show this help and exit
EOF
  exit 1
}

RPC_URL="" PRIVATE_KEY_AUTH_LIST="" PRIVATE_KEY_SENDER="" CONTRACT_FQN="" VALUE=""
declare -a CONSTRUCTOR_ARGS=()

while [[ $# -gt 0 ]]; do
  case $1 in
    --rpc-url)                RPC_URL="$2"; shift 2;;
    --private-key-auth-list)  PRIVATE_KEY_AUTH_LIST="$2"; shift 2;;
    --private-key-sender)     PRIVATE_KEY_SENDER="$2"; shift 2;;
    --contract)               CONTRACT_FQN="$2"; shift 2;;
    --value)                  VALUE="$2"; shift 2;;
    --constructor-args)       IFS=',' read -r -a CONSTRUCTOR_ARGS <<<"$2"; shift 2;;
    -h|--help)                usage;;
    *) echo "Unknown option $1"; usage;;
  esac
done

if [[ -z "$RPC_URL" || -z "$PRIVATE_KEY_AUTH_LIST" || -z "$PRIVATE_KEY_SENDER" || -z "$CONTRACT_FQN" || -z "$VALUE" ]]; then
  echo "Error: missing required flags"
  usage
fi

echo "Deploying ${CONTRACT_FQN}..."
if (( ${#CONSTRUCTOR_ARGS[@]} > 0 )); then
  JSON_OUT=$(forge create "$CONTRACT_FQN" "${CONSTRUCTOR_ARGS[@]}" \
    --rpc-url "$RPC_URL" \
    --private-key "$PRIVATE_KEY_SENDER" \
    --legacy --broadcast --json --evm-version "london")
else
  JSON_OUT=$(forge create "$CONTRACT_FQN" \
    --rpc-url "$RPC_URL" \
    --private-key "$PRIVATE_KEY_SENDER" \
    --legacy --broadcast --json --evm-version "london")
fi

TX_HASH=$(echo "$JSON_OUT" | jq -r '.txHash // .transactionHash')
echo "Deploy transaction hash: $TX_HASH"

wait_for_receipt() {
  local tx_hash="$1"
  while true; do
    local r
    r=$(cast rpc eth_getTransactionReceipt "$tx_hash" --rpc-url "$RPC_URL" 2>/dev/null) || sleep 1
    [[ "$r" != "null" ]] && { echo "$r"; return; }
  done
}

RECEIPT=$(wait_for_receipt "$TX_HASH")
CONTRACT_ADDRESS=$(echo "$RECEIPT" | jq -r '.contractAddress')
echo "Contract deployed at: $CONTRACT_ADDRESS"

echo "Signing authorization list for setValue()..."
SIGNED_AUTH=$(
  cast wallet sign-auth \
    "$CONTRACT_ADDRESS" \
    --private-key "$PRIVATE_KEY_AUTH_LIST" \
    --rpc-url "$RPC_URL"
)
echo "Signed auth: $SIGNED_AUTH"

echo "Sending setValue($VALUE)..."
SEND_OUT=$(cast send \
  "$CONTRACT_ADDRESS" \
  "setValue(uint256)" "$VALUE" \
  --rpc-url "$RPC_URL" \
  --private-key "$PRIVATE_KEY_SENDER" \
  --gas-limit 100000 \
  --auth "$SIGNED_AUTH" \
  --json)

TX_HASH2=$(echo "$SEND_OUT" | jq -r '.transactionHash')
echo "Transaction hash: $TX_HASH2"

RECEIPT2=$(wait_for_receipt "$TX_HASH2")

TX_TYPE=$(echo "$RECEIPT2" | jq -r '.type')
if [[ "$TX_TYPE" != "0x4" ]]; then
  echo "Error: unexpected tx type ($TX_TYPE), expected 4 for EIP-7702" >&2
  exit 1
fi

STATUS=$(echo "$RECEIPT2" | jq -r '.status')
if [[ "$STATUS" -ne 1 ]]; then
  echo "Error: transaction failed (status=$STATUS)" >&2
  exit 1
fi

echo "Success! Tx was type=$TX_TYPE and completed with status=$STATUS."
echo "EIP-7702 auth list was applied (type 4)."
