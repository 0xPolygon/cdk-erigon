#!/bin/bash

# Strict mode: exit on command failure or undefined variable
set -eu

# =============================================================================
# Configuration
# =============================================================================
BRIDGE_ADDRESS="0x4B24266C13AFEf2bb60e2C69A4C08A482d81e3CA"
# New account for testing
ACCOUNT="0xc5D2B4961083a56263E1FD06210c7c1cBAd1017B" 
PRIVATE_KEY="0x62d4ad25c2d91138b2106fa0223da5ebc24be4648c981cf4b02931921d2722d0"
BRIDGE_L2_1_VALUE="1000000000000000000"  # 1 ETH (wei)
BRIDGE_L2_2_VALUE="100000000000000000"  # 0.1 ETH (wei)


POL1_DEPLOYER_ADDRESS="0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
POL1_DEPLOYER_PRIVATE_KEY="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
POL1_ADDRESS="0x5FbDB2315678afecb367f032d93F642f64180aa3"
POL1_ETH_ADDRESS="0x0000000000000000000000000000000000000000"
POL1_METADATA="0x000000000000000000000000000000000000000000000000000000000000006000000000000000000000000000000000000000000000000000000000000000a000000000000000000000000000000000000000000000000000000000000000120000000000000000000000000000000000000000000000000000000000000009506f6c20546f6b656e00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000003504f4c0000000000000000000000000000000000000000000000000000000000"

POL2_DEPLOYER_PRIVATE_KEY="0xbcdf20249abf0ed6d944c0288fad489e33f66b3960d9e6229c1cd214ed3bbe31"
L2_2_WRAPPED_L2_1_TOKEN_ADDRESS="0xd46b2dc528723bcd4e4d3e325f33a0ea6b6bba4b"

# =============================================================================
# RPC Endpoint Configuration
# =============================================================================
L1RPC=http://127.0.0.1:8545
L2RPC1=http://127.0.0.1:8123
L2RPC2=http://127.0.0.1:8127
BRIDGE_SERVICE1=http://127.0.0.1:8080
BRIDGE_SERVICE2=http://127.0.0.1:8081

# Get Global Exit Root Manager address
GER_MGR=$(cast call "$BRIDGE_ADDRESS" "globalExitRootManager()(address)" --rpc-url "$L1RPC")
L2GER_MGR1=$(cast call "$BRIDGE_ADDRESS" "globalExitRootManager()(address)" --rpc-url "$L2RPC1")
L2GER_MGR2=$(cast call "$BRIDGE_ADDRESS" "globalExitRootManager()(address)" --rpc-url "$L2RPC2")
echo "GlobalExitRootManager Address:"
echo "  L1   ==> $GER_MGR"
echo "  L2_1 ==> $L2GER_MGR1"
echo "  L2_2 ==> $L2GER_MGR2"

# Get initial Global Exit Root
GER=$(cast call "$GER_MGR" "getLastGlobalExitRoot()" --rpc-url "$L1RPC")
echo "Initial GER on L1: $GER"

# =============================================================================
# Bridge from L1 to L2_1
# =============================================================================

# Mint POL on L1 and approve bridge
echo -e "\n========== Minting and Bridge POL from L1 to L2_1 =========="
cast send \
    --legacy \
    --rpc-url $L1RPC \
    --private-key $POL1_DEPLOYER_PRIVATE_KEY \
    $POL1_ADDRESS \
    'mint(address,uint256)' \
    $ACCOUNT $BRIDGE_L2_1_VALUE
cast send \
    --legacy \
    --rpc-url $L1RPC \
    --private-key $POL1_DEPLOYER_PRIVATE_KEY \
    $POL1_ADDRESS \
    'approve(address,uint256)' \
    $BRIDGE_ADDRESS $BRIDGE_L2_1_VALUE

# Check balance before bridging
L2_1_BALANCE_BEFORE_BRIDGE=$(cast balance "$ACCOUNT" --rpc-url "$L2RPC1")

cast send \
    --legacy \
    --rpc-url $L1RPC \
    --private-key $POL1_DEPLOYER_PRIVATE_KEY \
    $BRIDGE_ADDRESS \
    'function bridgeAsset(uint32 destinationNetwork, address destinationAddress, uint256 amount, address token, bool forceUpdateGlobalExitRoot, bytes permitData) returns()' \
    1 $ACCOUNT $BRIDGE_L2_1_VALUE $POL1_ADDRESS true 0x

# Wait for GER update on L1
echo "Waiting for GER to be updated on L1..."
while true; do
    GER_NEW=$(cast call "$GER_MGR" "getLastGlobalExitRoot()" --rpc-url "$L1RPC")
    if [ "$GER_NEW" != "$GER" ]; then
        GER=$GER_NEW
        echo "GER updated to $GER on L1"
        break
    fi
    sleep 1
