#!/usr/bin/env bash
set -euo pipefail

RPC_URL="$1"
PRIVATE_KEY="$2"
CONTRACT_FQN="$3"
shift 3
CONSTRUCTOR_ARGS=("$@")

wait_for_receipt() {
  local tx_hash="$1"
  while true; do
    raw=$(cast rpc eth_getTransactionReceipt "$tx_hash" --rpc-url "$RPC_URL") || {
      echo "Error calling eth_getTransactionReceipt"
      exit 1
    }

    trimmed=$(echo "$raw" | tr -d '\r\n' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')

    if [[ "$trimmed" != "null" ]]; then
      if echo "$trimmed" | jq -e . >/dev/null 2>&1; then
        echo "$trimmed"
        return 0
      else
        echo "Received non-JSON receipt: $trimmed" >&2
        exit 1
      fi
    fi

    sleep 0.1
  done
}

echo "Deploying $CONTRACT_FQN..."

if (( ${#CONSTRUCTOR_ARGS[@]} > 0 )); then
  JSON_OUTPUT=$(
    forge create \
      "$CONTRACT_FQN" "${CONSTRUCTOR_ARGS[@]}" \
      --legacy \
      --broadcast \
      --rpc-url "$RPC_URL" \
      --private-key "$PRIVATE_KEY" \
      --json
  )
else
  JSON_OUTPUT=$(
    forge create \
      "$CONTRACT_FQN" \
      --legacy \
      --broadcast \
      --rpc-url "$RPC_URL" \
      --private-key "$PRIVATE_KEY" \
      --json
  )
fi

TX_HASH=$(echo "$JSON_OUTPUT" | jq -r '.txHash // .transactionHash')
if [[ -z "$TX_HASH" || "$TX_HASH" == "null" ]]; then
  echo "Failed to get transaction hash from forge output:"
  echo "$JSON_OUTPUT"
  exit 1
fi

echo "Transaction submitted: $TX_HASH"

RECEIPT_JSON=$(wait_for_receipt "$TX_HASH")
CONTRACT_ADDRESS=$(echo "$RECEIPT_JSON" | jq -r '.contractAddress')

if [[ -z "$CONTRACT_ADDRESS" || "$CONTRACT_ADDRESS" == "null" ]]; then
  echo "Could not extract contractAddress from receipt:"
  echo "$RECEIPT_JSON"
  exit 1
fi

echo "Successfully deployed!"
echo "Contract address: $CONTRACT_ADDRESS"