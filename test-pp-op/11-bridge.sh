#!/bin/bash

# Strict mode: exit on command failure or undefined variable
set -eu
set -x

# =============================================================================
# Configuration
# =============================================================================
BRIDGE_ADDRESS="0xb2fAc1CE54bb9BF77A7FE106fA69F81453b72851"
ACCOUNT="0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534" 
PRIVATE_KEY="0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2"
BRIDGE_VALUE_BIG="1000000000000000000"  # 1 ETH in wei
BRIDGE_VALUE_SMALL="100000000000000000"  # 0.1 ETH in wei

L1_ETH_ADDRESS="0x0000000000000000000000000000000000000000"
L2_WETH="0xd80e5a44dc9628fae9b432eac67873238504ea29"

# =============================================================================
# RPC Endpoint Configuration
# =============================================================================
L1RPC=http://127.0.0.1:8545
L2RPC=http://127.0.0.1:8123
BRIDGE_SERVICE1=http://127.0.0.1:8080

# Get Global Exit Root Manager address
GER_MGR=$(cast call "$BRIDGE_ADDRESS" "globalExitRootManager()(address)" --rpc-url "$L1RPC")
L2GER_MGR1=$(cast call "$BRIDGE_ADDRESS" "globalExitRootManager()(address)" --rpc-url "$L2RPC")
echo "GlobalExitRootManager Address:"
echo "  L1   ==> $GER_MGR"
echo "  L2   ==> $L2GER_MGR1"

# Get initial Global Exit Root
GER=$(cast call "$GER_MGR" "getLastGlobalExitRoot()" --rpc-url "$L1RPC")
echo "Initial GER on L1: $GER"

# =============================================================================
# Bridge from L1 to L2
# =============================================================================

# Check balance before bridging
L2_BALANCE_BEFORE_BRIDGE=$(cast call "$L2_WETH" "balanceOf(address)(uint256)" "$ACCOUNT" --rpc-url "$L2RPC" | awk '{print $1}')

cast send \
    --legacy \
    --rpc-url $L1RPC \
    --private-key $PRIVATE_KEY \
    --value $BRIDGE_VALUE_BIG \
    $BRIDGE_ADDRESS \
    'function bridgeAsset(uint32 destinationNetwork, address destinationAddress, uint256 amount, address token, bool forceUpdateGlobalExitRoot, bytes permitData) returns()' \
    1 $ACCOUNT $BRIDGE_VALUE_BIG $L1_ETH_ADDRESS true 0x

# Wait for GER update on L1
echo "Waiting for GER to be updated on L1..."
while true; do
    GER_NEW=$(cast call "$GER_MGR" "getLastGlobalExitRoot()" --rpc-url "$L1RPC")
    if [ "$GER_NEW" != "$GER" ]; then
        GER=$GER_NEW
        echo "GER updated to $GER on L1"
        break
    fi
    sleep 10
done

# Wait for GER to sync to L2
echo "Waiting for GER to sync to L2..."
start_time=$(date +%s)
while true; do
    timestamp=$(cast call "$L2GER_MGR1" "globalExitRootMap(bytes32)(uint256)" "$GER" --rpc-url "$L2RPC")
    if [ "$timestamp" != "0" ]; then
        break
    fi
    sleep 10
done
end_time=$(date +%s)
total_elapsed=$((end_time - start_time))
echo "GER synced to L2, took $total_elapsed seconds"

# Wait for assets to be claimed automatically by sponsor
echo "Waiting for assets to be claimed by sponsor..."
start_time=$(date +%s)
while true; do
    sleep 60
    balance=$(cast call "$L2_WETH" "balanceOf(address)(uint256)" "$ACCOUNT" --rpc-url "$L2RPC" | awk '{print $1}')
    balance=${balance:-0}
    increment=$(echo "$balance - $L2_BALANCE_BEFORE_BRIDGE" | bc)
    echo "Current balance on L2: $balance (increment: $increment)"
    if [ "$increment" -gt 0 ]; then
        echo "Assets successfully claimed on L2"
        break
    fi
    
    cast send \
    --legacy \
    --rpc-url $L1RPC \
    --private-key $PRIVATE_KEY \
    --value $BRIDGE_VALUE_BIG \
    $BRIDGE_ADDRESS \
    'function bridgeAsset(uint32 destinationNetwork, address destinationAddress, uint256 amount, address token, bool forceUpdateGlobalExitRoot, bytes permitData) returns()' \
    1 $ACCOUNT $BRIDGE_VALUE_BIG $L1_ETH_ADDRESS true 0x

done
end_time=$(date +%s)
total_elapsed=$((end_time - start_time))
echo "Balance on L2 is $balance, claim took $total_elapsed seconds"

# Check balance after bridging
L2_BALANCE_AFTER_BRIDGE=$(cast balance "$ACCOUNT" --rpc-url "$L2RPC")
echo "Balance on L2(ETH):"
echo "  Before bridge = $L2_BALANCE_BEFORE_BRIDGE"
echo "  After bridge  = $L2_BALANCE_AFTER_BRIDGE"

