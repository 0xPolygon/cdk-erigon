#!/bin/bash

# =============================================================================
# OP Withdrawal Finalization Script
# =============================================================================
# This script finalizes a withdrawal from Optimism L2 to Ethereum L1
# 
# Usage: ./15-finalize-withdraw.sh <WITHDRAW_TX_HASH>
# Example: ./15-finalize-withdraw.sh 0x609fb2d1714eb94764bcbc36dd55378dacb5061e5014de3c51c8bc54768f1428
# 
# Prerequisites:
# - Withdrawal must be proven first (use 14-bridge-op.sh)
# - Challenge period must have passed
# - OP services must be running
# =============================================================================

# Strict mode: exit on command failure or undefined variable
set -u
#set -x

# =============================================================================
# Configuration
# =============================================================================
# Check if jq is available for JSON parsing
if ! command -v jq >/dev/null 2>&1; then
    echo "❌ jq is required but not installed. Please install jq to parse JSON config files."
    exit 1
fi

# Check if config file exists
if [ ! -f "config-op/state.json" ]; then
    echo "❌ config-op/state.json not found. Please ensure the config file exists."
    exit 1
fi

# Check if withdrawal transaction hash is provided as argument
if [ $# -eq 0 ]; then
    echo "❌ Usage: $0 <WITHDRAW_TX_HASH>"
    echo "Example: $0 0x609fb2d1714eb94764bcbc36dd55378dacb5061e5014de3c51c8bc54768f1428"
    exit 1
fi

WITHDRAW_TX_HASH="$1"

# Validate the transaction hash format
if [[ ! "$WITHDRAW_TX_HASH" =~ ^0x[a-fA-F0-9]{64}$ ]]; then
    echo "❌ Invalid transaction hash format: $WITHDRAW_TX_HASH"
    echo "Transaction hash should be a 64-character hex string starting with 0x"
    exit 1
fi

# OP Bridge addresses from config-op/state.json
OP_PORTAL_ADDRESS=$(jq -r '.opChainDeployments[0].OptimismPortalProxy' config-op/state.json 2>/dev/null); if [ "$OP_PORTAL_ADDRESS" = "null" ] || [ -z "$OP_PORTAL_ADDRESS" ]; then OP_PORTAL_ADDRESS="0x6004cdd3414e7af711834d654388c85b5721afa3"; fi
ACCOUNT="0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534" 
PRIVATE_KEY="0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2"

# =============================================================================
# RPC Endpoint Configuration
# =============================================================================
L1RPC=http://127.0.0.1:8545
L2RPC=http://127.0.0.1:8123

# =============================================================================
# Finalize Withdrawal on L1
# =============================================================================
echo "🔄 Finalizing OP withdrawal: $WITHDRAW_TX_HASH"

# Load environment variables
source .env

# Check current L1 balance
L1_BALANCE_BEFORE=$(cast balance "$ACCOUNT" --rpc-url "$L1RPC")
echo "L1_BALANCE_BEFORE: $L1_BALANCE_BEFORE"

# Finalize the withdrawal
FINALIZE_OUTPUT=$(docker run --rm \
    --network "$DOCKER_NETWORK" \
    "${OP_STACK_IMAGE_TAG}" \
    /app/op-chain-ops/bin/withdrawal finalize \
        --l1 ${L1_RPC_URL_IN_DOCKER} \
        --l2 ${L2_RPC_URL_IN_DOCKER} \
        --tx $WITHDRAW_TX_HASH \
        --portal-address $OP_PORTAL_ADDRESS \
        --private-key $PRIVATE_KEY 2>&1)



# Check for application failure

# Check for application failure
if echo "$FINALIZE_OUTPUT" | grep -q "Application failed"; then
    echo "❌ Withdrawal finalization failed:"
    echo "$FINALIZE_OUTPUT" | grep "Application failed" -A 5 -B 5
    echo ""
    echo "Common reasons:"
    echo "   - 0x730a1074: Withdrawal already finalized"
    echo "   - 0x332a57f8: Invalid root claim"
    echo "   - 0xd9bc01be: Proof not old enough"
    exit 1
fi

# Extract transaction hash from successful output
FINALIZE_TX_HASH=$(echo "$FINALIZE_OUTPUT" | grep "Finalized withdrawal" | tail -1 | awk '{print $NF}' | sed 's/tx=//' || echo "FINALIZE_PLACEHOLDER")

# Check if we got a valid transaction hash
if [ "$FINALIZE_TX_HASH" = "FINALIZE_PLACEHOLDER" ]; then
    echo "❌ Failed to extract transaction hash from finalization output"
    echo "Full output:"
    echo "$FINALIZE_OUTPUT"
    exit 1
fi

# Wait for finalize transaction to be confirmed
echo "⏳ Waiting for confirmation..."
max_attempts=30  # 5 minutes max wait
attempt=0
success=false

while [ $attempt -lt $max_attempts ]; do
    sleep 2
    receipt=$(cast receipt "$FINALIZE_TX_HASH" --rpc-url "$L1RPC" 2>/dev/null || echo "")
    
    if [ -n "$receipt" ]; then
        success=true
        break
    fi
    
    attempt=$((attempt + 1))
done

if [ "$success" = false ]; then
    echo "❌ Finalization failed: Transaction not confirmed within timeout"
    echo "   Check if challenge period has passed and withdrawal was properly proven"
    exit 1
fi

# Check final L1 balance and calculate changes
L1_BALANCE_AFTER=$(cast balance "$ACCOUNT" --rpc-url "$L1RPC")
L1_BALANCE_CHANGE=$(echo "$L1_BALANCE_AFTER - $L1_BALANCE_BEFORE" | bc)

# Get gas cost
FINALIZE_RECEIPT=$(cast receipt "$FINALIZE_TX_HASH" --rpc-url "$L1RPC" 2>/dev/null || echo "")
GAS_COST=0
if [ -n "$FINALIZE_RECEIPT" ]; then
    GAS_USED=$(echo "$FINALIZE_RECEIPT" | grep "gasUsed" | awk '{print $2}')
    GAS_PRICE=$(echo "$FINALIZE_RECEIPT" | grep "effectiveGasPrice" | awk '{print $2}')
    if [ -n "$GAS_USED" ] && [ -n "$GAS_PRICE" ]; then
        GAS_COST=$(echo "$GAS_USED * $GAS_PRICE" | bc)
    fi
fi

# Calculate actual withdrawal amount
ACTUAL_WITHDRAWAL_AMOUNT=$(echo "$L1_BALANCE_CHANGE + $GAS_COST" | bc)
EXPECTED_WITHDRAWAL="500000000000000000"  # 0.5 ETH

# Display results
echo ""
if [ "$ACTUAL_WITHDRAWAL_AMOUNT" -eq "$EXPECTED_WITHDRAWAL" ]; then
    echo "✅ Withdrawal finalized successfully!"
    echo "   L1 balance: $L1_BALANCE_BEFORE → $L1_BALANCE_AFTER wei (+$L1_BALANCE_CHANGE)"
    echo "   Gas cost: $GAS_COST wei"
    echo "   Net received: $ACTUAL_WITHDRAWAL_AMOUNT wei"
elif [ "$ACTUAL_WITHDRAWAL_AMOUNT" -gt 0 ]; then
    echo "⚠️  Withdrawal finalized partially"
    echo "   L1 balance: $L1_BALANCE_BEFORE → $L1_BALANCE_AFTER wei (+$L1_BALANCE_CHANGE)"
    echo "   Gas cost: $GAS_COST wei"
    echo "   Net received: $ACTUAL_WITHDRAWAL_AMOUNT wei"
    echo "   Expected: $EXPECTED_WITHDRAWAL wei"
else
    echo "❌ Withdrawal finalization failed"
    echo "   L1 balance: $L1_BALANCE_BEFORE → $L1_BALANCE_AFTER wei ($L1_BALANCE_CHANGE)"
    echo "   No assets transferred to L1"
fi
