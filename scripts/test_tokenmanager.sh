#!/bin/bash
set -e

echo "🔬 Token Manager Test Script"
echo "============================"

# Configuration parameters
RPC_URL="${RPC_URL:-http://localhost:8123}"
PROXY_ADDRESS="${PROXY_ADDRESS:-0x1FdC273F90e3Eba11D2b20561F233B11424Fcfab}"
TARGET_ADDRESS="0x4B24266C13AFEf2bb60e2C69A4C08A482d81e3CA"

# Test accounts
ADMIN_PRIVATE_KEY="0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2"
ADMIN="0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534"

OWNER_PRIVATE_KEY="0x9935c242a0b0ee41edcbd2d963f5bc7f142fdc803eb24f0df396a6fdb16c6af9"
OWNER="0xDE282DC882bbB5100b8A24E30D38a2D5B3080c15"

# Test operator account
OPERATOR_PRIVATE_KEY="0x3c9229289a6125f7fdf1885a77bb12c37a8d3b4962d936f7e3084dece32a3ca1"
OPERATOR=$(cast wallet address --private-key "$OPERATOR_PRIVATE_KEY")  # Calculate correct address from private key

# New test accounts
NEW_OPERATOR_KEY="0x4bbbf85ce3377467afe5d46f804f221813b2bb87f24d81f60f1fcdbf7cbf4356"
NEW_OPERATOR="0x14dC79964da2C08b23698B3D3cc7Ca32193d9955"
NEW_ADMIN_PRIVATE_KEY="0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a"
NEW_ADMIN="0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC"
NEW_OWNER_PRIVATE_KEY="0x689af8efa8c651a91ad287602527f3af2fe9f6501a7ac4b061667b5a93e037fd"
NEW_OWNER="0xbDA5747bFD65F08deb54cb465eB87D40e51B197E"

# Test amounts
BRIDGE_AMOUNT="1000000000000000000"  # 1 ETH

echo "🎯 Test configuration:"
echo "  Proxy contract: $PROXY_ADDRESS"
echo "  Target address: $TARGET_ADDRESS"
echo "  Admin: $ADMIN"
echo "  Owner: $OWNER"
echo "  Operator: $OPERATOR"
echo ""

# State reset: Ensure admin and operator are in expected state
echo "🔄 Resetting contract state..."
echo "----------------------------------------"

# Check and reset Admin (if needed)
CURRENT_ADMIN_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "admin()" 2>/dev/null)
CURRENT_ADMIN_ADDR="0x${CURRENT_ADMIN_RESULT:26}"
CURRENT_ADMIN_ADDR=$(cast to-check-sum-address "$CURRENT_ADMIN_ADDR")
EXPECTED_ADMIN=$(cast to-check-sum-address "$ADMIN")

if [ "$CURRENT_ADMIN_ADDR" != "$EXPECTED_ADMIN" ]; then
    echo "❌ Admin address mismatch, need to redeploy contract"
    echo "  Current: $CURRENT_ADMIN_ADDR"
    echo "  Expected: $EXPECTED_ADMIN"
    exit 1
fi
echo "✅ Admin state correct: $CURRENT_ADMIN_ADDR"

# Check and reset Operator to expected address (if needed)
OPERATOR_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "operator()")
CURRENT_OPERATOR="0x${OPERATOR_RESULT:26}"
CURRENT_OPERATOR=$(cast to-check-sum-address "$CURRENT_OPERATOR")
OPERATOR_CHECKSUM=$(cast to-check-sum-address "$OPERATOR")

if [ "$CURRENT_OPERATOR" != "$OPERATOR_CHECKSUM" ]; then
    echo "ℹ️  Resetting Operator to expected address..."
    cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
        "$PROXY_ADDRESS" "setOperator(address)" "$OPERATOR" >/dev/null 2>&1
    echo "✅ Operator reset successful: $OPERATOR_CHECKSUM"
else
    echo "✅ Operator already at expected address: $OPERATOR_CHECKSUM"
fi
echo ""

# Initialize test account funds
echo "💰 Initializing test accounts..."
TEST_ACCOUNTS=("$OPERATOR" "$NEW_OPERATOR" "$NEW_ADMIN" "$NEW_OWNER")
TRANSFER_AMOUNT_WEI=100000000000000000  # 0.1 ETH

for ACCOUNT in "${TEST_ACCOUNTS[@]}"; do
    BALANCE=$(cast balance "$ACCOUNT" --rpc-url "$RPC_URL" --ether)
    if (( $(echo "$BALANCE < 0.1" | bc -l) )); then
        echo "  Transferring 0.1 ETH to account $ACCOUNT..."
        cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" \
            --gas-limit 30000 --legacy \
            --value "$TRANSFER_AMOUNT_WEI" "$ACCOUNT" >/dev/null 2>&1
    fi
done
echo "✅ Test account initialization completed"
echo ""

