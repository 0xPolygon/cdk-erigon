#!/usr/bin/env bash
set -euo pipefail

RPC_URL="$1"
PRIVATE_KEY="$2"
CONTRACT_NAME="$3"
shift 3
CONSTRUCTOR_ARGS=("$@")

echo "Compiling contracts..."
forge build

echo "Extracting bytecode for '$CONTRACT_NAME'..."
BYTECODE="$(forge inspect "$CONTRACT_NAME" bytecode)"
if [[ -z "$BYTECODE" ]]; then
  echo "Error: No bytecode found for contract '$CONTRACT_NAME'."
  exit 1
fi

echo "Deploying '$CONTRACT_NAME' to $RPC_URL..."
if (( ${#CONSTRUCTOR_ARGS[@]} > 0 )); then
  TX_HASH="$(cast send \
    --rpc-url "$RPC_URL" \
    --private-key "$PRIVATE_KEY" \
    --create "$BYTECODE" \
    --constructor-args "${CONSTRUCTOR_ARGS[@]}")"
else
  TX_HASH="$(cast send \
    --rpc-url "$RPC_URL" \
    --private-key "$PRIVATE_KEY" \
    --create "$BYTECODE")"
fi

echo "Transaction submitted: $TX_HASH"

echo "Waiting for deployment to be mined..."
RECEIPT_JSON="$(cast wait "$TX_HASH" --rpc-url "$RPC_URL" --json)"
CONTRACT_ADDRESS="$(echo "$RECEIPT_JSON" | jq -r .contractAddress)"

echo "Successfully deployed!"
echo "Contract address: $CONTRACT_ADDRESS"
