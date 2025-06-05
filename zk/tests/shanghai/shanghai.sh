#!/bin/bash

RPC_URL=$1
PRIVATE_KEY="$2"
RUNDIR=$(dirname "$0")
CONTRACTS_DIR="$RUNDIR/../../debug_tools/test-contracts/contracts"

if [[ -z "$RPC_URL" || -z "$PRIVATE_KEY" ]]; then
    echo "Usage: $0 <rpc-url> <private-key>"
    exit 1
fi

. "$RUNDIR/../utils.sh"

# ------------------------------------
# EIP-3651 “Warm COINBASE” Test
# ------------------------------------
testEIP3651() {
    local RPC_URL="$1"
    local CONTRACT_ADDR
    local SEND_JSON
    local GAS_USED_HEX
    local GAS_USED_DEC

    # 1) Deploy the CoinbaseBalance contract
    echo "Deploying CoinbaseBalance contract…"
    CONTRACT_ADDR=$(forge create \
        "$CONTRACTS_DIR/CoinbaseBalance.sol:CoinbaseBalance" \
        --evm-version shanghai \
        --broadcast \
        --rpc-url "$RPC_URL" \
        --private-key "$PRIVATE_KEY" \
        --json | jq -r '.deployedTo')

    if [[ -z "$CONTRACT_ADDR" ]]; then
        echo "Error: Failed to deploy CoinbaseBalance contract" >&2
        return 1
    fi

    echo "Deployed at: $CONTRACT_ADDR"
    echo "Calling getCoinbaseBalance() on $CONTRACT_ADDR"

    # 2) Send a transaction that reads block.coinbase.balance
    SEND_JSON=$(cast send "$CONTRACT_ADDR" \
        "getCoinbaseBalance()(uint256)" \
        --rpc-url "$RPC_URL" \
        --private-key "$PRIVATE_KEY" \
        --gas-limit 100000 \
        --json)

    GAS_USED_HEX=$(echo "$SEND_JSON" | jq -r '.gasUsed')
    echo "Gas used by getCoinbaseBalance(): $GAS_USED_HEX"

    # 3) Convert to decimal
    GAS_USED_DEC=$((16#${GAS_USED_HEX#0x}))
    echo "Gas used (decimal): $GAS_USED_DEC"

    # 4) Compare against expected thresholds
    #    - If EIP-3651 is active, BALANCE(block.coinbase) is “warm” → cost ~21479
    #    - If not active, BALANCE(block.coinbase) is “cold” → cost ~ 23984
    if (( GAS_USED_DEC < 22000 )); then
        echo "EIP-3651 is active (warm COINBASE behavior detected)."
        return 0
    else
        echo "EIP-3651 not active (cold COINBASE cost observed)." >&2
        return 1
    fi

    echo "EIP-3651 test completed successfully."
}

# ------------------------------------
# EIP-3855 “PUSH0” Opcode Test
# ------------------------------------
testEIP3855() {
    local RPC_URL="$1"
    local CONTRACT_ADDR
    local RAW_RETURN
    local DECODED

    echo "Deploying Push0 contract…"
    local BYTECODE="0x6009600c60003960096000f35f60005260206000f3" #5f is PUSH0
    # 1) Deploy Push0
    CONTRACT_ADDR=$(cast send \
        --rpc-url "$RPC_URL" \
        --private-key "$PRIVATE_KEY" \
        --create "$BYTECODE" \
        --json | jq -r '.contractAddress')

    if [[ -z "$CONTRACT_ADDR" ]]; then
        echo "Error: Failed to deploy Push0 contract" >&2
        return 1
    fi

    echo "Deployed at: $CONTRACT_ADDR"
    echo "Getting contract code from $CONTRACT_ADDR"

    local CODE=$(cast code "$CONTRACT_ADDR" --rpc-url "$RPC_URL")
    if [[ -z "$CODE" ]]; then
        echo "Error: Failed to retrieve contract code" >&2
        return 1
    fi

    # Code must start with 0x5f (PUSH0 opcode)
    if [[ "${CODE:0:4}" != "0x5f" ]]; then
        echo "Error: Contract code does not start with PUSH0 opcode (0x5f)" >&2
        return 1
    fi

    echo "Contract code starts with PUSH0 opcode (0x5f). Proceeding with test."

    # 2) Call getZero() via eth_call to exercise PUSH0
    RAW_RETURN=$(cast call "$CONTRACT_ADDR" \
        0x \
        --rpc-url "$RPC_URL" 2>&1) || {
        echo "Reverted: EIP-3855 (PUSH0) not supported on this node" >&2
        return 1
    }

    echo "RAW return data: $RAW_RETURN"

    # 3) Decode the returned uint256
    echo "Decoding return data…"
    DECODED=$(cast abi-decode 'foo()(uint256)' "$RAW_RETURN")
    echo "Decoded uint256: $DECODED"

    # 4) Check that it equals 0x0
    if [[ "$DECODED" == "0" ]]; then
        echo "EIP-3855 (PUSH0) is active: returned 0."
        return 0
    else
        echo "Unexpected return: $DECODED (expected all zeros)" >&2
        return 1
    fi

    echo "EIP-3855 test completed successfully."
}

echo "=============== Running Shaghai tests ==============="

run testEIP3651 "$RPC_URL"
run testEIP3855 "$RPC_URL"

echo "=============== Shanghai tests completed ==============="
