#!/bin/bash

set -e
# Enable debug output only if DEBUG=1 is set
if [ "${DEBUG:-0}" = "1" ]; then
    set -x
fi

# Load environment variables
source .env

echo "🎯 Starting Challenge Test..."

# Function to capitalize first letter (compatible across bash versions)
capitalize_first() {
    local word="$1"
    echo "$(echo "${word:0:1}" | tr 'a-z' 'A-Z')${word:1}"
}

# Function to get the depth of the latest claim using list-claims
get_latest_claim_depth() {
    local claims_output=$(docker run --rm \
        --network "$DOCKER_NETWORK" \
        -v "$(pwd)/data/cannon-data:/data" \
        -v "$(pwd)/config-op/rollup.json:/rollup.json" \
        -v "$(pwd)/config-op/genesis.json:/l2-genesis.json" \
        "${OP_STACK_IMAGE_TAG}" \
        /app/op-challenger/bin/op-challenger list-claims \
            --l1-eth-rpc=${L1_RPC_URL_IN_DOCKER} \
            --game-address=$LATEST_GAME_ADDRESS 2>/dev/null)
    
    # Extract the depth from the last claim line (skip header lines)
    local latest_depth=$(echo "$claims_output" | grep -E "^\s*[0-9]+" | tail -1 | awk '{print $4}')
    
    # Default to 0 if parsing failed
    if [ -z "$latest_depth" ] || ! [[ "$latest_depth" =~ ^[0-9]+$ ]]; then
        latest_depth=0
    fi
    
    echo "$latest_depth"
}

# Get the latest game address using op-challenger list-game
echo "1. Getting latest game address..."
GAME_LIST=$(docker run --rm \
  --network "$DOCKER_NETWORK" \
  -v "$(pwd)/data/cannon-data:/data" \
  -v "$(pwd)/config-op/rollup.json:/rollup.json" \
  -v "$(pwd)/config-op/genesis.json:/l2-genesis.json" \
  "${OP_STACK_IMAGE_TAG}" \
  /app/op-challenger/bin/op-challenger list-games \
    --l1-eth-rpc=${L1_RPC_URL_IN_DOCKER} \
    --game-factory-address=${DISPUTE_GAME_FACTORY_ADDRESS})

# Debug: Show the game list
echo "📋 Game list:"
echo "$GAME_LIST"
echo ""

# First try to find a game "In Progress"
IN_PROGRESS_GAME=$(echo "$GAME_LIST" | grep -E "^\s*[0-9]+" | grep "In Progress" | tail -1)
if [ -n "$IN_PROGRESS_GAME" ]; then
    echo "🔄 Found game in progress, using it for challenge"
    LATEST_GAME_LINE="$IN_PROGRESS_GAME"
    LATEST_GAME_ADDRESS=$(echo "$LATEST_GAME_LINE" | awk '{print $2}')
else
    echo "📝 No game in progress, using latest game"
    # Extract the latest game address from the second column of the last game entry
    LATEST_GAME_LINE=$(echo "$GAME_LIST" | grep -E "^\s*[0-9]+" | tail -1)
    LATEST_GAME_ADDRESS=$(echo "$LATEST_GAME_LINE" | awk '{print $2}')
fi

echo "🔍 Selected game line: $LATEST_GAME_LINE"
echo "🎮 Extracted address: $LATEST_GAME_ADDRESS"

if [ -z "$LATEST_GAME_ADDRESS" ] || [[ ! "$LATEST_GAME_ADDRESS" =~ ^0x[a-fA-F0-9]{40}$ ]]; then
    echo "❌ Failed to get valid game address: $LATEST_GAME_ADDRESS"
    echo "💡 Available games:"
    echo "$GAME_LIST" | grep -E "^\s*[0-9]+"
    exit 1
fi

echo "✅ Latest game address: $LATEST_GAME_ADDRESS"

# Get challenge start time from first claim's clock
echo "🕒 Getting challenge start time from first claim's clock..."

# Get claimData for the first claim (index 0)
FIRST_CLAIM_DATA=$(docker run --rm \
    --network "$DOCKER_NETWORK" \
    "${OP_STACK_IMAGE_TAG}" \
    cast call \
        --rpc-url ${L1_RPC_URL_IN_DOCKER} \
        $LATEST_GAME_ADDRESS \
        "claimData(uint256)" \
        0 2>/dev/null)

