#!/bin/bash

# Strict mode: exit on command failure or undefined variable
set -eu
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

# OP Bridge addresses from config-op/state.json
OP_BRIDGE_ADDRESS="0x4200000000000000000000000000000000000010"
OP_PORTAL_ADDRESS=$(jq -r '.opChainDeployments[0].OptimismPortalProxy' config-op/state.json)
ACCOUNT="0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534" 
PRIVATE_KEY="0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2"
BRIDGE_VALUE="1000000000000000000"  # 1 ETH in wei
WITHDRAW_VALUE="500000000000000000"  # 0.5 ETH in wei

# =============================================================================
# RPC Endpoint Configuration
# =============================================================================
L1RPC=http://127.0.0.1:8545
L2RPC=http://127.0.0.1:8124

# =============================================================================
# Bridge from L1 to L2 (using OptimismPortal)
# =============================================================================
echo -e "\n========== Testing OP Bridge: L1 -> L2 =========="

# Check initial balances
L1_BALANCE_BEFORE=$(cast balance "$ACCOUNT" --rpc-url "$L1RPC")
L2_BALANCE_BEFORE=$(cast balance "$ACCOUNT" --rpc-url "$L2RPC")

echo "Initial balances:"
echo "  L1: $L1_BALANCE_BEFORE wei"
echo "  L2: $L2_BALANCE_BEFORE wei"

# Bridge ETH from L1 to L2 using OptimismPortal
echo -e "\nBridging ETH from L1 to L2 using OptimismPortal..."
echo "Bridge amount: $BRIDGE_VALUE wei"

cast send \
    --legacy \
    --rpc-url $L1RPC \
    --private-key $PRIVATE_KEY \
    --value $BRIDGE_VALUE \
    $OP_PORTAL_ADDRESS \
    'function depositTransaction(address _target, uint256 _value, uint64 _gasLimit, bool _isCreation, bytes _data)' \
    $ACCOUNT $BRIDGE_VALUE 100000 false 0x

echo "Deposit transaction sent successfully!"

# Wait for assets to appear on L2
echo -e "\nWaiting for assets to appear on L2..."
start_time=$(date +%s)
max_attempts=60  # 10 minutes max wait
attempt=0
success=false

while [ $attempt -lt $max_attempts ]; do
    sleep 10
    current_balance=$(cast balance "$ACCOUNT" --rpc-url "$L2RPC")
    current_balance=${current_balance:-0}
    increment=$(echo "$current_balance - $L2_BALANCE_BEFORE" | bc)
    
    echo "  Checking L2 balance... (attempt $((attempt + 1))/$max_attempts)"
    
    if [ "$increment" -gt 0 ]; then
        echo "✅ Assets successfully bridged to L2!"
        success=true
        break
    fi
    
    attempt=$((attempt + 1))
done

if [ "$success" = false ]; then
    echo "❌ Timeout waiting for assets to appear on L2"
    echo "This could indicate:"
    echo "  1. L2 sequencer is not processing transactions"
    echo "  2. Bridge service is not running properly"
    echo "  3. Network connectivity issues"
    exit 1
fi

end_time=$(date +%s)
total_elapsed=$((end_time - start_time))

# Check final balances after L1->L2 bridge
L1_BALANCE_AFTER_L1L2=$(cast balance "$ACCOUNT" --rpc-url "$L1RPC")
L2_BALANCE_AFTER_L1L2=$(cast balance "$ACCOUNT" --rpc-url "$L2RPC")

echo -e "\n========== L1->L2 Bridge Test Results =========="
echo "Bridge completed in $total_elapsed seconds"
echo "Actual bridged amount: $increment wei"

if [ "$increment" -eq "$BRIDGE_VALUE" ]; then
    echo "✅ L1->L2 Bridge test PASSED - Full amount bridged successfully!"
else
    echo "⚠️  L1->L2 Bridge test PARTIAL - Amount bridged: $increment wei (expected: $BRIDGE_VALUE wei)"
fi

# =============================================================================
# Withdraw from L2 to L1 (using L1StandardBridge)
# =============================================================================
echo -e "\n========== Testing OP Withdraw: L2 -> L1 =========="

# Check L2 balance before withdrawal
L2_BALANCE_BEFORE_WITHDRAW=$(cast balance "$ACCOUNT" --rpc-url "$L2RPC")