# =============================================================================
# Bridge from L2 to L1
# =============================================================================
echo -e "\n========== Bridging Assets: L2 -> L1 BRIDGE_VALUE_SMALL =========="

# Approve token
echo "Approving token..."
cast send \
  --legacy \
  --rpc-url $L2RPC \
  --private-key $PRIVATE_KEY \
  "$L2_WETH" \
  "function approve(address spender, uint256 amount) returns (bool)" \
  $BRIDGE_ADDRESS \
  $BRIDGE_VALUE_BIG

# Initiate bridging transaction
TX_HASH=$(cast send \
    --legacy \
    --private-key $PRIVATE_KEY \
    --rpc-url $L2RPC \
    --json \
    $BRIDGE_ADDRESS \
    'function bridgeAsset(uint32 destinationNetwork, address destinationAddress, uint256 amount, address token, bool forceUpdateGlobalExitRoot, bytes permitData) returns()' \
    0 $ACCOUNT $BRIDGE_VALUE_SMALL $L2_WETH true "0x" \
    | jq -r ' .transactionHash')
echo "Bridge transaction hash: $TX_HASH"

# Wait for GER update on L1
echo "Waiting for GER to be updated on L1..."
start_time=$(date +%s)
while true; do
    GER_NEW=$(cast call "$GER_MGR" "getLastGlobalExitRoot()" --rpc-url "$L1RPC")
    if [ "$GER_NEW" != "$GER" ]; then
        GER=$GER_NEW
        break
    fi
    sleep 10
done
end_time=$(date +%s)
total_elapsed=$((end_time - start_time))
echo "GER updated to $GER on L1, took $total_elapsed seconds"

sleep 30
echo "Getting deposit count and network ID from bridge service..."
result=$(curl -s "$BRIDGE_SERVICE1/bridges/$ACCOUNT?limit=20000&offset=0" | \
   jq -r '.deposits[] | select(.ready_for_claim == true and .claim_tx_hash == "" and .tx_hash=="'$TX_HASH'")')                                                                  
DEPOSIT_CNT=$(echo "$result" | jq -r '.deposit_cnt')
NETWORK_ID=$(echo "$result" | jq -r '.network_id')
GLOBAL_INDEX=$(echo "$result" | jq -r '.global_index')
ORINGIN_NETWORK=$(echo "$result" | jq -r '.orig_net')
ORINGIN_ADDRESS=$(echo "$result" | jq -r '.orig_addr')
DESTINATION_NETWORK=$(echo "$result" | jq -r '.dest_net')
IN_AMOUNT=$(echo "$result" | jq -r '.amount')
METADATA=$(echo "$result" | jq -r '.metadata')
echo "Deposit Count: $DEPOSIT_CNT"
echo "Network ID: $NETWORK_ID"
echo "Global Index: $GLOBAL_INDEX"
echo "Origin Network: $ORINGIN_NETWORK"
echo "Origin Address: $ORINGIN_ADDRESS"
echo "Destination Network: $DESTINATION_NETWORK"
echo "In Amount: $IN_AMOUNT"
echo "Metadata: $METADATA"

proof=$(curl -s "$BRIDGE_SERVICE1/merkle-proof?deposit_cnt=$DEPOSIT_CNT&net_id=$NETWORK_ID" | jq -r '.')
MERKLE_PROOF=$(echo "$proof" | jq -r -c '.proof | .merkle_proof' | tr -d '"')
ROLLUP_MERKLE_PROOF=$(echo "$proof" | jq -r -c '.proof | .rollup_merkle_proof' | tr -d '"')
MER=$(echo "$proof" | jq -r '.proof | .main_exit_root')
RER=$(echo "$proof" | jq -r '.proof | .rollup_exit_root')
echo "Merkle Proof: $MERKLE_PROOF"
echo "Rollup Merkle Proof: $ROLLUP_MERKLE_PROOF"
echo "Main Exit Root: $MER"
echo "Rollup Exit Root: $RER"

# Claim assets on L1
echo "Claiming assets on L1..."
L1_BALANCE_BEFORE_CLAIM=$(cast balance "$ACCOUNT" --rpc-url "$L1RPC")
cmd="cast send --legacy --rpc-url $L1RPC --private-key $PRIVATE_KEY $BRIDGE_ADDRESS 'claimAsset(bytes32[32],bytes32[32],uint256,bytes32,bytes32,uint32,address,uint32,address,uint256,bytes)' $MERKLE_PROOF $ROLLUP_MERKLE_PROOF $GLOBAL_INDEX $MER $RER $ORINGIN_NETWORK $ORINGIN_ADDRESS $DESTINATION_NETWORK $ACCOUNT $IN_AMOUNT $METADATA"

echo "Warning!!!!!!!!!! Claim asset on L1, Executing: $cmd"  
eval "$cmd"

L1_BALANCE_AFTER_CLAIM=$(cast balance "$ACCOUNT" --rpc-url "$L1RPC")
echo "Balance on L1:"
echo "  Before claiming: $L1_BALANCE_BEFORE_CLAIM"
echo "  After claiming: $L1_BALANCE_AFTER_CLAIM"