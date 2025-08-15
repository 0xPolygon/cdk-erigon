#!/bin/bash

# Token Manager Contracts Deployment Script
set -e

echo "🚀 Token Manager Deployment Script"
echo "=================================="

# Configuration parameters
PRIVATE_KEY="${PRIVATE_KEY:-0x9935c242a0b0ee41edcbd2d963f5bc7f142fdc803eb24f0df396a6fdb16c6af9}"
RPC_URL="${RPC_URL:-http://localhost:8123}"
GAS_PRICE="${GAS_PRICE:-1000000000}"
GAS_LIMIT="${GAS_LIMIT:-5000000}"
MAX_WAIT_SECONDS=60

# Permission separation configuration
PROXY_ADMIN="${PROXY_ADMIN:-0xDE282DC882bbB5100b8A24E30D38a2D5B3080c15}"  # Proxy admin, controls TokenManager contract upgrades (upgrade)
OWNER_ADDRESS="${OWNER_ADDRESS:-$PROXY_ADMIN}"  # TokenManager contract Owner, controls contract pause/unpause/setActivationBlock, temporarily set to ProxyAdmin
ADMIN_ADDRESS="${ADMIN_ADDRESS:-0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534}"  # Business Admin, manages roles (operator) and bridgeFrom whitelist

# Activation configuration
ACTIVATION_BLOCK="${ACTIVATION_BLOCK:-0}"

# Validate environment variables
if [[ "$PRIVATE_KEY" == "0xYOUR_PRIVATE_KEY" || -z "$PRIVATE_KEY" ]]; then
    echo "❌ Error: PRIVATE_KEY not set or invalid"
    exit 1
fi

# Check network connection
if ! cast chain-id --rpc-url "$RPC_URL" > /dev/null; then
    echo "❌ Error: Cannot connect to RPC endpoint $RPC_URL"
    exit 1
fi

# Check deployer balance
DEPLOYER=$(cast wallet address --private-key "$PRIVATE_KEY")
BALANCE=$(cast balance "$DEPLOYER" --rpc-url "$RPC_URL" --ether)
if (( $(echo "$BALANCE < 0.01" | bc -l) )); then
    echo "❌ Error: Insufficient deployer balance ($BALANCE ETH, at least 0.01 ETH required)"
    exit 1
fi

# Compile contracts
cd ../contracts || exit 1
if ! command -v solc &> /dev/null; then
    echo "❌ Error: solc compiler not found"
    exit 1
fi

if ! solc --bin --evm-version paris TokenManagerV1.sol -o . --overwrite --base-path . --include-path node_modules/ > /dev/null 2>&1; then
    echo "❌ Error: Failed to compile TokenManagerV1.sol"
    exit 1
fi

if ! solc --bin --evm-version paris TokenManagerProxy.sol -o . --overwrite --base-path . --include-path node_modules/ > /dev/null 2>&1; then
    echo "❌ Error: Failed to compile TokenManagerProxy.sol"
    exit 1
fi

cd ..

# Read contract bytecode
IMPL_BYTECODE="0x$(cat contracts/TokenManagerV1.bin)"
PROXY_BYTECODE="0x$(cat contracts/TokenManagerProxy.bin)"