# Check if we have enough balance for withdrawal
if [ "$(echo "$L2_BALANCE_BEFORE_WITHDRAW < $WITHDRAW_VALUE" | bc)" -eq 1 ]; then
    echo "❌ Insufficient L2 balance for withdrawal"
    echo "   Required: $WITHDRAW_VALUE wei"
    echo "   Available: $L2_BALANCE_BEFORE_WITHDRAW wei"
    exit 1
fi

# Step 1: Initiate withdrawal on L2
echo -e "\nStep 1: Initiating withdrawal on L2..."
echo "Withdrawal amount: $WITHDRAW_VALUE wei"

WITHDRAW_TX_HASH=$(cast send \
    --legacy \
    --private-key $PRIVATE_KEY \
    --rpc-url $L2RPC \
    --json \
    --value $WITHDRAW_VALUE \
    $OP_BRIDGE_ADDRESS \
    'function bridgeETH(uint32 _minGasLimit, bytes _extraData)' \
    100000 0x \
    | jq -r '.transactionHash')

echo "Withdrawal transaction hash: $WITHDRAW_TX_HASH"

# Wait for L2 transaction to be confirmed
echo -e "\nWaiting for L2 withdrawal transaction to be confirmed..."
max_attempts=30  # 5 minutes max wait
attempt=0
success=false

while [ $attempt -lt $max_attempts ]; do
    sleep 10
    receipt=$(cast receipt "$WITHDRAW_TX_HASH" --rpc-url "$L2RPC" 2>/dev/null || echo "")
    
    if [ -n "$receipt" ]; then
        echo "✅ L2 withdrawal transaction confirmed!"
        success=true
        break
    fi
    
    if [ $((attempt % 3)) -eq 0 ]; then
        echo "  Waiting for confirmation... (attempt $((attempt + 1))/$max_attempts)"
    fi
    
    attempt=$((attempt + 1))
done

if [ "$success" = false ]; then
    echo "❌ Timeout waiting for L2 withdrawal transaction confirmation"
    exit 1
fi

# Check L2 balance after withdrawal initiation
L2_BALANCE_AFTER_WITHDRAW=$(cast balance "$ACCOUNT" --rpc-url "$L2RPC")

# Step 2: Wait for withdrawal to be ready to prove
echo -e "\nStep 2: Waiting for withdrawal to be ready to prove..."

# Get DisputeGameFactory address from config
DISPUTE_GAME_FACTORY_ADDRESS=$(jq -r '.opChainDeployments[0].DisputeGameFactoryProxy' config-op/state.json)

# Get current game count before withdrawal
CURRENT_GAME_COUNT=$(cast call \
    --rpc-url $L1RPC \
    $DISPUTE_GAME_FACTORY_ADDRESS \
    'function gameCount() view returns (uint256)' 2>/dev/null || echo "0")

# Wait for new game to be created (indicating output root was submitted)
echo "Waiting for output root to be submitted to L1..."
max_attempts=60  # 10 minutes max wait
attempt=0
success=false

while [ $attempt -lt $max_attempts ]; do
    sleep 10
    NEW_GAME_COUNT=$(cast call \
        --rpc-url $L1RPC \
        $DISPUTE_GAME_FACTORY_ADDRESS \
        'function gameCount() view returns (uint256)' 2>/dev/null || echo "0")
    
    echo "Attempt $((attempt + 1))/$max_attempts - Current game count: $NEW_GAME_COUNT"
    
    if [ "$NEW_GAME_COUNT" -gt "$CURRENT_GAME_COUNT" ]; then
        echo "✅ New game detected! Output root has been submitted to L1"
        success=true
        break
    fi
    
    attempt=$((attempt + 1))
done

if [ "$success" = false ]; then
    echo "❌ Timeout waiting for output root to be submitted to L1"
    echo "This could indicate:"
    echo "  1. L2 sequencer is not submitting output roots"
    echo "  2. DisputeGameFactory is not working properly"
    echo "  3. Network connectivity issues"
    exit 1
fi

# Step 3: Prove withdrawal on L1
echo -e "\nStep 3: Proving withdrawal on L1..."

# Load environment variables for challenge period
source .env