done

# Wait for GER to sync to L2
echo "Waiting for GER to sync to L2_1..."
start_time=$(date +%s)
while true; do
    timestamp=$(cast call "$L2GER_MGR1" "globalExitRootMap(bytes32)(uint256)" "$GER" --rpc-url "$L2RPC1")
    if [ "$timestamp" != "0" ]; then
        break
    fi
    sleep 1
done
end_time=$(date +%s)
total_elapsed=$((end_time - start_time))
echo "GER synced to L2_1, took $total_elapsed seconds"

# Wait for assets to be claimed automatically by sponsor
echo "Waiting for assets to be claimed by sponsor..."
start_time=$(date +%s)
while true; do
    balance=$(cast balance "$ACCOUNT" --rpc-url "$L2RPC1" | xargs cast --to-dec)
    increment=$(echo "$balance - $L2_1_BALANCE_BEFORE_BRIDGE" | bc)
    if [ "$increment" -eq $BRIDGE_L2_1_VALUE ]; then
        break
    fi
    sleep 1
done
end_time=$(date +%s)
total_elapsed=$((end_time - start_time))
echo "Balance on L2_1 is $balance, claim took $total_elapsed seconds"

# Check balance after bridging
L2_1_BALANCE_AFTER_BRIDGE=$(cast balance "$ACCOUNT" --rpc-url "$L2RPC1")
echo "Balance on L2_1(ETH):"
echo "  Before bridge = $L2_1_BALANCE_BEFORE_BRIDGE"
echo "  After bridge  = $L2_1_BALANCE_AFTER_BRIDGE"

# =============================================================================
# Bridge from L2_1 to L2_2
# =============================================================================
echo -e "\n========== Bridging Assets: L2_1 -> L2_2 =========="

# Initiate bridging transaction
# Assume metadata is empty, destinationAddress is ACCOUNT
TX_HASH=$(cast send \
    --legacy \
    --private-key $PRIVATE_KEY \
    --rpc-url $L2RPC1 \
    --value $BRIDGE_L2_2_VALUE \
    --json \
    $BRIDGE_ADDRESS \
    'function bridgeAsset(uint32 destinationNetwork, address destinationAddress, uint256 amount, address token, bool forceUpdateGlobalExitRoot, bytes permitData) returns()' \
    2 $ACCOUNT $BRIDGE_L2_2_VALUE $POL1_ETH_ADDRESS true "0x" \
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
    sleep 1
done
end_time=$(date +%s)
total_elapsed=$((end_time - start_time))
echo "GER updated to $GER on L1, took $total_elapsed seconds"

# Wait for GER to sync to L2
echo "Waiting for GER to sync to L2_2..."
start_time=$(date +%s)
while true; do
    timestamp=$(cast call "$L2GER_MGR2" "globalExitRootMap(bytes32)(uint256)" "$GER" --rpc-url "$L2RPC2")
    if [ "$timestamp" != "0" ]; then
        break
    fi
    sleep 1
done
end_time=$(date +%s)
total_elapsed=$((end_time - start_time))
echo "GER synced to L2_2, took $total_elapsed seconds"

echo "Getting deposit count and network ID from bridge service..."
result=$(curl -s "$BRIDGE_SERVICE1/bridges/$ACCOUNT?limit=100&offset=0" | \
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

# Claim assets on L2_2
# Assume merkle proof is obtained via bridge service
echo "Claiming assets on L2_2..."
L2_2_BALANCE_BEFORE_CLAIM=$(cast call $L2_2_WRAPPED_L2_1_TOKEN_ADDRESS "balanceOf(address)" "$ACCOUNT" --rpc-url "$L2RPC2")
cast send \
    --legacy \
    --rpc-url $L2RPC2 \
    --private-key $POL2_DEPLOYER_PRIVATE_KEY \
    $BRIDGE_ADDRESS \
    'claimAsset(bytes32[32],bytes32[32],uint256,bytes32,bytes32,uint32,address,uint32,address,uint256,bytes)' \
    $MERKLE_PROOF \
    $ROLLUP_MERKLE_PROOF \
    $GLOBAL_INDEX \
    $MER \
    $RER \
    $ORINGIN_NETWORK \
    $ORINGIN_ADDRESS \
    $DESTINATION_NETWORK \
    $ACCOUNT \
    $IN_AMOUNT \
    $METADATA

L2_2_BALANCE_AFTER_CLAIM=$(cast call $L2_2_WRAPPED_L2_1_TOKEN_ADDRESS "balanceOf(address)" "$ACCOUNT" --rpc-url "$L2RPC2")
echo "Balance on L2_2:"
echo "  Before claiming: $L2_2_BALANCE_BEFORE_CLAIM"
echo "  After claiming: $L2_2_BALANCE_AFTER_CLAIM"