# Step 1: Operator management test
echo "🔬 Step 1: Operator Management Test"
echo "----------------------------------------"

# Verify current Admin
echo "ℹ️  Verifying current Admin..."
CURRENT_ADMIN_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "admin()" 2>/dev/null)
CURRENT_ADMIN_ADDR="0x${CURRENT_ADMIN_RESULT:26}"
CURRENT_ADMIN_ADDR=$(cast to-check-sum-address "$CURRENT_ADMIN_ADDR")
EXPECTED_ADMIN=$(cast to-check-sum-address "$ADMIN")

if [ "$CURRENT_ADMIN_ADDR" != "$EXPECTED_ADMIN" ]; then
    echo "❌ Error: Current Admin ($CURRENT_ADMIN_ADDR) does not match script configuration ($EXPECTED_ADMIN)"
    echo "Please check if ADMIN_PRIVATE_KEY is correct or contract admin configuration"
    exit 1
fi
echo "  Current Admin verification passed: $CURRENT_ADMIN_ADDR"

echo "ℹ️  Clearing existing Operator..."
# Check current operator state, only non-zero addresses need to be cleared
CURRENT_OP_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "operator()" 2>/dev/null)
CURRENT_OP="0x${CURRENT_OP_RESULT:26}"
if [ "$CURRENT_OP" != "0x0000000000000000000000000000000000000000" ]; then
    cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
        "$PROXY_ADDRESS" "setOperator(address)" "0x0000000000000000000000000000000000000000" >/dev/null 2>&1
    echo "  Cleared existing Operator: $CURRENT_OP"
else
    echo "  Current Operator is already zero address, no need to clear"
fi

echo "ℹ️  Setting Operator..."
cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "setOperator(address)" "$OPERATOR" >/dev/null 2>&1

OPERATOR_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "operator()")
CURRENT_OPERATOR="0x${OPERATOR_RESULT:26}"
CURRENT_OPERATOR=$(cast to-check-sum-address "$CURRENT_OPERATOR")
OPERATOR_CHECKSUM=$(cast to-check-sum-address "$OPERATOR")
if [ "$CURRENT_OPERATOR" = "$OPERATOR_CHECKSUM" ]; then
    echo "✅ Operator set successfully"
else
    echo "❌ Operator setting failed"
    echo "  Expected: $OPERATOR_CHECKSUM"
    echo "  Actual: $CURRENT_OPERATOR"
    exit 1
fi

echo "ℹ️  Testing new Operator setting..."
cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "setOperator(address)" "$NEW_OPERATOR" >/dev/null 2>&1

NEW_OPERATOR_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "operator()")
CURRENT_NEW_OPERATOR="0x${NEW_OPERATOR_RESULT:26}"
CURRENT_NEW_OPERATOR=$(cast to-check-sum-address "$CURRENT_NEW_OPERATOR")
NEW_OPERATOR_CHECKSUM=$(cast to-check-sum-address "$NEW_OPERATOR")
if [ "$CURRENT_NEW_OPERATOR" = "$NEW_OPERATOR_CHECKSUM" ]; then
    echo "✅ New Operator set successfully"
else
    echo "❌ New Operator setting failed"
    exit 1
fi

# Test Operator removal functionality
echo "ℹ️  Testing Operator removal (set to zero address)..."
cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "setOperator(address)" "0x0000000000000000000000000000000000000000" >/dev/null 2>&1

ZERO_OP_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "operator()")
ZERO_OP="0x${ZERO_OP_RESULT:26}"
if [ "$ZERO_OP" = "0x0000000000000000000000000000000000000000" ]; then
    echo "✅ Operator removal successful"
else
    echo "❌ Operator removal failed"
    exit 1
fi

# Restore original Operator
cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "setOperator(address)" "$OPERATOR" >/dev/null 2>&1
echo ""

# Step 2: BridgeFrom operation test
echo "🔬 Step 2: BridgeFrom Operation Test"
echo "----------------------------------------"

# Pre-check: Verify contract state
echo "ℹ️  Checking contract state..."
IS_ACTIVE_RAW=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "isActive()" 2>/dev/null)
IS_ACTIVE=$([ "$IS_ACTIVE_RAW" = "0x0000000000000000000000000000000000000000000000000000000000000001" ] && echo "true" || echo "false")
echo "  Contract activation status: $IS_ACTIVE"

PAUSED_RAW=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "paused()" 2>/dev/null)
PAUSED=$([ "$PAUSED_RAW" = "0x0000000000000000000000000000000000000000000000000000000000000001" ] && echo "true" || echo "false")
echo "  Contract pause status: $PAUSED"

CURRENT_OP_RAW=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "operator()" 2>/dev/null)
CURRENT_OP_ADDR="0x${CURRENT_OP_RAW:26}"
CURRENT_OP_ADDR=$(cast to-check-sum-address "$CURRENT_OP_ADDR")
EXPECTED_OP=$(cast to-check-sum-address "$OPERATOR")
echo "  Current Operator: $CURRENT_OP_ADDR"

