#!/bin/bash
set -e
set -x

source .env

PWD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$PWD_DIR")"
TMP_DIR="$PWD_DIR/tmp"

sed_inplace() {
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

DEPLOYER_ADDRESS="0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534"
DEPLOYER_PRIVATE_KEY="0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2"
DEPLOYER_MNEMONIC="moment wine false celery win galaxy glide thumb tail setup choose city"
RICH_ADDRESS="0x14dC79964da2C08b23698B3D3cc7Ca32193d9955"
RICH_PRIVATE_KEY="0x4bbbf85ce3377467afe5d46f804f221813b2bb87f24d81f60f1fcdbf7cbf4356"

SEQ_ADDRESS="0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
SEQ_PRIVATE_KEY="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
TOKEN_ADDRESS="0x5FbDB2315678afecb367f032d93F642f64180aa3"


git checkout config/agglayer-config.toml
git checkout config/aggkit.toml
git checkout config/test.bridge.config.toml
git checkout config/test.erigon.seq.config.yaml
git checkout config/test.genesis.config.json
git checkout config/dynamic-mynetwork-allocs.json
git checkout config/dynamic-mynetwork-conf.json
git checkout config/first-batch-config.json

echo "Sending funds to deployer..."
cast send -f $RICH_ADDRESS --private-key $RICH_PRIVATE_KEY --value 3ether --legacy $DEPLOYER_ADDRESS

mkdir -p $TMP_DIR
cd $TMP_DIR
if [ ! -d "./aggkit" ]; then
  echo "Cloning contract repository..."
  git clone -b feature/0.1.0 https://github.com/okx/aggkit.git
  cd ./aggkit
  echo "Cleaning and resting contract repository..."
  git reset --hard; git checkout feature/0.1.0;git pull
  make build-docker
fi

cd $TMP_DIR
if [ ! -d "./xlayer-contracts" ]; then
  echo "Cloning contract repository..."
  git clone -b zjg/v11.0.0-rc.0-op-v1 https://github.com/okx/xlayer-contracts.git
fi

cd $TMP_DIR/xlayer-contracts
echo "Cleaning contract repository (selective)..."
rm -rf artifacts cache .openzeppelin node_modules
rm -f deployment/v2/deploy_output.json deployment/v2/create_rollup_output_*.json deployment/v2/genesis.json .env
# git checkout -- .

echo "Creating .env file..."
cat > .env << EOF
MNEMONIC="$DEPLOYER_MNEMONIC"
INFURA_PROJECT_ID="000"
ETHERSCAN_API_KEY="000"
EOF

cd deployment/v2

echo "Creating create_rollup_parameters.json..."
cat > create_rollup_parameters.json << EOF
{
    "adminZkEVM": "$DEPLOYER_ADDRESS",
    "chainID": 195,
    "consensusContract": "PolygonPessimisticConsensus",
    "dataAvailabilityProtocol": "PolygonDataCommittee",
    "deployerPvtKey": "",
    "description": "description",
    "forkID": 13,
    "gasTokenAddress":"0x5FbDB2315678afecb367f032d93F642f64180aa3",
    "maxFeePerGas": "",
    "maxPriorityFeePerGas": "",
    "multiplierGas": "",
    "networkName": "zkevm",
    "realVerifier": false,
    "trustedSequencer": "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
    "trustedSequencerURL": "http://xlayer-seq:8545",
    "trustedAggregator":"0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
    "programVKey": "0x00d6e4bdab9cac75a50d58262bb4e60b3107a6b61131ccdff649576c624b6fb7"
}
EOF

echo "Creating deploy_parameters.json..."
cat > deploy_parameters.json << EOF
{
    "admin": "$DEPLOYER_ADDRESS",
    "deployerPvtKey": "",
    "emergencyCouncilAddress": "$DEPLOYER_ADDRESS",
    "initialZkEVMDeployerOwner": "$DEPLOYER_ADDRESS",
    "maxFeePerGas": "",
    "maxPriorityFeePerGas": "",
    "minDelayTimelock": 60,
    "multiplierGas": "",
    "description": "description",
    "pendingStateTimeout": 604799,
    "polTokenAddress": "0x5FbDB2315678afecb367f032d93F642f64180aa3",
    "salt": "0x0000000000000000000000000000000000000000000000000000000000000001",
    "timelockAdminAddress": "$DEPLOYER_ADDRESS",
    "trustedSequencer": "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
    "trustedSequencerURL": "http://xlayer-seq:8545",
    "trustedAggregator": "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
    "trustedAggregatorTimeout": 604799,
    "forkID": 13,
    "test": true,
    "ppVKey": "0x00d6e4bdab9cac75a50d58262bb4e60b3107a6b61131ccdff649576c624b6fb7",
    "ppVKeySelector": "0x00000001",
    "realVerifier": false,
    "defaultAdminAddress": "$DEPLOYER_ADDRESS",
    "aggchainDefaultVKeyRoleAddress": "$DEPLOYER_ADDRESS",
    "addRouteRoleAddress": "$DEPLOYER_ADDRESS",
    "freezeRouteRoleAddress": "$DEPLOYER_ADDRESS",
    "zkEVMDeployerAddress": "$DEPLOYER_ADDRESS"
}
EOF

echo "Compiling contracts..."
cd ../../
npm i
npm run deploy:v2:localhost

cd "$ROOT_DIR"
mkdir -p $TMP_DIR/pp-deployed
ROLLUP_OUTPUT_PATH=$(find $TMP_DIR/xlayer-contracts/deployment/v2 -name "create_rollup_output_*.json" | sort -r | head -n 1)
cp -rf $ROLLUP_OUTPUT_PATH $TMP_DIR/pp-deployed/create_rollup_output.json
cp -rf $TMP_DIR/xlayer-contracts/deployment/v2/create_rollup_parameters.json $TMP_DIR/pp-deployed/
cp -rf $TMP_DIR/xlayer-contracts/deployment/v2/deploy_parameters.json $TMP_DIR/pp-deployed/
cp -rf $TMP_DIR/xlayer-contracts/deployment/v2/deploy_output.json $TMP_DIR/pp-deployed/
cp -rf $TMP_DIR/xlayer-contracts/deployment/v2/genesis.json $TMP_DIR/pp-deployed/
ROLLUP_OUTPUT_PATH="$TMP_DIR/pp-deployed/create_rollup_output.json"
DEPLOY_OUTPUT_PATH="$TMP_DIR/pp-deployed/deploy_output.json"

echo "Transferring ERC20 token to Sequencer..."
cast send --legacy --from $SEQ_ADDRESS --private-key $SEQ_PRIVATE_KEY $TOKEN_ADDRESS "transfer(address,uint256)" $SEQ_ADDRESS 1000

echo "Setting Trusted Sequencer URL..."
POE_ADDRESS=$(cat $ROLLUP_OUTPUT_PATH | grep -o '"rollupAddress": "[^"]*"' | cut -d'"' -f4)
BRIDGE_ADDRESS=$(cat $DEPLOY_OUTPUT_PATH | grep -o '"polygonZkEVMBridgeAddress": "[^"]*"' | cut -d'"' -f4)
GENESIS_VALUE=$(cat $ROLLUP_OUTPUT_PATH | grep -o '"genesis": "[^"]*"' | cut -d'"' -f4)
TIMESTAMP_VALUE=$(cat $ROLLUP_OUTPUT_PATH | grep -o '"timestamp": [0-9]*' | cut -d' ' -f2)
L1_FIRST_BLOCK=$(cat $DEPLOY_OUTPUT_PATH | grep -o '"upgradeToULxLyBlockNumber": [0-9]*' | cut -d' ' -f2)
L1_SECOND_BLOCK=$(cat $ROLLUP_OUTPUT_PATH | grep -o '"createRollupBlockNumber": [0-9]*' | cut -d' ' -f2)
ROLLUP_MANAGER_ADDRESS=$(grep -o '"polygonRollupManagerAddress": "[^"]*"' "$DEPLOY_OUTPUT_PATH" | cut -d'"' -f4)
GLOBAL_EXIT_ROOT_ADDRESS=$(grep -o '"polygonZkEVMGlobalExitRootAddress": "[^"]*"' "$DEPLOY_OUTPUT_PATH" | cut -d'"' -f4)
echo "Poe address from JSON: $POE_ADDRESS"
echo "Bridge address from JSON: $BRIDGE_ADDRESS"
echo "Genesis value from JSON: $GENESIS_VALUE"
echo "Timestamp value from JSON: $TIMESTAMP_VALUE"
echo "L1FirstBlock value from JSON: $L1_FIRST_BLOCK"
echo "L1SecondBlock value from JSON: $L1_SECOND_BLOCK"
echo "RollupManagerAddress value from JSON: $ROLLUP_MANAGER_ADDRESS"
echo "GlobalExitRootAddress value from JSON: $GLOBAL_EXIT_ROOT_ADDRESS"

echo "Using POE address from JSON: $POE_ADDRESS"
cast send --legacy --from $DEPLOYER_ADDRESS --private-key $DEPLOYER_PRIVATE_KEY $POE_ADDRESS "setTrustedSequencerURL(string)" "http://xlayer-rpc:8545"

cast send --legacy --from $DEPLOYER_ADDRESS --private-key $DEPLOYER_PRIVATE_KEY $BRIDGE_ADDRESS 'function bridgeAsset(uint32 destinationNetwork, address destinationAddress, uint256 amount, address token, bool forceUpdateGlobalExitRoot, bytes permitData) returns()' 7 0x0000000000000000000000000000000000000000 0 0x0000000000000000000000000000000000000000 true 0x

CONTAINER_ID=$(docker ps | grep zkevm-mock-l1-network | awk '{print $1}')
if [ -n "$CONTAINER_ID" ]; then
  echo "Entering container $CONTAINER_ID..."
  docker exec -it $CONTAINER_ID /bin/sh -c "ps -ef | grep geth; kill -15 \$(ps -ef | grep geth | grep -v grep | awk '{print \$1}')"
fi

echo "Generating configuration files..."
go install ./cmd/hack/allocs
which allocs
allocs $TMP_DIR/xlayer-contracts/deployment/v2/genesis.json
mv allocs.json $PWD_DIR/config/dynamic-mynetwork-allocs.json

cat > $PWD_DIR/config/dynamic-mynetwork-conf.json << EOF
{
  "root": "$GENESIS_VALUE",
  "timestamp": $TIMESTAMP_VALUE,
  "gasLimit": 0,
  "difficulty": 0
}
EOF
echo "dynamic-mynetwork-conf.json file updated"

echo "Updating test.erigon.seq.config.yaml file..."
CONFIG_FILE="./test-pp-op/config/test.erigon.seq.config.yaml"
sed_inplace "s|zkevm.address-zkevm: \"[^\"]*\"|zkevm.address-zkevm: \"$POE_ADDRESS\"|g" $CONFIG_FILE
sed_inplace "s|zkevm.address-rollup: \"[^\"]*\"|zkevm.address-rollup: \"$ROLLUP_MANAGER_ADDRESS\"|g" $CONFIG_FILE
sed_inplace "s|zkevm.address-ger-manager: \"[^\"]*\"|zkevm.address-ger-manager: \"$GLOBAL_EXIT_ROOT_ADDRESS\"|g" $CONFIG_FILE
sed_inplace "s|zkevm.l1-first-block: [0-9]*|zkevm.l1-first-block: $L1_FIRST_BLOCK|g" $CONFIG_FILE

mkdir -p "$PWD_DIR/config"
jq '.firstBatchData' "$ROLLUP_OUTPUT_PATH" > "$PWD_DIR/config/first-batch-config.json"
echo "Successfully exported firstBatchData to $PWD_DIR/config/first-batch-config.json"

echo "Updating parameter in aggkit.toml..."
CONFIG_FILE="./test-pp-op/config/aggkit.toml"
sed_inplace "s|polygonBridgeAddr = \"[^\"]*\"|polygonBridgeAddr = \"$BRIDGE_ADDRESS\"|" "$CONFIG_FILE"
sed_inplace "s|BridgeAddr = \"[^\"]*\"|BridgeAddr = \"$BRIDGE_ADDRESS\"|" "$CONFIG_FILE"
sed_inplace "s|BridgeAddrL2 = \"[^\"]*\"|BridgeAddrL2 = \"$BRIDGE_ADDRESS\"|" "$CONFIG_FILE"
sed_inplace "s|rollupCreationBlockNumber = \"[^\"]*\"|rollupCreationBlockNumber = \"$L1_FIRST_BLOCK\"|" "$CONFIG_FILE"
sed_inplace "s|rollupManagerCreationBlockNumber = \"[^\"]*\"|rollupManagerCreationBlockNumber = \"$L1_SECOND_BLOCK\"|" "$CONFIG_FILE"
sed_inplace "s|genesisBlockNumber = \"[^\"]*\"|genesisBlockNumber = \"$L1_FIRST_BLOCK\"|" "$CONFIG_FILE"
sed_inplace "s|polygonRollupManagerAddress = \"[^\"]*\"|polygonRollupManagerAddress = \"$ROLLUP_MANAGER_ADDRESS\"|" "$CONFIG_FILE"
sed_inplace "s|polygonZkEVMGlobalExitRootAddress = \"[^\"]*\"|polygonZkEVMGlobalExitRootAddress = \"$GLOBAL_EXIT_ROOT_ADDRESS\"|" "$CONFIG_FILE"
sed_inplace "s|polygonZkEVMAddress = \"[^\"]*\"|polygonZkEVMAddress = \"$POE_ADDRESS\"|" "$CONFIG_FILE"
echo "Successfully updated contract address parameters in aggkit.toml"

echo "Updating contract address parameters in agglayer-config.toml..."
AGGLAYER_CONFIG_FILE="./test-pp-op/config/agglayer-config.toml"
sed_inplace "s|rollup-manager-contract = \"[^\"]*\"|rollup-manager-contract = \"$ROLLUP_MANAGER_ADDRESS\"|" "$AGGLAYER_CONFIG_FILE"
sed_inplace "s|polygon-zkevm-global-exit-root-v2-contract = \"[^\"]*\"|polygon-zkevm-global-exit-root-v2-contract = \"$GLOBAL_EXIT_ROOT_ADDRESS\"|" "$AGGLAYER_CONFIG_FILE"
GENESIS_CONFIG_FILE="./test-pp-op/config/test.genesis.config.json"
sed_inplace "s|\"genesisBlockNumber\": [0-9]*|\"genesisBlockNumber\": $L1_FIRST_BLOCK|" "$GENESIS_CONFIG_FILE"
sed_inplace "s|\"rollupCreationBlockNumber\": [0-9]*|\"rollupCreationBlockNumber\": $L1_SECOND_BLOCK|" "$GENESIS_CONFIG_FILE"
sed_inplace "s|\"rollupManagerCreationBlockNumber\": [0-9]*|\"rollupManagerCreationBlockNumber\": $L1_FIRST_BLOCK|" "$GENESIS_CONFIG_FILE"
AGGLAYER_CONFIG_FILE="./test-pp-op/config/agglayer-config.toml"
sed_inplace "s|polygon-zkevm-global-exit-root-v2-contract = \"[^\"]*\"|polygon-zkevm-global-exit-root-v2-contract = \"$GLOBAL_EXIT_ROOT_ADDRESS\"|" "$AGGLAYER_CONFIG_FILE"

echo "Initialization script completed!"