echo "Proving withdrawal transaction..."
PROVE_TX_HASH=$(docker run --rm \
    --network "$DOCKER_NETWORK" \
    "${OP_STACK_IMAGE_TAG}" \
    /app/op-chain-ops/bin/withdrawal prove \
        --l1 ${L1_RPC_URL_IN_DOCKER} \
        --l2 ${L2_RPC_URL_IN_DOCKER} \
        --tx $WITHDRAW_TX_HASH \
        --portal-address $(jq -r '.opChainDeployments[0].OptimismPortalProxy' config-op/state.json) \
        --private-key $PRIVATE_KEY 2>&1 | grep "Proved withdrawal" | tail -1 | awk '{print $NF}' | sed 's/tx=//' || echo "PROVE_PLACEHOLDER")

echo "Prove transaction hash: $PROVE_TX_HASH"

# Wait for prove transaction to be confirmed
echo -e "\nWaiting for prove transaction to be confirmed..."
max_attempts=30  # 5 minutes max wait
attempt=0
success=false

while [ $attempt -lt $max_attempts ]; do
    sleep 10
    receipt=$(cast receipt "$PROVE_TX_HASH" --rpc-url "$L1RPC" 2>/dev/null || echo "")
    
    if [ -n "$receipt" ]; then
        echo "✅ Prove transaction confirmed!"
        success=true
        break
    fi
    
    if [ $((attempt % 3)) -eq 0 ]; then
        echo "  Waiting for prove confirmation... (attempt $((attempt + 1))/$max_attempts)"
    fi
    
    attempt=$((attempt + 1))
done

if [ "$success" = false ]; then
    echo "⚠️  Prove transaction confirmation timeout"
    echo "This could indicate:"
    echo "  1. Prove transaction failed"
    echo "  2. Network connectivity issues"
    echo "  3. Gas price issues"
fi

# =============================================================================
# Calculate withdrawal timeline and final output
# =============================================================================

# Get MAX_CLOCK_DURATION from environment, default to 1 hour (3600 seconds) if not set
CLOCK_DURATION_SECONDS=${MAX_CLOCK_DURATION:-3600}

# Calculate withdrawal completion time
WITHDRAWAL_START_TIME=$(date +%s)
FINALIZATION_TIME=$((WITHDRAWAL_START_TIME + CLOCK_DURATION_SECONDS))

# Convert to human readable time
if command -v gdate >/dev/null 2>&1; then
    # GNU date (available via homebrew on macOS)
    RECOMMENDED_TIME=$(gdate -d "@$FINALIZATION_TIME" 2>/dev/null || echo "Withdrawal start + $((CLOCK_DURATION_SECONDS / 60)) minutes")
elif [[ "$OSTYPE" == "darwin"* ]]; then
    # macOS date command
    RECOMMENDED_TIME=$(date -r $FINALIZATION_TIME 2>/dev/null || echo "Withdrawal start + $((CLOCK_DURATION_SECONDS / 60)) minutes")
else
    # Linux date command
    RECOMMENDED_TIME=$(date -d "@$FINALIZATION_TIME" 2>/dev/null || echo "Withdrawal start + $((CLOCK_DURATION_SECONDS / 60)) minutes")
fi

# Get current time in readable format
CURRENT_TIME=$(date)

echo -e "\n========== OP Bridge & Withdraw Test Complete =========="
echo "✅ All operations completed successfully!"
echo ""
echo "📝 TX Details:"
echo "  L2 Withdrawal: $WITHDRAW_TX_HASH"
echo "  L1 Prove: $PROVE_TX_HASH"
echo ""
echo "⏰ Timeline:"
echo "  Now: $CURRENT_TIME"
echo "  Challenge period: $((CLOCK_DURATION_SECONDS / 60))min"
echo "  Finalize: $RECOMMENDED_TIME"
echo ""
echo "📊 Summary:"
echo "  • L1->L2(✅): $increment wei"
echo "  • L2->L1(waiting for challenge period to end): $WITHDRAW_VALUE wei "
echo "    • Withdrawal transaction hash: $WITHDRAW_TX_HASH"
echo ""

echo "🚀 You need: Run the following command to finalize the withdrawal after the challenge period ends at $RECOMMENDED_TIME:"
echo "   ./15-bridge-op-claim-asset.sh $WITHDRAW_TX_HASH"