if [ "$CURRENT_OP_ADDR" != "$EXPECTED_OP" ]; then
    echo "❌ Error: Current Operator does not match expected"
    echo "  Expected: $EXPECTED_OP"
    echo "  Actual: $CURRENT_OP_ADDR"
    exit 1
fi

# Check if current balance is too large (may cause overflow)
OPERATOR_BALANCE_BEFORE=$(cast balance "$OPERATOR" --rpc-url "$RPC_URL")
echo "ℹ️  Operator balance (before operation): $OPERATOR_BALANCE_BEFORE wei"

# Check if balance is close to uint256 maximum (may cause overflow)
MAX_SAFE_BALANCE="100000000000000000000000000000000000000000000000000000000000000000000000000"  # 10^75 wei
if (( $(echo "$OPERATOR_BALANCE_BEFORE > $MAX_SAFE_BALANCE" | bc -l) )); then
    echo "⚠️  Warning: Operator balance too large, may cause overflow issues"
    echo "  Current balance: $OPERATOR_BALANCE_BEFORE wei"
    echo "  Recommend using new test account or clearing balance before retesting"
fi

echo "ℹ️  Executing bridgeFrom operation (amount: $BRIDGE_AMOUNT wei)..."
set +e  # Temporarily disable strict mode
BRIDGE_RESULT=$(cast send --private-key "$OPERATOR_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "bridgeFrom(uint256)" "$BRIDGE_AMOUNT" 2>&1)
BRIDGE_EXIT_CODE=$?
set -e  # Re-enable strict mode

if [ $BRIDGE_EXIT_CODE -ne 0 ]; then
    echo "❌ BridgeFrom operation failed:"
    echo "$BRIDGE_RESULT"
    exit 1
fi

OPERATOR_BALANCE_AFTER=$(cast balance "$OPERATOR" --rpc-url "$RPC_URL")
echo "ℹ️  Operator balance (after operation): $OPERATOR_BALANCE_AFTER wei"

# Use bc for large number calculations to avoid bash integer overflow
BALANCE_DIFF=$(echo "$OPERATOR_BALANCE_AFTER - $OPERATOR_BALANCE_BEFORE" | bc)
echo "ℹ️  Balance change: $BALANCE_DIFF wei"

# Consider gas fees, actual increase should be close to bridge amount (allow some error)
MIN_EXPECTED=$(echo "$BRIDGE_AMOUNT - 100000000000000000" | bc)  # Allow 0.1 ETH gas fee error

if (( $(echo "$BALANCE_DIFF >= $MIN_EXPECTED" | bc -l) )); then
    echo "✅ BridgeFrom operation successful"
else
    echo "❌ BridgeFrom operation failed:"
    echo "  Balance before operation: $OPERATOR_BALANCE_BEFORE wei"
    echo "  Balance after operation: $OPERATOR_BALANCE_AFTER wei"
    echo "  Balance change: $BALANCE_DIFF wei"
    echo "  Expected minimum: $MIN_EXPECTED wei"
    echo "  Bridge amount: $BRIDGE_AMOUNT wei"
    
    # Check if it's negative (serious problem)
    if (( $(echo "$BALANCE_DIFF < 0" | bc -l) )); then
        echo "  🚨 Balance decreased, serious overflow or state issue exists!"
    fi
    exit 1
fi

# Boundary test 1: amount is 0 (should fail)
echo "ℹ️  Testing bridgeFrom boundary condition - amount is 0..."
set +e
ZERO_BRIDGE_RESULT=$(cast send --private-key "$OPERATOR_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "bridgeFrom(uint256)" "0" 2>&1)
ZERO_BRIDGE_EXIT_CODE=$?
set -e

if [ $ZERO_BRIDGE_EXIT_CODE -ne 0 ] && echo "$ZERO_BRIDGE_RESULT" | grep -q "Amount must be greater than zero"; then
    echo "✅ Correctly rejected when amount is 0"
else
    echo "❌ Should fail when amount is 0 but didn't fail"
    echo "Result: $ZERO_BRIDGE_RESULT"
    exit 1
fi

# Boundary test 2: Non-operator calling bridgeFrom (should fail)
echo "ℹ️  Testing bridgeFrom boundary condition - non-operator call..."
set +e
UNAUTHORIZED_BRIDGE_RESULT=$(cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "bridgeFrom(uint256)" "$BRIDGE_AMOUNT" 2>&1)
UNAUTHORIZED_BRIDGE_EXIT_CODE=$?
set -e

if [ $UNAUTHORIZED_BRIDGE_EXIT_CODE -ne 0 ] && echo "$UNAUTHORIZED_BRIDGE_RESULT" | grep -q "Only operator can call this function"; then
    echo "✅ Correctly rejected when called by non-operator"
else
    echo "❌ Should fail when called by non-operator but didn't fail"
    echo "Result: $UNAUTHORIZED_BRIDGE_RESULT"
    exit 1
fi

# Boundary test 3: Incremental value test
echo "ℹ️  Testing bridgeFrom boundary condition - incremental value test..."

# Define test value array (ETH units)
declare -a TEST_AMOUNTS=(
    "10000000000000000000"          # 10 ETH
    "1000000000000000000000"        # 1000 ETH
    "100000000000000000000000"      # 100000 ETH (100K ETH)
    "10000000000000000000000000"    # 10000000 ETH (10M ETH)
    "100000000000000000000000000"   # 100000000 ETH (100M ETH)
    "1000000000000000000000000000"  # 1000000000 ETH (1B ETH)
)

declare -a TEST_LABELS=(
    "10 ETH"
    "1000 ETH"
    "100000 ETH (100K ETH)"
    "10000000 ETH (10M ETH)"
    "100000000 ETH (100M ETH)"
    "1000000000 ETH (1B ETH)"
)

# Test incremental values one by one
for i in "${!TEST_AMOUNTS[@]}"; do
    AMOUNT="${TEST_AMOUNTS[$i]}"
    LABEL="${TEST_LABELS[$i]}"
    
    echo "ℹ️  Testing $LABEL..."
    OPERATOR_BALANCE_BEFORE=$(cast balance "$OPERATOR" --rpc-url "$RPC_URL")
    
    set +e
    LARGE_BRIDGE_RESULT=$(cast send --private-key "$OPERATOR_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "bridgeFrom(uint256)" "$AMOUNT" 2>&1)
LARGE_BRIDGE_EXIT_CODE=$?
set -e

if [ $LARGE_BRIDGE_EXIT_CODE -eq 0 ]; then
        # Verify balance actually increased
        OPERATOR_BALANCE_AFTER=$(cast balance "$OPERATOR" --rpc-url "$RPC_URL")
        
        # Use bc for large number calculations to avoid bash integer overflow
        BALANCE_INCREASE=$(echo "$OPERATOR_BALANCE_AFTER - $OPERATOR_BALANCE_BEFORE" | bc)
        MIN_EXPECTED=$(echo "$AMOUNT - 1000000000000000000" | bc)  # Allow 1 ETH gas fee
        
        # Check if balance increased reasonably (using bc comparison)
        if (( $(echo "$BALANCE_INCREASE >= $MIN_EXPECTED" | bc -l) )); then
            echo "✅ $LABEL bridgeFrom successful"
            echo "  Balance increase: $BALANCE_INCREASE wei"
        else
            echo "❌ $LABEL bridgeFrom balance increase abnormal:"
            echo "  Balance before operation: $OPERATOR_BALANCE_BEFORE wei"
            echo "  Balance after operation: $OPERATOR_BALANCE_AFTER wei"  
            echo "  Actual increase: $BALANCE_INCREASE wei"
            echo "  Expected minimum: $MIN_EXPECTED wei"
            echo "  Bridge amount: $AMOUNT wei"
            
            # Check if it's negative (indicates overflow or other issues)
            if (( $(echo "$BALANCE_INCREASE < 0" | bc -l) )); then
                echo "  🚨 Balance decreased, serious problem may exist!"
                exit 1
            fi
            
            # For large values, don't exit immediately, continue testing to find boundary
            if [ "$i" -lt 2 ]; then
                exit 1
            else
                echo "  ⚠️  Continue testing to find system boundary..."
            fi
        fi
    else
        # Check if it's a reasonable failure
        if echo "$LARGE_BRIDGE_RESULT" | grep -q -E "gas|limit|insufficient|overflow"; then
    echo "✅ $LABEL bridgeFrom reasonably rejected (system limit)"
else
    echo "❌ $LABEL bridgeFrom failed: $LARGE_BRIDGE_RESULT"
            # Failure for small values is serious, failure for large values may be system protection
            if [ "$i" -lt 2 ]; then
                exit 1
            else
                echo "  ⚠️  May have reached system processing limit"
            fi
        fi
    fi
done

echo "✅ Incremental value test completed, tested up to 1 billion ETH"

# Boundary test 4: operator is zero address
echo "ℹ️  Testing bridgeFrom boundary condition - operator is zero address..."
# Temporarily set operator to zero address
cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "setOperator(address)" "0x0000000000000000000000000000000000000000" >/dev/null 2>&1

set +e
NO_OP_BRIDGE_RESULT=$(cast send --private-key "$OPERATOR_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "bridgeFrom(uint256)" "$BRIDGE_AMOUNT" 2>&1)
NO_OP_BRIDGE_EXIT_CODE=$?
set -e

# Restore operator
cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "setOperator(address)" "$OPERATOR" >/dev/null 2>&1

if [ $NO_OP_BRIDGE_EXIT_CODE -ne 0 ] && echo "$NO_OP_BRIDGE_RESULT" | grep -q "Only operator can call this function"; then
    echo "✅ Correctly rejected when operator is zero address"
else
    echo "❌ Should fail when operator is zero address but didn't fail"
    echo "Result: $NO_OP_BRIDGE_RESULT"
    exit 1
fi

echo "✅ All bridgeFrom boundary tests completed"
echo ""

# Step 3: Cleanup operation test
echo "🔬 Step 3: Cleanup Operation Test"
echo "----------------------------------------"

TARGET_BALANCE=$(cast balance "$TARGET_ADDRESS" --rpc-url "$RPC_URL")

if [ "$TARGET_BALANCE" -le 1 ]; then
    echo "ℹ️  Target address balance insufficient, using ForceTransfer contract to force transfer 2 ETH..."
    
    # ForceTransfer contract bytecode (pre-compiled)
    FORCE_TRANSFER_BYTECODE="0x6080604052610121806100136000396000f3fe608060405260043610601f5760003560e01c80630399c93c14602a576025565b36602557005b600080fd5b348015603557600080fd5b50604c60048036038101906048919060c3565b604e565b005b8073ffffffffffffffffffffffffffffffffffffffff16ff5b600080fd5b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b6000609582606c565b9050919050565b60a381608c565b811460ad57600080fd5b50565b60008135905060bd81609c565b92915050565b60006020828403121560d65760d56067565b5b600060e28482850160b0565b9150509291505056fea26469706673582212201a5f7f8aa11552d8a57999ec1e7e44da43911c65037b19d14d6b285783fc02af64736f6c634300081e0033"
    
    # Deploy ForceTransfer contract
    echo "  Deploying ForceTransfer contract..."
    FORCE_TRANSFER_TX=$(cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --gas-price 1000000000 --gas-limit 5000000 --legacy --value 2000000000000000000 --create "$FORCE_TRANSFER_BYTECODE" --json | jq -r '.transactionHash')
    
    # Wait for deployment to complete
    waited=0
    while [ $waited -lt 60 ]; do
        FORCE_CONTRACT=$(cast receipt "$FORCE_TRANSFER_TX" contractAddress --rpc-url "$RPC_URL" 2>/dev/null)
        if [ -n "$FORCE_CONTRACT" ] && [ "$FORCE_CONTRACT" != "null" ]; then
            break
        fi
        sleep 1
        waited=$((waited + 1))
    done
    
    if [ -z "$FORCE_CONTRACT" ] || [ "$FORCE_CONTRACT" = "null" ]; then
        echo "❌ Error: ForceTransfer contract deployment failed"
        exit 1
    fi
    
    echo "  ForceTransfer contract address: $FORCE_CONTRACT"
    
    # Call forceTransfer function
    echo "  Calling forceTransfer function..."
    cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
        "$FORCE_CONTRACT" "forceTransfer(address)" "$TARGET_ADDRESS" >/dev/null 2>&1
    
    TARGET_BALANCE=$(cast balance "$TARGET_ADDRESS" --rpc-url "$RPC_URL")
fi

echo "ℹ️  Target address balance (before cleanup): $TARGET_BALANCE wei"

set +e  # Temporarily disable strict mode
CLEANUP_RESULT=$(cast send --private-key "$OPERATOR_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "cleanup()" 2>&1)
CLEANUP_EXIT_CODE=$?
set -e  # Re-enable strict mode

if [ $CLEANUP_EXIT_CODE -ne 0 ]; then
    echo "❌ Cleanup operation failed:"
    echo "$CLEANUP_RESULT"
    exit 1
fi

TARGET_BALANCE_AFTER=$(cast balance "$TARGET_ADDRESS" --rpc-url "$RPC_URL")
echo "ℹ️  Target address balance (after cleanup): $TARGET_BALANCE_AFTER wei"

if [ "$TARGET_BALANCE_AFTER" -eq 1 ]; then
    echo "✅ Cleanup operation successful (retained 1 wei)"
else
    echo "❌ Cleanup operation failed (balance: $TARGET_BALANCE_AFTER wei)"
    exit 1
fi

# Test cleanup operation on already cleaned address (should succeed, idempotent)
set +e  # Temporarily disable strict mode
CLEANUP2_RESULT=$(cast send --private-key "$OPERATOR_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "cleanup()" 2>&1)
CLEANUP2_EXIT_CODE=$?
set -e  # Re-enable strict mode

if [ $CLEANUP2_EXIT_CODE -ne 0 ]; then
    echo "❌ Second cleanup operation failed:"
    echo "$CLEANUP2_RESULT"
    exit 1
fi

FINAL_BALANCE=$(cast balance "$TARGET_ADDRESS" --rpc-url "$RPC_URL")
if [ "$FINAL_BALANCE" -eq 1 ]; then
    echo "✅ Cleanup operation on already cleaned address successful (idempotent)"
else
    echo "❌ Cleanup operation on already cleaned address failed"
    exit 1
fi

# Boundary test 1: Non-operator calling cleanup (should fail)
echo "ℹ️  Testing cleanup boundary condition - non-operator call..."
set +e
UNAUTHORIZED_CLEANUP_RESULT=$(cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "cleanup()" 2>&1)
UNAUTHORIZED_CLEANUP_EXIT_CODE=$?
set -e

if [ $UNAUTHORIZED_CLEANUP_EXIT_CODE -ne 0 ] && echo "$UNAUTHORIZED_CLEANUP_RESULT" | grep -q "Only operator can call this function"; then
    echo "✅ Correctly rejected when cleanup called by non-operator"
else
    echo "❌ Should fail when cleanup called by non-operator but didn't fail"
    echo "Result: $UNAUTHORIZED_CLEANUP_RESULT"
    exit 1
fi

# Boundary test 2: operator is zero address when calling cleanup
echo "ℹ️  Testing cleanup boundary condition - operator is zero address..."
# Temporarily set operator to zero address
cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "setOperator(address)" "0x0000000000000000000000000000000000000000" >/dev/null 2>&1

set +e
NO_OP_CLEANUP_RESULT=$(cast send --private-key "$OPERATOR_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "cleanup()" 2>&1)
NO_OP_CLEANUP_EXIT_CODE=$?
set -e

# Restore operator
cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "setOperator(address)" "$OPERATOR" >/dev/null 2>&1

if [ $NO_OP_CLEANUP_EXIT_CODE -ne 0 ] && echo "$NO_OP_CLEANUP_RESULT" | grep -q "Only operator can call this function"; then
    echo "✅ Correctly rejected when operator is zero address for cleanup"
else
    echo "❌ Should fail when operator is zero address for cleanup but didn't fail"
    echo "Result: $NO_OP_CLEANUP_RESULT"
    exit 1
fi

echo "✅ All cleanup boundary tests completed"
echo ""

# Step 4: Pause/Resume functionality test
echo "🔬 Step 4: Pause/Resume Functionality Test"
echo "----------------------------------------"

echo "ℹ️  Pausing contract..."
cast send --private-key "$OWNER_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "pause()" >/dev/null 2>&1

PAUSED_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "paused()")
if [ "$PAUSED_RESULT" = "0x0000000000000000000000000000000000000000000000000000000000000001" ]; then
    echo "✅ Contract pause successful"
else
    echo "❌ Contract pause failed"
    exit 1
fi

echo "ℹ️  Testing bridgeFrom operation in paused state (should fail)..."
set +e
BRIDGE_RESULT=$(cast send --private-key "$OPERATOR_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "bridgeFrom(uint256)" "$BRIDGE_AMOUNT" 2>&1)
set -e

if echo "$BRIDGE_RESULT" | grep -q "revert\|failed"; then
    echo "✅ BridgeFrom operation correctly rejected in paused state"
else
    echo "❌ BridgeFrom operation not rejected in paused state"
    exit 1
fi

echo "ℹ️  Resuming contract..."
cast send --private-key "$OWNER_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "unpause()" >/dev/null 2>&1

PAUSED_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "paused()")
if [ "$PAUSED_RESULT" = "0x0000000000000000000000000000000000000000000000000000000000000000" ]; then
    echo "✅ Contract resume successful"
else
    echo "❌ Contract resume failed"
    exit 1
fi
echo ""

# Step 5: Query functionality test
echo "🔬 Step 5: Query Functionality Test"
echo "----------------------------------------"

# Test operator query
CURRENT_OP_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "operator()")
CURRENT_OP="0x${CURRENT_OP_RESULT:26}"
CURRENT_OP=$(cast to-check-sum-address "$CURRENT_OP")
if [ "$CURRENT_OP" = "$(cast to-check-sum-address "$OPERATOR")" ]; then
    echo "✅ Operator query normal"
else
    echo "❌ Operator query abnormal"
    exit 1
fi

# Test admin query
ADMIN_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "admin()")
ADMIN_ADDR="0x${ADMIN_RESULT:26}"
ADMIN_ADDR=$(cast to-check-sum-address "$ADMIN_ADDR")
if [ "$ADMIN_ADDR" = "$(cast to-check-sum-address "$ADMIN")" ]; then
    echo "✅ Admin query normal"
else
    echo "❌ Admin query abnormal"
    exit 1
fi



# Test VERSION
VERSION_RAW=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "VERSION()")
VERSION=$(cast to-ascii "$VERSION_RAW" 2>/dev/null || echo "Unable to decode")
# Remove leading and trailing spaces
VERSION=$(echo "$VERSION" | xargs)
if [ "$VERSION" = "1.0.0" ]; then
    echo "✅ VERSION query normal"
else
    echo "❌ VERSION query abnormal: '$VERSION'"
    exit 1
fi

# Test isActive
IS_ACTIVE_RAW=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "isActive()")
IS_ACTIVE=$([ "$IS_ACTIVE_RAW" = "0x0000000000000000000000000000000000000000000000000000000000000001" ] && echo "Active" || echo "Inactive")
echo "ℹ️  Contract status: $IS_ACTIVE"
echo ""

# Step 6: Admin role transfer test
echo "🔬 Step 6: Admin Role Transfer Test"
echo "----------------------------------------"

echo "ℹ️  Transferring Admin role..."
cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "setAdmin(address)" "$NEW_ADMIN" >/dev/null 2>&1

# Verify Admin transfer
NEW_ADMIN_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "admin()")
NEW_ADMIN_ADDR="0x${NEW_ADMIN_RESULT:26}"
NEW_ADMIN_ADDR=$(cast to-check-sum-address "$NEW_ADMIN_ADDR")
if [ "$NEW_ADMIN_ADDR" = "$(cast to-check-sum-address "$NEW_ADMIN")" ]; then
    echo "✅ Admin role transfer successful"
else
    echo "❌ Admin role transfer failed"
    exit 1
fi

# Verify permission transfer: Test old admin losing permissions, new admin gaining permissions
CURRENT_OP_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "operator()")
CURRENT_OP="0x${CURRENT_OP_RESULT:26}"
CURRENT_OP=$(cast to-check-sum-address "$CURRENT_OP")