if [ -n "$FIRST_CLAIM_DATA" ] && [ "$FIRST_CLAIM_DATA" != "0x" ]; then
    # Extract the clock field from ClaimData struct
    # Struct layout: parentIndex(32B) + counteredBy(32B) + claimant(32B) + bond(32B) + claim(32B) + position(32B) + clock(32B)
    # Clock is the 7th field (0-indexed = 6), starts at position 6*64 = 384 characters
    RAW_HEX=$(echo "$FIRST_CLAIM_DATA" | sed 's/0x//')
    CLAIM_CLOCK_HEX=$(echo "$RAW_HEX" | cut -c385-448)
    
    # Convert hex clock to decimal epoch
    CHALLENGE_START_EPOCH=$(printf "%d" "0x$CLAIM_CLOCK_HEX" 2>/dev/null || echo "0")
    
    if [ "$CHALLENGE_START_EPOCH" -gt 0 ]; then
        # Convert epoch to human readable time
        if [[ "$OSTYPE" == "darwin"* ]]; then
            CHALLENGE_START_TIME=$(date -r $CHALLENGE_START_EPOCH 2>/dev/null || echo "Unknown time")
        else
            CHALLENGE_START_TIME=$(date -d "@$CHALLENGE_START_EPOCH" 2>/dev/null || echo "Unknown time")
        fi
        echo "✅ Challenge start time from claim 0 clock: $CHALLENGE_START_TIME (epoch: $CHALLENGE_START_EPOCH)"
        echo "   📊 Raw claim data length: ${#RAW_HEX} chars, clock hex: 0x$CLAIM_CLOCK_HEX"
    else
        echo "⚠️  Invalid clock value from claim 0 (0x$CLAIM_CLOCK_HEX), using current time as fallback"
        CHALLENGE_START_TIME=$(date)
        CHALLENGE_START_EPOCH=$(date +%s)
    fi
else
    echo "⚠️  Failed to get claimData for claim 0, using current time as fallback"
    CHALLENGE_START_TIME=$(date)
    CHALLENGE_START_EPOCH=$(date +%s)
fi

# Get game information
echo "📋 Getting game information..."
GAME_INFO=$(docker run --rm \
    --network "$DOCKER_NETWORK" \
    -v "$(pwd)/data/cannon-data:/data" \
    -v "$(pwd)/config-op/rollup.json:/rollup.json" \
    -v "$(pwd)/config-op/genesis.json:/l2-genesis.json" \
    "${OP_STACK_IMAGE_TAG}" \
    /app/op-challenger/bin/op-challenger list-claims \
        --l1-eth-rpc=${L1_RPC_URL_IN_DOCKER} \
        --game-address=$LATEST_GAME_ADDRESS 2>/dev/null | head -2)

echo "$GAME_INFO"

# Get initial depth
INITIAL_DEPTH=$(get_latest_claim_depth)
echo "🌳 Initial claim depth: $INITIAL_DEPTH"
echo "🎯 Target: Reach maximum depth (73) then wait for DEFENDER_WINS (status=2)"

echo "2. Starting move sequence..."

# Function to get the depth of a specific claim
get_claim_depth() {
    local claim_index=$1
    # Use list-claims to get the depth of the specific claim
    local claims_output=$(docker run --rm \
        --network "$DOCKER_NETWORK" \
        -v "$(pwd)/data/cannon-data:/data" \
        -v "$(pwd)/config-op/rollup.json:/rollup.json" \
        -v "$(pwd)/config-op/genesis.json:/l2-genesis.json" \
        "${OP_STACK_IMAGE_TAG}" \
        /app/op-challenger/bin/op-challenger list-claims \
            --l1-eth-rpc=${L1_RPC_URL_IN_DOCKER} \
            --game-address=$LATEST_GAME_ADDRESS 2>/dev/null)
    
    # Find the line with the specific claim index and extract depth
    local claim_depth=$(echo "$claims_output" | grep -E "^\s*$claim_index\s" | awk '{print $4}')
    
    # Default to 0 if parsing failed
    if [ -z "$claim_depth" ] || ! [[ "$claim_depth" =~ ^[0-9]+$ ]]; then
        claim_depth=0
    fi
    
    echo "$claim_depth"
}

# Function to generate claim based on parent claim depth
generate_claim() {
    local parent_index=$1
    local parent_depth=$(get_claim_depth $parent_index)
    local base_claim="0x35ac85f39df227892e62fd41961f98fdf09bcac8474b3b19e60bafec5ac762a6"
    
    if [ $parent_depth -eq 30 ]; then
        # For defending against claims at depth 30 (Split Depth), set first byte to 00
        echo "0x00ac85f39df227892e62fd41961f98fdf09bcac8474b3b19e60bafec5ac762a6"
    else
        echo "$base_claim"
    fi
}

