#!/bin/bash

RPC_URL=$1
PRIVATE_KEY="$2"
RUNDIR=$(dirname "$0")

if [[ -z "$RPC_URL" || -z "$PRIVATE_KEY" ]]; then
    echo "Usage: $0 <rpc-url> <private-key>"
    exit 1
fi

. "$RUNDIR/../utils.sh"

# ------------------------------------
# EIP 1559 Transaction Test
# ------------------------------------
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

# ------------------------------------
# EIP 3198 Base Fee Test
# ------------------------------------
testBaseFeeEIP3198() {
    local RPC_URL="$1"
    local CONTRACT_ADDR

    CONTRACT_ADDR=$(forge create \
        $CONTRACTS_DIR/BaseFee.sol:Basefee \
        --broadcast \
        --rpc-url "$RPC_URL" \
        --private-key "$PRIVATE_KEY" \
        --json | jq -r '.deployedTo')

    if [ -z "$CONTRACT_ADDR" ]; then
        echo "Failed to deploy Basefee contract" >&2
        return 1
    fi

    echo "Deployed at: $CONTRACT_ADDR"
    echo "Calling getBasefee() on contract at $CONTRACT_ADDR"

    BASEFEE=$(cast call \
        "$CONTRACT_ADDR" \
        "getBasefee()(uint256)" \
        --rpc-url "$RPC_URL")

    echo "Basefee: $BASEFEE"

    # Check that no error occurred
    # "call" will not return real basefee, but returns 0
    # the real basefee is used in a tx only
    if [[ "$BASEFEE" != "0" ]]; then
        echo "Error: Expected basefee to be 0, got $BASEFEE"
        return 1
    fi

    echo "Basefee test passed"
}

# ------------------------------------
# EIP 3541 Reject new contract code starting with the 0xEF byte
# ------------------------------------
testEIP3541() {
    local RPC_URL="$1"
    local EF_INITCODE="0x60ef60005360016000f3"  # runtime = 0xef (should revert)
    local FE_INITCODE="0x60fe60005360016000f3"  # runtime = 0xfe (should succeed)
    local STATUS

    echo "Deploy runtime 0xef (must fail under EIP-3541)"

    STATUS=$(cast send \
        --rpc-url "$RPC_URL" \
        --private-key "$PRIVATE_KEY" \
        --gas-limit 100000 \
        --create "$EF_INITCODE" \
        --json | jq -r .status)

    if [[ $STATUS == "0x1" ]]; then
        echo "Error: Contract with runtime 0xef was deployed, but it should have been rejected." >&2
        return 1
    fi

    echo "Correctly reverted. EIP-3541 prevented 0xef runtime."

    echo "Deploy runtime 0xfe (must succeed)"

    STATUS=$(cast send \
        --rpc-url "$RPC_URL" \
        --private-key "$PRIVATE_KEY" \
        --gas-limit 100000 \
        --create "$FE_INITCODE" \
        --json | jq -r .status)

    if [[ $STATUS != "0x1" ]]; then
        echo "Error: Contract with runtime 0xfe failed to deploy." >&2
        return 1
    fi

    echo "EIP-3541 test passed"
}

run testTxEIP1559 "$RPC_URL"
run testBaseFeeEIP3198 "$RPC_URL"
run testEIP3541 "$RPC_URL"