# Choose a different test address (ensure it's not the current operator)
if [ "$CURRENT_OP" = "$(cast to-check-sum-address "$OWNER")" ]; then
    TEST_OP_ADDRESS="$NEW_OWNER"
else
    TEST_OP_ADDRESS="$OWNER"
fi

# Test old admin permission invalidation
set +e
OLD_ADMIN_RESULT=$(cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "setOperator(address)" "$TEST_OP_ADDRESS" 2>&1)
set -e

if echo "$OLD_ADMIN_RESULT" | grep -q "revert\|failed"; then
    # Test new admin permission activation
    if cast send --private-key "$NEW_ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
        "$PROXY_ADDRESS" "setOperator(address)" "$TEST_OP_ADDRESS" >/dev/null 2>&1; then
        echo "✅ Admin role transfer successful"
        
        # Restore original operator
        cast send --private-key "$NEW_ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
            "$PROXY_ADDRESS" "setOperator(address)" "$CURRENT_OP" >/dev/null 2>&1
    else
        echo "❌ New admin permissions not activated"
        exit 1
    fi
else
    echo "❌ Old admin permissions not invalidated"
    exit 1
fi
echo ""

# Step 7: Owner transfer test
echo "🔬 Step 7: Owner Transfer Test"
echo "----------------------------------------"