# Function to get the claimant address of a specific claim
get_claimant() {
    local claim_index=$1
    # claimData returns a struct, claimant is the 3rd field (bytes 64-84 in the hex output)
    local raw_output=$(docker run --rm \
        --network "$DOCKER_NETWORK" \
        "${OP_STACK_IMAGE_TAG}" \
        cast call \
            --rpc-url ${L1_RPC_URL_IN_DOCKER} \
            $LATEST_GAME_ADDRESS \
            "claimData(uint256)" \
            $claim_index)
    
    # Extract claimant address from position 64-84 (32-byte aligned, so starts at byte 64)
    # The address is in the 3rd 32-byte slot, padded with zeros
    local hex_data=$(echo "$raw_output" | tr -d '\n' | sed 's/0x//')
    local claimant_padded=${hex_data:128:64}  # 3rd 32-byte slot (2*64 = 128 start position)
    local claimant="0x${claimant_padded: -40}"  # Last 40 chars (20 bytes) for address
    echo "$claimant"
}

# Verify challenger address matches the private key (for reference)
CHALLENGER_ADDRESS_FROM_KEY=$(docker run --rm \
    --network "$DOCKER_NETWORK" \
    "${OP_STACK_IMAGE_TAG}" \
    cast wallet address ${OP_CHALLENGER_PRIVATE_KEY})

echo "🎯 Challenger address: $CHALLENGER_ADDRESS_FROM_KEY"

# Move loop
SUCCESSFUL_MOVES=0    # Track only successful moves
ATTEMPT_COUNT=0       # Track total attempts
LAST_CLAIM_COUNT=0    # Track last seen claim count

