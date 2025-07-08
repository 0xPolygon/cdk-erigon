
set -e
set -x
source .env

L2_RPC_URL="http://127.0.0.1:8123"
DEPLOYER_ADDRESS="0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534"
DEPLOYER_PRIVATE_KEY="0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2"
ORACLE_ADDRESS="0x70997970c51812dc3a010c7d01b50e0d17dc79c8"
TIME_LOCK_ADDRESS="0x27284DBa79e6DF953Fbd232A9d8D87029F03BBf5"
GER_MANAGER_ADDRESS="0xa40d5f56745a118d0906a34e69aec8c0db1cb8fa"

sed_inplace() {
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

PWD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$PWD_DIR")"
TMP_DIR="$PWD_DIR/tmp"

# 1 upgrade the GER contract
cd $TMP_DIR/xlayer-contracts/

cd upgrade/upgradeToV2

echo "Creating upgrade_parameters.json..."
cat > upgrade_parameters.json << EOF
{   
    "timelockDelay": 70,
    "timelockSalt": "",
    "globalExitRootUpdater": "$ORACLE_ADDRESS",
    "globalExitRootRemover": "$ORACLE_ADDRESS"
} 
EOF

cp ../../deployment/v2/deploy_parameters.json ./deploy_parameters.json
cp ../../deployment/v2/deploy_output.json ./deploy_output.json

sed_inplace '1s/{/{\n "polygonZkEVMGlobalExitRootL2Address": "0xa40d5f56745a118d0906a34e69aec8c0db1cb8fa",/' deploy_output.json

cd ../../

hardhat_output=$(npm run upgradeL2GER:timelock:l2localhost)
echo "hardhat_output: $hardhat_output"

schedule_data=$(echo "$hardhat_output" | awk -F"'" '/scheduleData:/ {print $2}')
execute_data=$(echo "$hardhat_output" | awk -F"'" '/executeData:/ {print $2}')
echo "schedule_data: $schedule_data"
echo "execute_data: $execute_data"

cast send --legacy --rpc-url "$L2_RPC_URL" --private-key "$DEPLOYER_PRIVATE_KEY" "$TIME_LOCK_ADDRESS" "$schedule_data"
sleep 70
cast send --legacy --rpc-url "$L2_RPC_URL" --private-key "$DEPLOYER_PRIVATE_KEY" "$TIME_LOCK_ADDRESS" "$execute_data"
sleep 5
cast call --rpc-url "$L2_RPC_URL" $GER_MANAGER_ADDRESS 'GER_SOVEREIGN_VERSION()(string)'

cd $PWD_DIR

sed_inplace 's/http:\/\/xlayer-rpc:8545/http:\/\/op-geth:8545/' config/agglayer-config.toml
sed_inplace 's/http:\/\/xlayer-rpc:8545/http:\/\/op-geth:8545/' config/aggkit.toml
sed_inplace '/\[BridgeL2Sync\]/a\
InitialBlockNum = '$FORK_BLOCK'
' config/aggkit.toml
sed_inplace 's/http:\/\/xlayer-rpc:8545/http:\/\/op-geth:8545/' config/test.bridge.config.toml
sed_inplace 's/RequireSovereignChainSmcs = \[false\]/RequireSovereignChainSmcs = \[true\]/' config/test.bridge.config.toml
sed_inplace '/\[NetworkConfig\]/a\
L2GenBlockNumber = '$FORK_BLOCK'
' config/test.bridge.config.toml

while true; do
  sleep 10
  L2_BLOCK_HEIGHT=$(cast block-number --rpc-url $L2_RPC_URL)
  if [ "$L2_BLOCK_HEIGHT" -ge "$FORK_BLOCK" ]; then
    echo "L2 block height $L2_BLOCK_HEIGHT has reached fork block $FORK_BLOCK"
    sleep 10
    break
  fi
  echo "Waiting for L2 block height to reach $FORK_BLOCK (current: $L2_BLOCK_HEIGHT)"
done

docker-compose up -d xlayer-oracle
sleep 5
docker-compose up -d xlayer-agglayer-prover
docker-compose up -d xlayer-agglayer
docker-compose up -d xlayer-bridge-service
sleep 10
docker rm xlayer-bridge-ui
docker-compose up -d xlayer-bridge-ui
docker-compose up -d xlayer-agg-sender