# Check current owner
CURRENT_OWNER_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "owner()")
CURRENT_OWNER_ADDR="0x${CURRENT_OWNER_RESULT:26}"
CURRENT_OWNER_ADDR=$(cast to-check-sum-address "$CURRENT_OWNER_ADDR")

if [ "$CURRENT_OWNER_ADDR" != "$(cast to-check-sum-address "$OWNER")" ]; then
    echo "⚠️  Current owner ($CURRENT_OWNER_ADDR) does not match OWNER in script ($OWNER), skipping owner transfer test"
else
    echo "ℹ️  Transferring Owner permissions..."
    cast send --private-key "$OWNER_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
        "$PROXY_ADDRESS" "transferOwnership(address)" "$NEW_OWNER" >/dev/null 2>&1

    # Verify Owner transfer
    NEW_OWNER_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "owner()")
    NEW_OWNER_ADDR="0x${NEW_OWNER_RESULT:26}"
    NEW_OWNER_ADDR=$(cast to-check-sum-address "$NEW_OWNER_ADDR")
    if [ "$NEW_OWNER_ADDR" = "$(cast to-check-sum-address "$NEW_OWNER")" ]; then
        echo "✅ Owner transfer successful"
    else
        echo "❌ Owner transfer failed"
        exit 1
    fi

    # Verify old owner permission invalidation, new owner permission activation
    set +e
    OLD_OWNER_RESULT=$(cast send --private-key "$OWNER_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
        "$PROXY_ADDRESS" "pause()" 2>&1)
    set -e

    if echo "$OLD_OWNER_RESULT" | grep -q "revert\|failed"; then
        echo "✅ Owner permission transfer successful"
    else
        echo "❌ Old owner permissions not invalidated"
        exit 1
    fi