while true; do  # Continue until max depth reached or game ends
    ATTEMPT_COUNT=$((ATTEMPT_COUNT + 1))
    
    # Check if the latest claim has reached max depth (73)
    LATEST_DEPTH=$(get_latest_claim_depth)
    echo "🔍 Attempt #$ATTEMPT_COUNT: Latest claim depth: $LATEST_DEPTH"
    
    if [ "$LATEST_DEPTH" -ge 73 ]; then
        echo "🏁 Maximum depth (73) reached! No more claims can be made."
        echo "   Latest depth: $LATEST_DEPTH"
        break
    fi
    
    # Get current claim count
    CURRENT_CLAIMS=$(docker run --rm \
        --network "$DOCKER_NETWORK" \
        "${OP_STACK_IMAGE_TAG}" \
        cast call \
            --rpc-url ${L1_RPC_URL_IN_DOCKER} \
            $LATEST_GAME_ADDRESS \
            "claimDataLen()")
    
    CURRENT_CLAIM_COUNT=$(printf "%d" $CURRENT_CLAIMS)
    
    echo "🔍 Current claims: $CURRENT_CLAIM_COUNT, Last seen: $LAST_CLAIM_COUNT, Successful moves: $SUCCESSFUL_MOVES"
    
    # Check if claim count increased and is odd (proposer made a new claim)
    if [ $CURRENT_CLAIM_COUNT -le $LAST_CLAIM_COUNT ]; then
        echo "⏳ No new claims, waiting..."
        sleep 10
        continue
    fi
    
    # Check if current claim count is odd (proposer's turn completed)
    if [ $((CURRENT_CLAIM_COUNT % 2)) -eq 0 ]; then
        echo "⏳ Claim count is even ($CURRENT_CLAIM_COUNT), waiting for proposer..."
        LAST_CLAIM_COUNT=$CURRENT_CLAIM_COUNT
        sleep 10
        continue
    fi
    
    echo "✅ New proposer claim detected! Claims: $LAST_CLAIM_COUNT -> $CURRENT_CLAIM_COUNT (odd)"
    
    # Target the latest claim (since claim count is odd, latest claim is from proposer)
    PROPOSER_CLAIM_INDEX=$((CURRENT_CLAIM_COUNT - 1))
    echo "   🎯 Will target latest claim at index $PROPOSER_CLAIM_INDEX"
    
    # Get the depth of the claim we're targeting
    PARENT_CLAIM_DEPTH=$(get_claim_depth $PROPOSER_CLAIM_INDEX)
    
    # Determine move type and generate claim based on parent claim depth
    if [ $PARENT_CLAIM_DEPTH -eq 30 ] || [ $PARENT_CLAIM_DEPTH -eq 28 ]; then
        MOVE_TYPE="--defend"
        MOVE_NAME="defending"
        CLAIM=$(generate_claim $PROPOSER_CLAIM_INDEX)
        echo "🔥 Special defend against claim at depth 30 (Split Depth) with modified claim: $CLAIM"
    else
        MOVE_TYPE="--attack"
        MOVE_NAME="attacking"
        CLAIM=$(generate_claim $PROPOSER_CLAIM_INDEX)
    fi
    
    echo "🗡️  $(capitalize_first "$MOVE_NAME") proposer claim #$PROPOSER_CLAIM_INDEX at depth $PARENT_CLAIM_DEPTH (attempt #$ATTEMPT_COUNT, successful: $SUCCESSFUL_MOVES)"
    echo "   Claim: $CLAIM"
    
    # Execute move (attack or defend)
    if docker run --rm \
        --network "$DOCKER_NETWORK" \
        -v "$(pwd)/data/cannon-data:/data" \
        -v "$(pwd)/config-op/rollup.json:/rollup.json" \
        -v "$(pwd)/config-op/genesis.json:/l2-genesis.json" \
        "${OP_STACK_IMAGE_TAG}" \
        /app/op-challenger/bin/op-challenger move \
            --l1-eth-rpc=${L1_RPC_URL_IN_DOCKER} \
            --game-address=$LATEST_GAME_ADDRESS \
            $MOVE_TYPE \
            --parent-index=$PROPOSER_CLAIM_INDEX \
            --claim=$CLAIM \
            --private-key=${OP_CHALLENGER_PRIVATE_KEY}; then
        
        # Move succeeded - increment successful count
        SUCCESSFUL_MOVES=$((SUCCESSFUL_MOVES + 1))
        LAST_CLAIM_COUNT=$CURRENT_CLAIM_COUNT
        echo "✅ Move successful! (#$SUCCESSFUL_MOVES successful moves total)"
        echo "   $(capitalize_first "$MOVE_NAME") proposer claim at index $PROPOSER_CLAIM_INDEX (parent depth: $PARENT_CLAIM_DEPTH, game depth: $LATEST_DEPTH)"
        
        if [ $PARENT_CLAIM_DEPTH -eq 30 ]; then
            echo "🎯 Completed special defend against claim at depth 30 (Split Depth)!"
        fi
        
        # Small delay before next check
        echo "⏳ Waiting for proposer to respond..."
        sleep 5
        
    else
        echo "❌ Move attempt #$ATTEMPT_COUNT failed (target claim: $PROPOSER_CLAIM_INDEX, mode: $MOVE_NAME)"
        
        # Update last seen claim count to avoid retry
        LAST_CLAIM_COUNT=$CURRENT_CLAIM_COUNT
        
        # Check if game has ended
        GAME_STATUS=$(docker run --rm \
            --network "$DOCKER_NETWORK" \
            "${OP_STACK_IMAGE_TAG}" \
            cast call \
                --rpc-url ${L1_RPC_URL_IN_DOCKER} \
                $LATEST_GAME_ADDRESS \
                "status()")
        
        STATUS_DECIMAL=$(printf "%d" $GAME_STATUS)
        
        if [ $STATUS_DECIMAL -ne 0 ]; then
            echo "🏁 Game ended during attacks with status: $STATUS_DECIMAL"
            break
        fi
        
        # Wait before next check
        sleep 5
    fi
done

echo "🏁 Challenge sequence completed!"
echo "   Total attempts: $ATTEMPT_COUNT"
echo "   Successful moves: $SUCCESSFUL_MOVES"
echo "   Latest claim depth: $LATEST_DEPTH"

# Get final claim count for summary
FINAL_CLAIMS=$(docker run --rm \
    --network "$DOCKER_NETWORK" \
    "${OP_STACK_IMAGE_TAG}" \
    cast call \
        --rpc-url ${L1_RPC_URL_IN_DOCKER} \
        $LATEST_GAME_ADDRESS \
        "claimDataLen()")

FINAL_CLAIM_COUNT=$(printf "%d" $FINAL_CLAIMS)

# Calculate recommended execution time (MAX_CLOCK_DURATION after challenge start)
# Get MAX_CLOCK_DURATION from environment, default to 1 hour (3600 seconds) if not set
CLOCK_DURATION_SECONDS=${MAX_CLOCK_DURATION:-3600}

