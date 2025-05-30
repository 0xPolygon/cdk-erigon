#!/bin/bash
set -e
set -x

PWD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$PWD_DIR")"

sed_inplace() {
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

ROLLUP_MANAGER_ADDRESS="0xE96dBF374555C6993618906629988d39184716B3"
ORIGINAL_ADMIN_ADDRESS="0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534"
ORIGINAL_ADMIN_PRIVATE_KEY="0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2"

DEPLOYER_ADDRESS="0x8943545177806ed17b9f23f0a21ee5948ecaa776"
DEPLOYER_PRIVATE_KEY="0xbcdf20249abf0ed6d944c0288fad489e33f66b3960d9e6229c1cd214ed3bbe31"
RICH_ADDRESS="0x14dC79964da2C08b23698B3D3cc7Ca32193d9955"
RICH_PRIVATE_KEY="0x4bbbf85ce3377467afe5d46f804f221813b2bb87f24d81f60f1fcdbf7cbf4356"

SEQ_ADDRESS="0x3b59731225d8567ad89343a4669509fa91ba37ee"
SEQ_PRIVATE_KEY="0xc1c2b957b72b8bfcbdd9ebee5c00c5c6e8b19ed7a7c9d714c6bc6abed15caee1"

echo "Sending funds to deployer..."
cast send -f $RICH_ADDRESS --private-key $RICH_PRIVATE_KEY --value 5ether --legacy $DEPLOYER_ADDRESS

if [ ! -d "./agglayer-contracts" ]; then
  echo "Cloning contract repository..."
  git clone -b v10.0.0-rc.6 https://github.com/agglayer/agglayer-contracts.git
fi

cd ./agglayer-contracts
echo "Cleaning and resting contract repository..."
rm -rf *; git reset --hard

cp ../contract/genesis.json ./deployment/v2/
cp ../contract/deploy_output.json ./deployment/v2/

echo "Creating .env file..."
cat > .env << EOF
MNEMONIC="$DEPLOYER_MNEMONIC"
INFURA_PROJECT_ID="000"
ETHERSCAN_API_KEY="000"
EOF

# Grant admin role to deployer address
echo "Granting admin role to deployer address..."
cast send $ROLLUP_MANAGER_ADDRESS "grantRole(bytes32,address)" 0x0000000000000000000000000000000000000000000000000000000000000000 $DEPLOYER_ADDRESS --private-key $ORIGINAL_ADMIN_PRIVATE_KEY --rpc-url http://127.0.0.1:8545 --chain 1337

cd deployment/v2

echo "Creating create_rollup_parameters.json..."
cat > create_rollup_parameters.json << EOF
{
    "adminZkEVM": "$DEPLOYER_ADDRESS",
    "chainID": 1101,
    "consensusContract": "PolygonPessimisticConsensus",
    "dataAvailabilityProtocol": "PolygonDataCommittee",
    "deployerPvtKey": "$DEPLOYER_PRIVATE_KEY",
    "description": "description",
    "forkID": 13,
    "gasTokenAddress":"",
    "maxFeePerGas": "",
    "maxPriorityFeePerGas": "",
    "multiplierGas": "",
    "networkName": "polygonzkevm",
    "realVerifier": false,
    "trustedSequencer": "0x3b59731225d8567ad89343a4669509fa91ba37ee",
    "trustedSequencerURL": "http://polygonzkevm-seq:8545",
    "trustedAggregator":"0xee93b9561399e406d09ae5c5b08fb291adec57f0",
    "programVKey": "0x00d6e4bdab9cac75a50d58262bb4e60b3107a6b61131ccdff649576c624b6fb7"
}
EOF

echo "Compiling contracts..."
npm i
# Deploy gas token
forge create \
    --broadcast \
    --json \
    --private-key "$DEPLOYER_PRIVATE_KEY" \
    contracts/mocks/ERC20PermitMock.sol:ERC20PermitMock \
    --constructor-args "CDK Gas Token" "CDK" "$DEPLOYER_ADDRESS" "1000000000000000000000000" \
    > gasToken-erc20.json
jq \
    --slurpfile c gasToken-erc20.json \
    '.gasTokenAddress = $c[0].deployedTo' \
    ./create_rollup_parameters.json > temp.json && \
    mv temp.json ./create_rollup_parameters.json
cd ../../
npx hardhat run deployment/v2/4_createRollup.ts --network localhost 2>&1 | tee 05_create_rollup.out

cd "$ROOT_DIR"
ROLLUP_OUTPUT_PATH=$(find ./test-pp/agglayer-contracts/deployment/v2 -name "create_rollup_output_*.json" | sort -r | head -n 1)
rm -rf ./test-pp/contract2/*
cp -rf $ROLLUP_OUTPUT_PATH ./test-pp/contract2/create_rollup_output.json
cp -rf ./test-pp/agglayer-contracts/deployment/v2/create_rollup_parameters.json ./test-pp/contract2/
cp -rf ./test-pp/agglayer-contracts/deployment/v2/deploy_output.json ./test-pp/contract2/
cp -rf ./test-pp/agglayer-contracts/deployment/v2/genesis.json ./test-pp/contract2/

ROLLUP_OUTPUT_PATH="./test-pp/contract2/create_rollup_output.json"
DEPLOY_OUTPUT_PATH="./test-pp/contract2/deploy_output.json"

echo "Transferring ERC20 token to Sequencer..."
TOKEN_ADDRESS=$(cat $ROLLUP_OUTPUT_PATH | grep -o '"gasTokenAddress": "[^"]*"' | cut -d'"' -f4)
cast send --legacy --from $DEPLOYER_ADDRESS --private-key $DEPLOYER_PRIVATE_KEY $TOKEN_ADDRESS "transfer(address,uint256)" $SEQ_ADDRESS 1000

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
cast send --legacy --from $DEPLOYER_ADDRESS --private-key $DEPLOYER_PRIVATE_KEY $POE_ADDRESS "setTrustedSequencerURL(string)" "http://polygonzkevm-rpc:8545"

cast send --legacy --from $DEPLOYER_ADDRESS --private-key $DEPLOYER_PRIVATE_KEY $BRIDGE_ADDRESS 'function bridgeAsset(uint32 destinationNetwork, address destinationAddress, uint256 amount, address token, bool forceUpdateGlobalExitRoot, bytes permitData) returns()' 7 0x0000000000000000000000000000000000000000 0 0x0000000000000000000000000000000000000000 true 0x

echo "Generating configuration files..."
go install ./cmd/hack/allocs
which allocs
jq '.genesis |= map(if .accountName == "deployer" then .address = "'"$DEPLOYER_ADDRESS"'" else . end)' ./test-pp/contract2/genesis.json > temp.json && mv temp.json ./test-pp/contract2/genesis.json
allocs ./test-pp/contract2/genesis.json
mv allocs.json ./test-pp/config/dynamic-polygonzkevm-allocs.json
chmod 644 ./test-pp/config/dynamic-polygonzkevm-allocs.json

cat > ./test-pp/config/dynamic-polygonzkevm-conf.json << EOF
{
  "root": "$GENESIS_VALUE",
  "timestamp": $TIMESTAMP_VALUE,
  "gasLimit": 0,
  "difficulty": 0
}
EOF
echo "dynamic-polygonzkevm-conf.json file updated"

echo "Updating test.erigon.polygonzkevm.seq.config.yaml file..."
CONFIG_FILE="./test-pp/config/test.erigon.polygonzkevm.seq.config.yaml"
sed_inplace "s|zkevm.address-zkevm: \"[^\"]*\"|zkevm.address-zkevm: \"$POE_ADDRESS\"|g" $CONFIG_FILE
sed_inplace "s|zkevm.address-rollup: \"[^\"]*\"|zkevm.address-rollup: \"$ROLLUP_MANAGER_ADDRESS\"|g" $CONFIG_FILE
sed_inplace "s|zkevm.address-ger-manager: \"[^\"]*\"|zkevm.address-ger-manager: \"$GLOBAL_EXIT_ROOT_ADDRESS\"|g" $CONFIG_FILE
sed_inplace "s|zkevm.l1-first-block: [0-9]*|zkevm.l1-first-block: $L1_FIRST_BLOCK|g" $CONFIG_FILE

mkdir -p "$PWD_DIR/config"
jq '.firstBatchData' "$ROLLUP_OUTPUT_PATH" > "$PWD_DIR/config/polygonzkevm-first-batch-config.json"
echo "Successfully exported firstBatchData to $PWD_DIR/config/polygonzkevm-first-batch-config.json"

echo "Updating polygonBridgeAddr parameter in cdk-node-config.toml..."
CONFIG_FILE="./test-pp/config/polygonzkevm-cdk-node-config.toml"
sed_inplace "s|polygonBridgeAddr = \"[^\"]*\"|polygonBridgeAddr = \"$BRIDGE_ADDRESS\"|" "$CONFIG_FILE"
CONFIG_FILE="./test-pp/config/polygonzkevm-cdk-node-config.toml"
sed_inplace "s|rollupCreationBlockNumber = \"[^\"]*\"|rollupCreationBlockNumber = \"$L1_FIRST_BLOCK\"|" "$CONFIG_FILE"
sed_inplace "s|rollupManagerCreationBlockNumber = \"[^\"]*\"|rollupManagerCreationBlockNumber = \"$L1_SECOND_BLOCK\"|" "$CONFIG_FILE"
sed_inplace "s|genesisBlockNumber = \"[^\"]*\"|genesisBlockNumber = \"$L1_FIRST_BLOCK\"|" "$CONFIG_FILE"
sed_inplace "s|polygonRollupManagerAddress = \"[^\"]*\"|polygonRollupManagerAddress = \"$ROLLUP_MANAGER_ADDRESS\"|" "$CONFIG_FILE"
sed_inplace "s|polygonZkEVMBridgeAddress = \"[^\"]*\"|polygonZkEVMBridgeAddress = \"$BRIDGE_ADDRESS\"|" "$CONFIG_FILE"
sed_inplace "s|polygonZkEVMGlobalExitRootAddress = \"[^\"]*\"|polygonZkEVMGlobalExitRootAddress = \"$GLOBAL_EXIT_ROOT_ADDRESS\"|" "$CONFIG_FILE"
sed_inplace "s|polygonZkEVMAddress = \"[^\"]*\"|polygonZkEVMAddress = \"$POE_ADDRESS\"|" "$CONFIG_FILE"
sed_inplace "s|polTokenAddress = \"[^\"]*\"|polTokenAddress = \"$TOKEN_ADDRESS\"|" "$CONFIG_FILE"

echo "Successfully updated contract address parameters in polygonzkevm-cdk-node-config.toml"

GENESIS_CONFIG_FILE="./test-pp/config/test.polygonzkevm.genesis.config.json"
sed_inplace "s|\"genesisBlockNumber\": [0-9]*|\"genesisBlockNumber\": $L1_FIRST_BLOCK|" "$GENESIS_CONFIG_FILE"
sed_inplace "s|\"rollupCreationBlockNumber\": [0-9]*|\"rollupCreationBlockNumber\": $L1_SECOND_BLOCK|" "$GENESIS_CONFIG_FILE"
sed_inplace "s|\"rollupManagerCreationBlockNumber\": [0-9]*|\"rollupManagerCreationBlockNumber\": $L1_FIRST_BLOCK|" "$GENESIS_CONFIG_FILE"
sed_inplace 's/"polygonZkEVMAddress": "[^"]*"/"polygonZkEVMAddress": "'"$POE_ADDRESS"'"/' ./test-pp/config/test.polygonzkevm.genesis.config.json
