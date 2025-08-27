set -e
set -x

sed_inplace() {
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

# Load environment variables early
source .env

PWD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$PWD_DIR")"

# Function to add game type via Transactor
add_game_type_via_transactor() {
    local GAME_TYPE=$1
    local IS_PERMISSIONED=$2
    local CLOCK_EXTENSION_VAL=$3
    local MAX_CLOCK_DURATION_VAL=$4
    local ABSOLUTE_PRESTATE_VAL=$5
    
    echo "=== Adding Game Type $GAME_TYPE via Transactor ==="
    echo "Game Type: $GAME_TYPE"
    echo "Is Permissioned: $IS_PERMISSIONED"
    echo "Clock Extension: $CLOCK_EXTENSION_VAL"
    echo "Max Clock Duration: $MAX_CLOCK_DURATION_VAL"
    echo ""
    
    docker run --rm \
      --network "$DOCKER_NETWORK" \
      -v "$(pwd)/$CONFIG_DIR:/deployments" \
      -w /app \
      "${OP_CONTRACTS_IMAGE_TAG}" \
      bash -c "
        set -e
        
        # Get addresses from environment
        RPC_URL=$L1_RPC_URL_IN_DOCKER
        TRANSACTOR_ADDRESS=$TRANSACTOR
        SENDER_ADDRESS=\$(cast wallet address --private-key $DEPLOYER_PRIVATE_KEY)
        PRIVATE_KEY=$DEPLOYER_PRIVATE_KEY
        
        # Get addresses from environment variables
        SYSTEM_CONFIG=$SYSTEM_CONFIG_PROXY_ADDRESS
        PROXY_ADMIN=$PROXY_ADMIN
        OPCM=$OPCM_IMPL_ADDRESS
        DISPUTE_GAME_FACTORY=\$(cast call --rpc-url \$RPC_URL \$SYSTEM_CONFIG 'disputeGameFactory()(address)')
        
        echo 'State JSON Path: '\$STATE_JSON_PATH
        echo 'Dispute Game Factory: '\$DISPUTE_GAME_FACTORY
        echo 'System Config: '\$SYSTEM_CONFIG
        echo 'Proxy Admin: '\$PROXY_ADMIN
        echo 'OPCM: '\$OPCM
        echo 'Transactor Address: '\$TRANSACTOR_ADDRESS
        echo 'RPC URL: '\$RPC_URL
        echo 'Sender Address: '\$SENDER_ADDRESS
        echo ''
        
        # Retrieve existing permissioned game implementation for parameters
        echo 'Retrieving permissioned game parameters...'
        PERMISSIONED_GAME=\$(cast call --rpc-url \$RPC_URL \$DISPUTE_GAME_FACTORY 'gameImpls(uint32)(address)' 1)
        echo 'Permissioned Game Implementation: '\$PERMISSIONED_GAME
        
        if [ \"\$PERMISSIONED_GAME\" == \"0x0000000000000000000000000000000000000000\" ]; then
            echo 'Error: No permissioned game found. Cannot retrieve parameters.'
            exit 1
        fi
        
        # Retrieve parameters from existing permissioned game
        ABSOLUTE_PRESTATE='$ABSOLUTE_PRESTATE_VAL'
        MAX_GAME_DEPTH=\$(cast call --rpc-url \$RPC_URL \$PERMISSIONED_GAME 'maxGameDepth()')
        SPLIT_DEPTH=\$(cast call --rpc-url \$RPC_URL \$PERMISSIONED_GAME 'splitDepth()')
        VM=\$(cast call --rpc-url \$RPC_URL \$PERMISSIONED_GAME 'vm()(address)')
        
        echo 'Retrieved parameters:'
        echo '  Absolute Prestate: '\$ABSOLUTE_PRESTATE
        echo '  Max Game Depth: '\$MAX_GAME_DEPTH
        echo '  Split Depth: '\$SPLIT_DEPTH
        echo '  Clock Extension: '$CLOCK_EXTENSION_VAL'
        echo '  Max Clock Duration: '$MAX_CLOCK_DURATION_VAL'
        echo '  VM: '\$VM
        echo ''
        
        # Set initial bond
        INITIAL_BOND='1000000000000000000'  # 1 ETH in wei
        
        # Create unique salt mixer
        SALT_MIXER='123'
        
        echo 'Creating addGameType calldata...'
        
        # Create calldata for addGameType function
        ADDGAMETYPE_CALLDATA=\$(cast calldata 'addGameType((string,address,address,address,uint32,bytes32,uint256,uint256,uint64,uint64,uint256,address,bool)[])' \
        \"[(\
        \\\"\$SALT_MIXER\\\",\
        \$SYSTEM_CONFIG,\
        \$PROXY_ADMIN,\
        0x0000000000000000000000000000000000000000,\
        $GAME_TYPE,\
        \$ABSOLUTE_PRESTATE,\
        \$MAX_GAME_DEPTH,\
        \$SPLIT_DEPTH,\
        $CLOCK_EXTENSION_VAL,\
        $MAX_CLOCK_DURATION_VAL,\
        \$INITIAL_BOND,\
        \$VM,\
        $IS_PERMISSIONED\
        )]\")
        
        echo 'AddGameType calldata: '\$ADDGAMETYPE_CALLDATA
        echo ''
        
        # Create calldata for Transactor's DELEGATECALL function
        echo 'Creating Transactor DELEGATECALL calldata...'
        TRANSACTOR_CALLDATA=\$(cast calldata 'DELEGATECALL(address,bytes)' \$OPCM \$ADDGAMETYPE_CALLDATA)
        
        echo 'Transactor calldata: '\$TRANSACTOR_CALLDATA
        echo ''
        
        # Execute the transaction through Transactor
        echo 'Executing transaction via Transactor...'
        echo 'Target: '\$TRANSACTOR_ADDRESS
        echo 'From: '\$SENDER_ADDRESS
        
        cast send \\
            --rpc-url \$RPC_URL \\
            --private-key \$PRIVATE_KEY \\
            --from \$SENDER_ADDRESS \\
            \$TRANSACTOR_ADDRESS \\
            \$TRANSACTOR_CALLDATA
        
        echo ''
        echo 'Transaction sent! Check the transaction hash above for confirmation.'
        echo ''
        
        # Verify the new game type was added
        echo 'Verifying new game type was added...'
        NEW_GAME_IMPL=\$(cast call --rpc-url \$RPC_URL \$DISPUTE_GAME_FACTORY 'gameImpls(uint32)(address)' $GAME_TYPE)
        
        if [ \"\$NEW_GAME_IMPL\" != \"0x0000000000000000000000000000000000000000\" ]; then
            echo '✅ Success! New game type $GAME_TYPE added.'
            echo 'Game Type $GAME_TYPE Implementation: '\$NEW_GAME_IMPL
        else
            echo '❌ Warning: Could not verify game type was added. Check transaction status.'
        fi
        
        echo '✅ AddGameType operations completed successfully'
      "
}

docker compose up -d op-batcher

sleep 10
# TODO, we need to reseach and fix it,  0 block hash mismatch
LOG_OUTPUT=$(docker compose logs op-seq 2>&1 | tail -20)
if echo "$LOG_OUTPUT" | grep -q "expected L2 genesis hash to match L2 block at genesis block number"; then
    CORRECT_HASH=$(echo "$LOG_OUTPUT" | grep "expected L2 genesis hash to match L2 block at genesis block number" | sed -n 's/.*genesis block number [0-9]*: \([0-9a-fx]*\) <>.*/\1/p' | head -1)
    if [ -n "$CORRECT_HASH" ]; then
        echo "Fixing genesis hash: $CORRECT_HASH"
        sed_inplace '/\"l2\":/,/}/ s/\"hash\": \"0x[a-fA-F0-9]*\"/\"hash\": \"'$CORRECT_HASH'\"/' ./config-op/rollup.json
        docker compose restart op-seq
    fi
fi

OP_SEQ_IP=$(docker inspect op-seq --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
export OP_SEQ_IP

docker compose up -d op-rpc

sleep 10

PWD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd $PWD_DIR
EXPORT_DIR="$PWD_DIR/data/cannon-data"
mkdir -p $EXPORT_DIR

echo "Adding game type to DisputeGameFactory via op-deployer..."

RPC_URL=http://127.0.0.1:8545

# Retrieve existing values from chain for reference
# Get permissioned game implementation
PERMISSIONED_GAME_RAW=$(cast call --rpc-url $RPC_URL $DISPUTE_GAME_FACTORY_ADDRESS "gameImpls(uint32)" 1)
# Convert 32-byte hex to 20-byte address (last 40 hex chars, with 0x prefix)
PERMISSIONED_GAME="0x${PERMISSIONED_GAME_RAW: -40}"

# Get prestate value from prestate-proof-mt64.json
ABSOLUTE_PRESTATE=$(jq -r '.pre' "$EXPORT_DIR/prestate-proof-mt64.json")
MAX_GAME_DEPTH=$(cast call --rpc-url $RPC_URL $PERMISSIONED_GAME "maxGameDepth()")
SPLIT_DEPTH=$(cast call --rpc-url $RPC_URL $PERMISSIONED_GAME "splitDepth()")
VM_RAW=$(cast call --rpc-url $RPC_URL $PERMISSIONED_GAME "vm()")
VM="0x${VM_RAW: -40}"
ANCHOR_STATE_REGISTRY=$(cast call --rpc-url $RPC_URL $PERMISSIONED_GAME "anchorStateRegistry()")
L2_CHAIN_ID=$(cast call --rpc-url $RPC_URL $PERMISSIONED_GAME "l2ChainId()")

# Call the function to add game type 1 (permissioned) via Transactor
add_game_type_via_transactor 1 true $TEMP_CLOCK_EXTENSION $TEMP_MAX_CLOCK_DURATION $ABSOLUTE_PRESTATE

export GAME_TYPE=1
export FORK_BLOCK=0
docker compose up -d op-proposer

echo "Waiting for op-proposer to create a game..."
GAME_CREATED=false
MAX_WAIT_TIME=600  # 10 minutes timeout
WAIT_COUNT=0

while [ "$GAME_CREATED" = false ] && [ $WAIT_COUNT -lt $MAX_WAIT_TIME ]; do
    # Check if a game was created by op-proposer
    GAME_COUNT=$(cast call --rpc-url $RPC_URL $DISPUTE_GAME_FACTORY_ADDRESS "gameCount()(uint256)")
    if [ "$GAME_COUNT" -gt 0 ]; then
        echo "✅ Game created! Game count: $GAME_COUNT"
        GAME_CREATED=true
    else
        echo "⏳ Waiting for game creation... ($WAIT_COUNT/$MAX_WAIT_TIME seconds)"
        sleep 1
        WAIT_COUNT=$((WAIT_COUNT + 1))
    fi
done

if [ "$GAME_CREATED" = false ]; then
    echo "❌ Timeout waiting for game creation"
    exit 1
fi

echo "🛑 Stopping op-proposer..."
docker compose stop op-proposer

echo "⏰ Sleeping for ($TEMP_MAX_CLOCK_DURATION seconds)..."
sleep $TEMP_MAX_CLOCK_DURATION

echo "🔧 Executing dispute resolution sequence using op-challenger..."

# Get the latest game address
LATEST_GAME_INDEX=$((GAME_COUNT - 1))
GAME_INFO=$(cast call --rpc-url $RPC_URL $DISPUTE_GAME_FACTORY_ADDRESS "gameAtIndex(uint256)(uint256,uint256,address)" $LATEST_GAME_INDEX)
# Extract the third value (address) from the returned tuple - address is the last 40 hex chars
GAME_ADDRESS="0x${GAME_INFO: -40}"

echo "Latest game address: $GAME_ADDRESS"

# Execute the dispute resolution sequence using op-challenger commands
echo "1. Resolving claim (0,0) using op-challenger..."
docker run --rm \
  --network "$DOCKER_NETWORK" \
  -v "$(pwd)/data/cannon-data:/data" \
  -v "$(pwd)/config-op/rollup.json:/rollup.json" \
  -v "$(pwd)/config-op/genesis.json:/l2-genesis.json" \
  "${OP_STACK_IMAGE_TAG}" \
  /app/op-challenger/bin/op-challenger resolve-claim \
    --l1-eth-rpc=${L1_RPC_URL_IN_DOCKER} \
    --private-key=${OP_CHALLENGER_PRIVATE_KEY} \
    --game-address=$GAME_ADDRESS \
    --claim=0

echo "2. Resolving game using op-challenger..."
docker run --rm \
  --network "$DOCKER_NETWORK" \
  -v "$(pwd)/data/cannon-data:/data" \
  -v "$(pwd)/config-op/rollup.json:/rollup.json" \
  -v "$(pwd)/config-op/genesis.json:/l2-genesis.json" \
  "${OP_STACK_IMAGE_TAG}" \
  /app/op-challenger/bin/op-challenger resolve \
    --l1-eth-rpc=${L1_RPC_URL_IN_DOCKER} \
    --private-key=${OP_CHALLENGER_PRIVATE_KEY} \
    --game-address=$GAME_ADDRESS

sleep $DISPUTE_GAME_FINALITY_DELAY_SECONDS

echo "3. Claiming credit for proposer using cast command..."
docker run --rm \
  --network "$DOCKER_NETWORK" \
  "${OP_STACK_IMAGE_TAG}" \
  cast send \
    --rpc-url ${L1_RPC_URL_IN_DOCKER} \
    --private-key ${OP_CHALLENGER_PRIVATE_KEY} \
    $GAME_ADDRESS \
    "claimCredit(address)" \
    $PROPOSER_ADDRESS

echo "✅ Dispute resolution sequence completed using op-challenger commands!"

# Retrieve existing values from chain for reference
# Get permissioned game implementation
PERMISSIONED_GAME_RAW=$(cast call --rpc-url $RPC_URL $DISPUTE_GAME_FACTORY_ADDRESS "gameImpls(uint32)" 1)
# Convert 32-byte hex to 20-byte address (last 40 hex chars, with 0x prefix)
PERMISSIONED_GAME="0x${PERMISSIONED_GAME_RAW: -40}"

ABSOLUTE_PRESTATE=$(cast call --rpc-url $RPC_URL $PERMISSIONED_GAME "absolutePrestate()")
ANCHOR_STATE_REGISTRY=$(cast call --rpc-url $RPC_URL $PERMISSIONED_GAME "anchorStateRegistry()")

# Call the function to add game type 0 (permissionless) via Transactor
add_game_type_via_transactor 0 false $CLOCK_EXTENSION $MAX_CLOCK_DURATION $ABSOLUTE_PRESTATE

docker run --rm \
  --network "$DOCKER_NETWORK" \
  -v "$(pwd)/$CONFIG_DIR:/deployments" \
  -w /app \
  "${OP_CONTRACTS_IMAGE_TAG}" \
  bash -c "
    set -e
    
    echo '📋 Gathering contract addresses and generating calldata...'
    DISPUTE_GAME_FACTORY_ADDR=\$(cast call --rpc-url $L1_RPC_URL_IN_DOCKER $SYSTEM_CONFIG_PROXY_ADDRESS 'disputeGameFactory()(address)')
    OPTIMISM_PORTAL_ADDR=\$(cast call --rpc-url $L1_RPC_URL_IN_DOCKER $SYSTEM_CONFIG_PROXY_ADDRESS 'optimismPortal()(address)')
    echo 'disputeGameFactory: '\$DISPUTE_GAME_FACTORY_ADDR
    echo 'optimismPortal: '\$OPTIMISM_PORTAL_ADDR
    
    # Get anchorStateRegistry address with proper return type specification
    ANCHOR_STATE_REGISTRY_ADDR=\$(cast call --rpc-url $L1_RPC_URL_IN_DOCKER \$OPTIMISM_PORTAL_ADDR 'anchorStateRegistry()(address)')
    echo 'anchorStateRegistry: '\$ANCHOR_STATE_REGISTRY_ADDR
    
    GAME_ADDR=\$(cast call --rpc-url $L1_RPC_URL_IN_DOCKER \$DISPUTE_GAME_FACTORY_ADDR 'gameImpls(uint32)(address)' 0)
    echo 'gameImpls(0): '\$GAME_ADDR
    
    cast send \$ANCHOR_STATE_REGISTRY_ADDR 'setRespectedGameType(uint32)' 0 --rpc-url $L1_RPC_URL_IN_DOCKER --private-key $DEPLOYER_PRIVATE_KEY

    echo '✅ setRespectedGameType completed successfully'
  "

export GAME_TYPE=0
source .env # source .env to update `FORK_BLOCK`

sleep $TEMP_GAME_WINDOW
docker compose up -d op-proposer op-challenger op-dispute-mon

if [ $CHECK_REGENESIS = "true" ]; then
  ./scripts/check-regenesis.sh
fi
