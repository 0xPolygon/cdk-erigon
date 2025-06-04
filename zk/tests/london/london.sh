#!/bin/bash

RPC_URL=$1
PRIVATE_KEY="$2"
RUNDIR=$(dirname "$0")

. "$RUNDIR/../utils.sh"

testTxEIP1559() {
    local RPC_URL="$1"
    local VALUE="0.01ether"
    local TO="0x000000000000000000000000000000000000dead"  # example recipient

    # Gas settings for EIP-1559
    local MAX_FEE="50gwei"
    local PRIORITY_FEE="2gwei"
    local GAS_LIMIT="21000"

    # Attempt to send the transaction
    local OUTPUT
    OUTPUT=$(cast send "$TO" \
        --value "$VALUE" \
        --gas-price "$MAX_FEE" \
        --priority-gas-price "$PRIORITY_FEE" \
        --gas-limit "$GAS_LIMIT" \
        --private-key "$PRIVATE_KEY" \
        --rpc-url "$RPC_URL" --json | jq -r '.status')

    if [[ "$OUTPUT" != "0x1" ]]; then
        echo "Error: transaction failed to send"
        echo "$OUTPUT"
        return 1
    fi

    echo "Transaction successfully sent"
    return 0
}

run testTxEIP1559 "$RPC_URL"