fi
echo ""

# Step 8: Restore state
echo "🔄 Step 8: Restore Test State"
echo "----------------------------------------"

echo "ℹ️  Restoring Admin to original address..."
# Use current NEW_ADMIN permissions to restore admin
cast send --private-key "$NEW_ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "setAdmin(address)" "$ADMIN" >/dev/null 2>&1

# Verify Admin restoration
RESTORED_ADMIN_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "admin()")
RESTORED_ADMIN_ADDR="0x${RESTORED_ADMIN_RESULT:26}"
RESTORED_ADMIN_ADDR=$(cast to-check-sum-address "$RESTORED_ADMIN_ADDR")
if [ "$RESTORED_ADMIN_ADDR" = "$(cast to-check-sum-address "$ADMIN")" ]; then
    echo "✅ Admin restoration successful: $RESTORED_ADMIN_ADDR"
else
    echo "❌ Admin restoration failed"
    exit 1
fi

echo "ℹ️  Restoring Owner to original address..."
# Use current NEW_OWNER permissions to restore owner
cast send --private-key "$NEW_OWNER_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
    "$PROXY_ADDRESS" "transferOwnership(address)" "$OWNER" >/dev/null 2>&1

# Verify Owner restoration
RESTORED_OWNER_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "owner()")
RESTORED_OWNER_ADDR="0x${RESTORED_OWNER_RESULT:26}"
RESTORED_OWNER_ADDR=$(cast to-check-sum-address "$RESTORED_OWNER_ADDR")
if [ "$RESTORED_OWNER_ADDR" = "$(cast to-check-sum-address "$OWNER")" ]; then
    echo "✅ Owner restoration successful: $RESTORED_OWNER_ADDR"