# Get challenger duration for claims 71 and 72, take the maximum
echo "🔍 Getting challenger durations for claims 71 and 72..."

duration_71=$(docker run --rm \
    --network "$DOCKER_NETWORK" \
    "${OP_STACK_IMAGE_TAG}" \
    cast call \
        --rpc-url ${L1_RPC_URL_IN_DOCKER} \
        $LATEST_GAME_ADDRESS \
        "getChallengerDuration(uint256)" \
        71 2>/dev/null || echo "0")

duration_72=$(docker run --rm \
    --network "$DOCKER_NETWORK" \
    "${OP_STACK_IMAGE_TAG}" \
    cast call \
        --rpc-url ${L1_RPC_URL_IN_DOCKER} \
        $LATEST_GAME_ADDRESS \
        "getChallengerDuration(uint256)" \
        72 2>/dev/null || echo "0")

# Convert hex to decimal and find maximum
duration_71_dec=$(printf "%d" $duration_71 2>/dev/null || echo "0")
duration_72_dec=$(printf "%d" $duration_72 2>/dev/null || echo "0")

if [ $duration_71_dec -gt $duration_72_dec ]; then
    max_challenger_duration=$duration_71_dec
    max_claim=71
else
    max_challenger_duration=$duration_72_dec
    max_claim=72
fi

echo "📊 Challenger durations:"
echo "   Claim 71: $duration_71_dec seconds"
echo "   Claim 72: $duration_72_dec seconds"
echo "   Maximum: $max_challenger_duration seconds (claim $max_claim)"

# Calculate target time using epoch timestamp (more reliable)
# Total wait = MAX_CLOCK_DURATION + max challenger duration
TARGET_EPOCH=$((CHALLENGE_START_EPOCH + CLOCK_DURATION_SECONDS + max_challenger_duration))

# Convert back to human readable time
if command -v gdate >/dev/null 2>&1; then
    # GNU date (available via homebrew on macOS)
    RECOMMENDED_TIME=$(gdate -d "@$TARGET_EPOCH" 2>/dev/null || echo "Challenge start + $CLOCK_DURATION_MINUTES minutes")
elif [[ "$OSTYPE" == "darwin"* ]]; then
    # macOS date command
    RECOMMENDED_TIME=$(date -r $TARGET_EPOCH 2>/dev/null || echo "Challenge start + $CLOCK_DURATION_MINUTES minutes")
else
    # Linux date command
    RECOMMENDED_TIME=$(date -d "@$TARGET_EPOCH" 2>/dev/null || echo "Challenge start + $CLOCK_DURATION_MINUTES minutes")
fi

echo ""
echo "🎯 Game Resolution Required"
echo "============================================"
echo "The challenge phase has reached maximum depth ($LATEST_DEPTH)."
echo "Now you need to manually resolve the game to complete the process."
echo ""
echo "📋 Game Information:"
echo "   - Game Address: $LATEST_GAME_ADDRESS"
echo "   - Challenge Started: $CHALLENGE_START_TIME"
echo "   - Total Claims: $FINAL_CLAIM_COUNT"
echo "   - Successful Challenger Moves: $SUCCESSFUL_MOVES"
echo "   - Max Challenger Duration: $max_challenger_duration seconds (claim $max_claim)"
echo ""
echo "⏰ IMPORTANT: You must wait until the following time before running the game resolution:"
echo "   Wait until: $RECOMMENDED_TIME"
echo "   (Do NOT run the resolution before this time. This is a strict requirement to ensure all game mechanics have completed.)"
echo ""
echo "📊 Total Wait Time Calculation:"
echo "   Base time (MAX_CLOCK_DURATION): $((CLOCK_DURATION_SECONDS / 60)) minutes ($CLOCK_DURATION_SECONDS seconds)"
echo "   Additional challenger duration: $max_challenger_duration seconds"
echo "   Total additional wait: $((CLOCK_DURATION_SECONDS + max_challenger_duration)) seconds"
echo ""
echo "🚫 Do NOT run the resolution command before $RECOMMENDED_TIME, or it will fail."
echo ""
echo "🚀 After the recommended time, run the following command to resolve the game:"
echo "   ./13-resolve-game.sh $LATEST_GAME_ADDRESS"
echo ""
echo "📝 This command will:"
echo "   1. Resolve all claims from back to front (claimResolve)"
echo "   2. Call the main game resolve() function"
echo "   3. Verify the final status is DEFENDER_WINS (status=2)"
echo ""
exit 0