# Deploy implementation contract
echo "📋 Deploying implementation contract..."
IMPL_TX=$(cast send --private-key "$PRIVATE_KEY" --rpc-url "$RPC_URL" --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" --legacy --create "$IMPL_BYTECODE" --json | jq -r '.transactionHash')
if [ $? -ne 0 ] || [ -z "$IMPL_TX" ]; then
    echo "❌ Error: Implementation contract deployment failed"
    exit 1
fi

waited=0
while [ $waited -lt $MAX_WAIT_SECONDS ]; do
    IMPL_ADDRESS=$(cast receipt "$IMPL_TX" contractAddress --rpc-url "$RPC_URL" 2>/dev/null)
    if [ -n "$IMPL_ADDRESS" ] && [ "$IMPL_ADDRESS" != "null" ]; then
        break
    fi
    sleep 1
    waited=$((waited + 1))
done

if [ -z "$IMPL_ADDRESS" ] || [ "$IMPL_ADDRESS" = "null" ]; then
    echo "❌ Error: Implementation contract deployment failed"
    exit 1
fi

echo "✅ Implementation contract: $IMPL_ADDRESS"

# Deploy proxy contract
echo "📋 Deploying proxy contract..."
PROXY_CONSTRUCTOR=$(cast abi-encode "constructor(address,address,bytes)" "$IMPL_ADDRESS" "$PROXY_ADMIN" "0x")
PROXY_DEPLOY_DATA="${PROXY_BYTECODE}${PROXY_CONSTRUCTOR:2}"

PROXY_TX=$(cast send --private-key "$PRIVATE_KEY" --rpc-url "$RPC_URL" --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" --legacy --create "$PROXY_DEPLOY_DATA" --json | jq -r '.transactionHash')
if [ $? -ne 0 ] || [ -z "$PROXY_TX" ]; then
    echo "❌ Error: Proxy contract deployment failed"
    exit 1
fi

waited=0
while [ $waited -lt $MAX_WAIT_SECONDS ]; do
    PROXY_ADDRESS=$(cast receipt "$PROXY_TX" contractAddress --rpc-url "$RPC_URL" 2>/dev/null)
    if [ -n "$PROXY_ADDRESS" ] && [ "$PROXY_ADDRESS" != "null" ]; then
        break
    fi
    sleep 1
    waited=$((waited + 1))
done

if [ -z "$PROXY_ADDRESS" ] || [ "$PROXY_ADDRESS" = "null" ]; then
    echo "❌ Error: Proxy contract deployment failed"
    exit 1
fi

echo "✅ Proxy contract: $PROXY_ADDRESS"

# Initialize contract
echo "📋 Initializing contract..."
INIT_DATA=$(cast calldata "initialize(address,address)" "$OWNER_ADDRESS" "$ADMIN_ADDRESS")
INIT_TX=$(cast send "$PROXY_ADDRESS" "$INIT_DATA" --private-key "$PRIVATE_KEY" --rpc-url "$RPC_URL" --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" --legacy --json | jq -r '.transactionHash')
if [ $? -ne 0 ] || [ -z "$INIT_TX" ]; then
    echo "❌ Error: Initialization failed"
    exit 1
fi

# Set activation block
echo "📋 Setting activation block..."
ACTIVATION_DATA=$(cast calldata "setActivationBlock(uint256)" "$ACTIVATION_BLOCK")
ACTIVATION_TX=$(cast send "$PROXY_ADDRESS" "$ACTIVATION_DATA" --private-key "$PRIVATE_KEY" --rpc-url "$RPC_URL" --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" --legacy --json | jq -r '.transactionHash')
if [ $? -ne 0 ] || [ -z "$ACTIVATION_TX" ]; then
    echo "❌ Error: Failed to set activation block"
    exit 1
fi

echo "✅ Activation block set successfully"

# Verify deployment
echo "📋 Verifying deployment..."

# Verify Owner
CURRENT_OWNER=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "owner()" 2>/dev/null)
# Remove leading 24 zero bytes (48 characters)
CURRENT_OWNER="0x${CURRENT_OWNER:26}"
CURRENT_OWNER=$(cast to-check-sum-address "$CURRENT_OWNER" 2>/dev/null || echo "$CURRENT_OWNER")
EXPECTED_OWNER=$(cast to-check-sum-address "$OWNER_ADDRESS")

if [ "$CURRENT_OWNER" != "$EXPECTED_OWNER" ]; then
    echo "❌ Error: Owner verification failed"
    echo "  Expected: $EXPECTED_OWNER"
    echo "  Actual: $CURRENT_OWNER"
    exit 1
fi

# Verify Admin
CURRENT_ADMIN=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "admin()" 2>/dev/null)
# Remove leading 24 zero bytes (48 characters)
CURRENT_ADMIN="0x${CURRENT_ADMIN:26}"
CURRENT_ADMIN=$(cast to-check-sum-address "$CURRENT_ADMIN" 2>/dev/null || echo "$CURRENT_ADMIN")
EXPECTED_ADMIN=$(cast to-check-sum-address "$ADMIN_ADDRESS")

if [ "$CURRENT_ADMIN" != "$EXPECTED_ADMIN" ]; then
    echo "❌ Error: Admin verification failed"
    echo "  Expected: $EXPECTED_ADMIN"
    echo "  Actual: $CURRENT_ADMIN"
    exit 1
fi

VERSION_RAW=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "VERSION()" 2>/dev/null)
VERSION=$(cast to-ascii "$VERSION_RAW" 2>/dev/null || echo "Unable to decode")

IS_ACTIVE_RAW=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "isActive()" 2>/dev/null)
IS_ACTIVE=$([ "$IS_ACTIVE_RAW" = "0x0000000000000000000000000000000000000000000000000000000000000001" ] && echo "Active" || echo "Inactive")

# Deployment summary
echo "🎉 Deployment completed"
echo "===================="
echo ""
echo "📋 Deployed contracts:"
echo "  Implementation contract: $IMPL_ADDRESS"
echo "  Proxy contract (main contract): $PROXY_ADDRESS"
echo ""
echo "👑 Permission separation architecture:"
echo "  Proxy admin: $PROXY_ADMIN (contract upgrades) = Owner address"
echo "  System Owner: $CURRENT_OWNER (pause/unpause)"
echo "  Business Admin: $CURRENT_ADMIN (operator management/bridgeFrom/cleanUp)"
echo ""
echo "⚙️ Configuration:"
echo "  Status: $IS_ACTIVE"
echo "  Version: $VERSION"
echo ""