set -e
set -x

sed_inplace() {
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

source .env
source ./tools.sh

ROOT_DIR=$(git rev-parse --show-toplevel)
TEST_DIR="$ROOT_DIR/test-pp-op"

cd $TEST_DIR
if [ $CHECK_REGENESIS = "true" ]; then
  ./scripts/prepare-check-regenesis.sh $CHECK_TYPE
else
  docker stop xlayer-seq
fi

docker compose stop xlayer-rpc

docker compose stop xlayer-bridge-service
docker compose stop xlayer-bridge-ui
docker compose stop xlayer-agg-sender

docker compose stop xlayer-agglayer
docker compose stop xlayer-agglayer-prover

LOG_OUTPUT=$(docker logs xlayer-seq 2>&1)
echo "LOG_OUTPUT: $LOG_OUTPUT"

FORK_BLOCK=$(echo "$LOG_OUTPUT" | grep "Finish block" | tail -1 | sed -n 's/.*Finish block \([0-9]*\) with.*/\1/p')
echo "FORK_BLOCK=$FORK_BLOCK"
sed_inplace "s/FORK_BLOCK=.*/FORK_BLOCK=$FORK_BLOCK/" .env

PWD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$PWD_DIR")"
TMP_DIR="$PWD_DIR/tmp"


cd $PWD_DIR

source .env

# Deploy Transactor contract first
echo "🔧 Deploying Transactor contract..."
TRANSACTOR_DEPLOY_OUTPUT=$(docker run \
  --network "$DOCKER_NETWORK" \
  -v "$(pwd)/$CONFIG_DIR:/deployments" \
  -w /app \
  "${OP_CONTRACTS_IMAGE_TAG}" \
  bash -c "
    set -e
    cd /app/packages/contracts-bedrock
    cast send --rpc-url $L1_RPC_URL_IN_DOCKER --private-key $DEPLOYER_PRIVATE_KEY --create \"\$(forge inspect src/periphery/Transactor.sol:Transactor bytecode)\$(cast abi-encode 'constructor(address)' $ADMIN_OWNER_ADDRESS | sed 's/0x//')\" --json
  ")

# Extract contract address from deployment output
TRANSACTOR_ADDRESS=$(echo "$TRANSACTOR_DEPLOY_OUTPUT" | jq -r '.contractAddress // empty')
if [ -z "$TRANSACTOR_ADDRESS" ] || [ "$TRANSACTOR_ADDRESS" = "null" ]; then
  echo "❌ Failed to extract Transactor contract address from deployment output"
  echo "Deployment output: $TRANSACTOR_DEPLOY_OUTPUT"
  exit 1
fi

echo "✅ Transactor contract deployed at: $TRANSACTOR_ADDRESS"

# Update .env file with Transactor address
sed_inplace "s/TRANSACTOR=.*/TRANSACTOR=$TRANSACTOR_ADDRESS/" .env
source .env
echo "✅ Updated TRANSACTOR address in .env: $TRANSACTOR_ADDRESS"

echo "🔧 Bootstrapping superchain with op-deployer..."

docker run \
  --network "$DOCKER_NETWORK" \
  -v "$(pwd)/$CONFIG_DIR:/deployments" \
  -w /app \
  "${OP_CONTRACTS_IMAGE_TAG}" \
  bash -c "
    set -e
    /app/op-deployer/bin/op-deployer bootstrap superchain \
      --l1-rpc-url $L1_RPC_URL_IN_DOCKER \
      --private-key $DEPLOYER_PRIVATE_KEY \
      --artifacts-locator file:///app/packages/contracts-bedrock/forge-artifacts \
      --superchain-proxy-admin-owner $TRANSACTOR_ADDRESS \
      --protocol-versions-owner $ADMIN_OWNER_ADDRESS \
      --guardian $ADMIN_OWNER_ADDRESS \
      --outfile /deployments/superchain.json
  "

echo "🔧 Bootstrapping implementations with op-deployer..."

SUPERCHAIN_JSON="$CONFIG_DIR/superchain.json"
PROTOCOL_VERSIONS_PROXY=$(jq -r '.protocolVersionsProxyAddress' "$SUPERCHAIN_JSON")
SUPERCHAIN_CONFIG_PROXY=$(jq -r '.superchainConfigProxyAddress' "$SUPERCHAIN_JSON")
PROXY_ADMIN=$(jq -r '.proxyAdminAddress' "$SUPERCHAIN_JSON")

docker run \
  --network "$DOCKER_NETWORK" \
  -v "$(pwd)/$CONFIG_DIR:/deployments" \
  -w /app \
  "${OP_CONTRACTS_IMAGE_TAG}" \
  bash -c "
    set -e
    /app/op-deployer/bin/op-deployer bootstrap implementations \
      --artifacts-locator file:///app/packages/contracts-bedrock/forge-artifacts \
      --l1-rpc-url $L1_RPC_URL_IN_DOCKER \
      --outfile /deployments/implementations.json \
      --mips-version "7" \
      --private-key $DEPLOYER_PRIVATE_KEY \
      --protocol-versions-proxy $PROTOCOL_VERSIONS_PROXY \
      --superchain-config-proxy $SUPERCHAIN_CONFIG_PROXY \
      --superchain-proxy-admin $PROXY_ADMIN \
      --upgrade-controller $ADMIN_OWNER_ADDRESS \
      --challenge-period-seconds $CHALLENGE_PERIOD_SECONDS \
      --withdrawal-delay-seconds $WITHDRAWAL_DELAY_SECONDS \
      --proof-maturity-delay-seconds $WITHDRAWAL_DELAY_SECONDS \
      --dispute-game-finality-delay-seconds $DISPUTE_GAME_FINALITY_DELAY_SECONDS
  "

cp ./config-op/intent.toml.bak ./config-op/intent.toml
cp ./config-op/state.json.bak ./config-op/state.json

# Update intent.toml with Transactor address for l1ProxyAdminOwner
sed_inplace "s/l1ProxyAdminOwner = \".*\"/l1ProxyAdminOwner = \"$TRANSACTOR_ADDRESS\"/" ./config-op/intent.toml
echo "✅ Updated l1ProxyAdminOwner in intent.toml: $TRANSACTOR_ADDRESS"

# Read opcmAddress from implementations.json and write it into intent.toml
OPCM_ADDRESS=$(jq -r '.opcmAddress' ./config-op/implementations.json)
if [ -z "$OPCM_ADDRESS" ] || [ "$OPCM_ADDRESS" = "null" ]; then
  echo "❌ Failed to read opcmAddress from implementations.json"
  exit 1
fi

# Replace the opcmAddress field in intent.toml with the new value
sed_inplace "s/^opcmAddress = \".*\"/opcmAddress = \"$OPCM_ADDRESS\"/" ./config-op/intent.toml
echo "✅ Updated opcmAddress ($OPCM_ADDRESS) in intent.toml"

# deploy contracts, TODO, should we need to modify source code to deploy contracts?
docker run \
  --network "$DOCKER_NETWORK" \
  -v "$(pwd)/$CONFIG_DIR:/deployments" \
  -w /app \
  "${OP_CONTRACTS_IMAGE_TAG}" \
  bash -c "
    set -e
    echo '🔧 Starting contract deployment with op-deployer...'

    # Deploy using op-deployer, wait for completion before proceeding
    /app/op-deployer/bin/op-deployer apply \
      --workdir /deployments \
      --private-key $DEPLOYER_PRIVATE_KEY \
      --l1-rpc-url $L1_RPC_URL_IN_DOCKER

    echo '📄 Generating L2 genesis and rollup config...'

    # Generate L2 genesis using op-deployer
    /app/op-deployer/bin/op-deployer inspect genesis \
      --workdir /deployments \
      195 > /deployments/genesis.json

    # Generate L2 rollup using op-node
    /app/op-deployer/bin/op-deployer inspect rollup \
      --workdir /deployments \
      195 > /deployments/rollup.json

    echo '✅ Contract deployment completed successfully'
  "

echo "genesis.json and rollup.json are generated in deployments folder"

DATA_DIR="data"
if [ $CHECK_TYPE = "mainnet" ]; then
  DATA_DIR="mainnet"
fi

if [ $CHECK_REGENESIS = "true" ]; then
  ./scripts/generate-genesis-check-regenesis.sh
else
  # regenerate genesis.json for op-geth
  cp ./config-op/genesis.json ./config-op/genesis-op-raw.json

  # Try to build hack tool locally first, fall back to Docker if it fails
  echo "🔧 Building hack tool..."
  cd $ROOT_DIR

  if go install ./cmd/hack/ 2>/dev/null; then
      echo "✅ hack tool built successfully"
      cd $PWD_DIR
      hack -action migrateGenesis -chaindata ./${DATA_DIR}/seq/chaindata/ -input ./config-op/genesis-op-raw.json -output ./config-op/genesis.json
  else
      echo "❌ Local build failed, using Docker fallback..."
      cd $PWD_DIR

      # Build Docker image for hack tool
      echo "📦 Building hack tool Docker image..."
      cat > Dockerfile-hack << 'EOF'
FROM golang:1.24

RUN apt-get update && apt-get install -y git build-essential && apt-get clean

WORKDIR /app
COPY . .
RUN go build -o hack ./cmd/hack

CMD ["./hack"]
EOF

      cd $ROOT_DIR
      docker build -f $PWD_DIR/Dockerfile-hack -t hack-tool:latest .

      # Run hack tool in Docker
      cd $PWD_DIR
      docker run --rm \
          -v "$(pwd)/${DATA_DIR}/seq/chaindata:/chaindata:rw" \
          -v "$(pwd)/config-op:/config:rw" \
          hack-tool:latest \
          ./hack -action migrateGenesis \
          -chaindata /chaindata \
          -input /config/genesis-op-raw.json \
          -output /config/genesis.json

      # Clean up
      rm -f Dockerfile-hack
      echo "✅ hack tool completed via Docker"
  fi
fi

FORK_BLOCK_HEX=$(printf "0x%x" "$FORK_BLOCK")
cp ./config-op/genesis.json ./config-op/genesis-op-before-number.json
sed_inplace 's/"number": "0x0"/"number": "'"$FORK_BLOCK_HEX"'"/' ./config-op/genesis.json
sed_inplace 's/"number": 0/"number": '"$FORK_BLOCK"'/' ./config-op/rollup.json

# Extract contract addresses from state.json and update .env file
echo "🔧 Extracting contract addresses from state.json..."
STATE_JSON="$PWD_DIR/config-op/state.json"

if [ -f "$STATE_JSON" ]; then
    # Extract contract addresses from state.json
    DEPLOYMENTS_TYPE=$(jq -r 'type' "$STATE_JSON")
    if [ "$DEPLOYMENTS_TYPE" = "object" ]; then
        OPCD_TYPE=$(jq -r '.opChainDeployments | type' "$STATE_JSON" 2>/dev/null)
        if [ "$OPCD_TYPE" = "object" ]; then
            DISPUTE_GAME_FACTORY_ADDRESS=$(jq -r '.opChainDeployments.DisputeGameFactoryProxy // empty' "$STATE_JSON")
            L2OO_ADDRESS=$(jq -r '.opChainDeployments.L2OutputOracleProxy // empty' "$STATE_JSON")
            OPCM_IMPL_ADDRESS=$(jq -r '.appliedIntent.opcmAddress // empty' "$STATE_JSON")
            SYSTEM_CONFIG_PROXY_ADDRESS=$(jq -r '.opChainDeployments.SystemConfigProxy // empty' "$STATE_JSON")
            PROXY_ADMIN=$(jq -r '.superchainContracts.SuperchainProxyAdminImpl // empty' "$STATE_JSON")
        elif [ "$OPCD_TYPE" = "array" ]; then
            DISPUTE_GAME_FACTORY_ADDRESS=$(jq -r '.opChainDeployments[0].DisputeGameFactoryProxy // empty' "$STATE_JSON")
            L2OO_ADDRESS=$(jq -r '.opChainDeployments[0].L2OutputOracleProxy // empty' "$STATE_JSON")
            OPCM_IMPL_ADDRESS=$(jq -r '.appliedIntent.opcmAddress // empty' "$STATE_JSON")
            SYSTEM_CONFIG_PROXY_ADDRESS=$(jq -r '.opChainDeployments[0].SystemConfigProxy // empty' "$STATE_JSON")
            PROXY_ADMIN=$(jq -r '.superchainContracts.SuperchainProxyAdminImpl // empty' "$STATE_JSON")
        else
            DISPUTE_GAME_FACTORY_ADDRESS=""
            L2OO_ADDRESS=""
            OPCM_IMPL_ADDRESS=""
            SYSTEM_CONFIG_PROXY_ADDRESS=""
            PROXY_ADMIN=""
        fi

        # Update .env if found
        if [ -n "$DISPUTE_GAME_FACTORY_ADDRESS" ]; then
            echo "✅ Found DisputeGameFactoryProxy address: $DISPUTE_GAME_FACTORY_ADDRESS"
            sed_inplace "s/DISPUTE_GAME_FACTORY_ADDRESS=.*/DISPUTE_GAME_FACTORY_ADDRESS=$DISPUTE_GAME_FACTORY_ADDRESS/" .env
        else
            echo "⚠️  DisputeGameFactoryProxy address not found in opChainDeployments"
        fi

        if [ -n "$L2OO_ADDRESS" ]; then
            echo "✅ Found L2OutputOracleProxy address: $L2OO_ADDRESS"
            sed_inplace "s/L2OO_ADDRESS=.*/L2OO_ADDRESS=$L2OO_ADDRESS/" .env
        else
            echo "⚠️  L2OutputOracleProxy address not found in opChainDeployments"
        fi

        if [ -n "$OPCM_IMPL_ADDRESS" ]; then
            echo "✅ Found opcmAddress address: $OPCM_IMPL_ADDRESS"
            sed_inplace "s/OPCM_IMPL_ADDRESS=.*/OPCM_IMPL_ADDRESS=$OPCM_IMPL_ADDRESS/" .env
        else
            echo "⚠️  opcmAddress address not found in opChainDeployments"
        fi

        if [ -n "$SYSTEM_CONFIG_PROXY_ADDRESS" ]; then
            echo "✅ Found SystemConfigProxy address: $SYSTEM_CONFIG_PROXY_ADDRESS"
            sed_inplace "s/SYSTEM_CONFIG_PROXY_ADDRESS=.*/SYSTEM_CONFIG_PROXY_ADDRESS=$SYSTEM_CONFIG_PROXY_ADDRESS/" .env
        else
            echo "⚠️  SystemConfigProxy address not found in opChainDeployments"
        fi

        if [ -n "$PROXY_ADMIN" ]; then
            echo "✅ Found ProxyAdmin address: $PROXY_ADMIN"
            sed_inplace "s/PROXY_ADMIN=.*/PROXY_ADMIN=$PROXY_ADMIN/" .env
        else
            echo "⚠️  ProxyAdmin address not found in opChainDeployments"
        fi

        # Show summary
        echo "📄 Contract addresses updated in .env:"
        echo "   DISPUTE_GAME_FACTORY_ADDRESS=$DISPUTE_GAME_FACTORY_ADDRESS"
        echo "   L2OO_ADDRESS=$L2OO_ADDRESS"
        echo "   OPCM_IMPL_ADDRESS=$OPCM_IMPL_ADDRESS"
        echo "   SYSTEM_CONFIG_PROXY_ADDRESS=$SYSTEM_CONFIG_PROXY_ADDRESS"
        echo "   PROXY_ADMIN=$PROXY_ADMIN"
    else
        echo "❌ $STATE_JSON is not a valid JSON object"
    fi
else
    echo "❌ state.json not found at $STATE_JSON"
fi

echo "🎉 OP Stack deployment preparation completed!"


if [[ "$OSTYPE" == "darwin"* ]]; then
    # macOS
    sed -i '' 's/"parentHash": "0x[0-9a-fA-F]\{64\}"/"parentHash": "'$PARENT_HASH'"/' config-op/genesis.json
else
    # Linux
    sed -i 's/"parentHash": "0x[0-9a-fA-F]\{64\}"/"parentHash": "'$PARENT_HASH'"/' config-op/genesis.json
fi

# init op-geth-seq and op-geth-rpc
OP_GETH_DATADIR="$(pwd)/data/op-geth-seq"
rm -rf "$OP_GETH_DATADIR"
mkdir -p "$OP_GETH_DATADIR"
docker compose run --no-deps \
  -v "$(pwd)/$CONFIG_DIR/genesis.json:/genesis.json" \
  op-geth-seq \
  --datadir "/datadir" \
  --gcmode=archive \
  --db.engine=$DB_ENGINE \
  --log.format json \
  init \
  --state.scheme=hash \
  /genesis.json 2>&1 | tee init.log

NEW_BLOCK_HASH=$(grep "Successfully wrote genesis state" init.log | jq -r .hash)
ROLLUP_CONTENT=$(jq ".genesis.l2.hash = \"$NEW_BLOCK_HASH\"" config-op/rollup.json)
echo $ROLLUP_CONTENT | jq > config-op/rollup.json

OP_GETH_DATADIR="$(pwd)/data/op-geth-rpc"
rm -rf "$OP_GETH_DATADIR"
mkdir -p "$OP_GETH_DATADIR"
docker compose run --no-deps \
  -v "$(pwd)/$CONFIG_DIR/genesis.json:/genesis.json" \
  op-geth-rpc \
  --datadir "/datadir" \
  --gcmode=archive \
  --db.engine=$DB_ENGINE \
  init \
  --state.scheme=hash \
  /genesis.json

echo "finished init op-geth-seq and op-geth-rpc"

# Ensure prestate files exist and devnetL1.json is consistent before deploying contracts
EXPORT_DIR="$PWD_DIR/data/cannon-data"
mkdir -p $EXPORT_DIR


$MD5SUM_CMD config-op/genesis.json
# genesis.json is too large to embed in go, so we compress it now and decompress it in go code
gzip -c config-op/genesis.json > config-op/genesis.gz.json

if [ "$DOCKER_TYPE" = "rootless" ]; then
docker run --rm --privileged \
    -v "$(pwd)/scripts:/scripts" \
    -v "$(pwd)/config-op/rollup.json:/app/op-program/chainconfig/configs/195-rollup.json" \
    -v "$(pwd)/config-op/genesis.gz.json:/app/op-program/chainconfig/configs/195-genesis-l2.json" \
    -v "$EXPORT_DIR:/app/op-program/bin" \
    -w /app \
    --network "${DOCKER_NETWORK}" \
    "${OP_STACK_IMAGE_TAG}" \
    bash -c "
        echo '📊 Verifying Docker connection:'
        /scripts/dind-install-start.sh
        docker --version
        docker ps --format 'table {{.Names}}\t{{.Status}}' | head -3

        echo '🚀 Running make reproducible-prestate...'
        make reproducible-prestate

        echo '📁 Checking contents of op-program/bin:'
        ls -la /app/op-program/bin/ || echo 'Directory is empty or does not exist'
    "
else
docker run --rm \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v "$(pwd)/config-op/rollup.json:/app/op-program/chainconfig/configs/195-rollup.json" \
    -v "$(pwd)/config-op/genesis.gz.json:/app/op-program/chainconfig/configs/195-genesis-l2.json" \
    -v "$EXPORT_DIR:/app/op-program/bin" \
    -w /app \
    --network "${DOCKER_NETWORK}" \
    -e DOCKER_HOST=unix:///var/run/docker.sock \
    "${OP_STACK_IMAGE_TAG}" \
    bash -c "
        echo '📊 Verifying Docker connection:'
        apt-get update
        apt-get install docker.io -y
        docker --version
        docker ps --format 'table {{.Names}}\t{{.Status}}' | head -3

        echo '🚀 Running make reproducible-prestate...'
        make reproducible-prestate

        echo '📁 Checking contents of op-program/bin:'
        ls -la /app/op-program/bin/ || echo 'Directory is empty or does not exist'
    "
fi