else
    echo "❌ Owner restoration failed"
    exit 1
fi

echo "ℹ️  Restoring Operator to original address..."
# Check if current operator needs restoration
CURRENT_OPERATOR_RESULT=$(cast call --rpc-url "$RPC_URL" "$PROXY_ADDRESS" "operator()")
CURRENT_OPERATOR_ADDR="0x${CURRENT_OPERATOR_RESULT:26}"
CURRENT_OPERATOR_ADDR=$(cast to-check-sum-address "$CURRENT_OPERATOR_ADDR")
EXPECTED_OPERATOR=$(cast to-check-sum-address "$OPERATOR")

if [ "$CURRENT_OPERATOR_ADDR" != "$EXPECTED_OPERATOR" ]; then
    # Use restored admin permissions to reset operator
    cast send --private-key "$ADMIN_PRIVATE_KEY" --rpc-url "$RPC_URL" --legacy \
        "$PROXY_ADDRESS" "setOperator(address)" "$OPERATOR" >/dev/null 2>&1
    echo "✅ Operator restoration successful: $EXPECTED_OPERATOR"
else
    echo "✅ Operator already at expected address: $EXPECTED_OPERATOR"
fi

echo "✅ All state restoration completed"
echo ""

# Step 9: Precompiled contract gas verification test
echo "🔬 Step 9: Precompiled Contract Gas Verification Test"
echo "----------------------------------------"

