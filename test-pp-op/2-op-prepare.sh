set -e
set -x

sed_inplace() {
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}


docker-compose stop xlayer-seq
docker-compose stop xlayer-rpc

docker-compose stop xlayer-bridge-service
docker-compose stop xlayer-bridge-ui
docker-compose stop xlayer-agg-sender

docker-compose stop xlayer-agglayer
docker-compose stop xlayer-agglayer-prover

LOG_OUTPUT=$(docker compose logs xlayer-seq 2>&1 | tail -100)
echo "LOG_OUTPUT: $LOG_OUTPUT"

FORK_BLOCK=$(echo "$LOG_OUTPUT" | grep "Finish block" | tail -1 | sed -n 's/.*Finish block \([0-9]*\) with.*/\1/p')
echo "FORK_BLOCK=$FORK_BLOCK"
sed_inplace "s/FORK_BLOCK=.*/FORK_BLOCK=$FORK_BLOCK/" .env

PWD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$PWD_DIR")"
TMP_DIR="$PWD_DIR/tmp"

cd $TMP_DIR

if [ ! -d "optimism" ]; then
    echo "Cloning Optimism repository..."
    git clone -b v1.9.3 https://github.com/ethereum-optimism/optimism.git
    cp $PWD_DIR/op-docker/Dockerfile-opstack optimism/Dockerfile
    cd optimism
    docker build -t op-stack:v1.9.3 .
    cd ..
fi

if [ ! -d "op-geth" ]; then
    echo "Cloning op-geth repository..."
    git clone -b v1.101511.0 https://github.com/ethereum-optimism/op-geth.git
    cp $PWD_DIR/op-docker/Dockerfile-opgeth op-geth/Dockerfile
    cd op-geth
    docker build -t op-geth:v1.101511.0 .
    cd ..
fi

cd $PWD_DIR

source .env

# deploy contracts, TODO, should we need to modify source code to deploy contracts?
docker run \
  --network "$DOCKER_NETWORK" \
  -v "$(pwd)/$CONFIG_DIR:/app/packages/contracts-bedrock/deployments" \
  -w /app/packages/contracts-bedrock \
  "${OP_STACK_IMAGE_TAG}" \
  bash -c "yes | DEPLOYMENT_OUTFILE=deployments/artifact.json DEPLOY_CONFIG_PATH=deployments/devnetL1.json forge script -vvv scripts/deploy/Deploy.s.sol:Deploy \
      --rpc-url $L1_RPC_URL_IN_DOCKER \
      --broadcast --private-key $DEPLOYER_PRIVATE_KEY --non-interactive && \
      FORK=latest STATE_DUMP_PATH=deployments/state_dump.json DEPLOY_CONFIG_PATH=deployments/devnetL1.json CONTRACT_ADDRESSES_PATH=deployments/artifact.json forge script scripts/L2Genesis.s.sol:L2Genesis --sig 'runWithStateDump()' &&\
      go run ../../op-node/cmd/main.go genesis l2 \
      --deploy-config=deployments/devnetL1.json \
      --l1-deployments=deployments/artifact.json \
      --l2-allocs=deployments/state_dump.json \
      --outfile.l2=deployments/genesis.json \
      --outfile.rollup=deployments/rollup.json \
      --l1-rpc=$L1_RPC_URL_IN_DOCKER"

echo "genesis.json and rollup.json are generated in deployments folder"

# regenerate genesis.json for op-geth
cd $ROOT_DIR
go install ./cmd/hack/
cd $PWD_DIR
cp ./config-op/genesis.json ./config-op/genesis-op-raw.json
hack -action migrateGenesis -chaindata ./data/seq/chaindata/ -input ./config-op/genesis-op-raw.json   -output ./config-op/genesis.json

# FORK_BLOCK_HEX=$(printf "0x%x" "$FORK_BLOCK")
# cp ./config-op/genesis.json ./config-op/genesis-op-before-number.json
# sed_inplace 's/"number": "0x0"/"number": "'"$FORK_BLOCK_HEX"'"/' ./config-op/genesis.json
# sed_inplace 's/"number": 0/"number": '"$FORK_BLOCK"'/' ./config-op/rollup.json

# init op-geth
OP_GETH_DATADIR="$(pwd)/data/op-geth"
rm -rf "$OP_GETH_DATADIR"
mkdir -p "$OP_GETH_DATADIR"
docker compose run --no-deps \
  -v "$(pwd)/$CONFIG_DIR/genesis.json:/genesis.json" \
  op-geth \
  --datadir "/datadir" \
  --gcmode=archive \
  init \
  --state.scheme=hash \
  /genesis.json

echo "finished init op-geth"

if ! command -v jq &> /dev/null; then
    echo "Warning: 'jq' is not installed. The op-proposer service will fail if you try to run it."
    echo "Please install jq (e.g., 'sudo apt-get install jq' or 'brew install jq')."
else
    # Get L2OutputOracleProxy address and update .env file
    L2OO_ADDRESS=$(jq -r .L2OutputOracleProxy "$(pwd)/$CONFIG_DIR/artifact.json")
    if [ -z "$L2OO_ADDRESS" ] || [ "$L2OO_ADDRESS" == "null" ]; then
        echo "Warning: L2OutputOracleProxy address not found in $CONFIG_DIR/artifact.json. op-proposer will fail if started."
    else
        echo "L2OutputOracleProxy address set for op-proposer: $L2OO_ADDRESS"
        sed_inplace "s/L2OO_ADDRESS=.*/L2OO_ADDRESS=$L2OO_ADDRESS/" .env
        export L2OO_ADDRESS
    fi
fi

source .env