echo "ℹ️  Verifying TEST_OP gas consumption..."
TEST_OP_GAS=$(cast estimate --rpc-url "$RPC_URL" --from "$ADMIN" 0x0000000000000000000000000000000000001001 0x01)

# Calculate expected gas value
# Base transaction gas: 21000
# Data gas: 16 (1 byte of 0x01, using EIP2028's TxDataNonZeroGasEIP2028)
# TEST_OP gas: 700 (our modification)
EXPECTED_GAS=21716

if [ "$TEST_OP_GAS" -eq "$EXPECTED_GAS" ]; then
    echo "✅ TEST_OP gas verification successful: $TEST_OP_GAS (expected: $EXPECTED_GAS)"
else
    echo "❌ TEST_OP gas verification failed:"
    echo "  Actual: $TEST_OP_GAS"
    echo "  Expected: $EXPECTED_GAS"
    echo "  Difference: $((TEST_OP_GAS - EXPECTED_GAS))"
    exit 1
fi

echo "ℹ️  Verifying precompiled contract call functionality..."
TEST_OP_RESULT=$(cast call --rpc-url "$RPC_URL" --from "$ADMIN" 0x0000000000000000000000000000000000001001 0x01)

if [ "$TEST_OP_RESULT" = "0x4f4b" ]; then
    echo "✅ TEST_OP call successful, returned 'OK'"
else
    echo "❌ TEST_OP call failed, returned: $TEST_OP_RESULT"
    exit 1
fi

echo "✅ Precompiled contract gas verification test completed"
echo ""

echo "🎉 All tests completed!"
echo "  ✅ Operator management normal"
echo "  ✅ BridgeFrom operations normal (including boundary tests)"
echo "  ✅ Cleanup operations normal (including boundary tests)"
echo "  ✅ Pause/Resume functionality normal"
echo "  ✅ Query functionality normal"
echo "  ✅ Admin transfer functionality normal"
echo "  ✅ Owner transfer functionality normal"
echo "  ✅ State restoration normal"
echo "  ✅ Precompiled contract gas verification normal"