#!/bin/bash

# Start monitoring in background
cd /app/xlayer-erigon
bash ./.github/scripts/cpu_monitor.sh > cpu_monitor.log 2>&1 &
monitor_pid=$!

# Wait for 30 seconds
sleep 30

# Stop monitoring and get analysis
kill -TERM $monitor_pid
wait $monitor_pid || {
  echo "CPU usage exceeded threshold!"
  exit 1
}

# Monitor verified batches
cd /app/kurtosis-cdk
timeout 900s ./.github/scripts/monitor-verified-batches.sh --enclave cdk-v1 --rpc-url $(kurtosis port print cdk-v1 cdk-erigon-rpc-001 rpc) --target 20 --timeout 900

#  Set up envs
cd /app
kurtosis files download cdk-v1 bridge-config-artifact
export BRIDGE_ADDRESS="$(/usr/local/bin/yq '.NetworkConfig.PolygonBridgeAddress' bridge-config-artifact/bridge-config.toml)"
export ETH_RPC_URL="$(kurtosis port print cdk-v1 el-1-geth-lighthouse rpc)"
export BRIDGE_API_URL="$(kurtosis port print cdk-v1 zkevm-bridge-service-001 rpc)"
export L2_RPC_URL="$(kurtosis port print cdk-v1 cdk-erigon-rpc-001 rpc)"

# Clone bridge repository
git clone --recurse-submodules -j8 https://github.com/0xPolygonHermez/zkevm-bridge-service.git -b v0.6.0-RC10  bridge

# Build docker image
cd bridge
make build-docker-e2e-real_network

# Run test ERC20 Bridge
mkdir tmp
cat <<EOF > ./tmp/test.toml
TestL1AddrPrivate="0x12d7de8621a77640c9241b2595ba78ce443d05e94090365ab3bb5e19df82c625"
TestL2AddrPrivate="0x12d7de8621a77640c9241b2595ba78ce443d05e94090365ab3bb5e19df82c625"
[ConnectionConfig]
L1NodeURL="http://${ETH_RPC_URL}"
L2NodeURL="${L2_RPC_URL}"
BridgeURL="${BRIDGE_API_URL}"
L1BridgeAddr="${BRIDGE_ADDRESS}"
L2BridgeAddr="${BRIDGE_ADDRESS}"
EOF
docker run --network=host --volume "./tmp/:/config/" --env BRIDGE_TEST_CONFIG_FILE=/config/test.toml bridge-e2e-realnetwork-erc20
if [ $? -ne 0 ]; then
    echo "Test ERC20 Bridge failed"
    # copy logs to /logs
    mkdir -p /logs
    cd /logs
    cp /app/xlayer-erigon/logs/evm-rpc-tests.log .
    kurtosis service logs cdk-v1 cdk-erigon-rpc-001 --all > cdk-erigon-rpc-001.log
    kurtosis service logs cdk-v1 cdk-erigon-sequencer-001 --all > cdk-erigon-sequencer-001.log
    kurtosis service logs cdk-v1 zkevm-agglayer-001 --all > zkevm-agglayer-001.log
    kurtosis service logs cdk-v1 zkevm-prover-001 --all > zkevm-prover-001.log
    kurtosis service logs cdk-v1 cdk-node-001 --all > cdk-node-001.log
    kurtosis service logs cdk-v1 zkevm-bridge-service-001 --all > zkevm-bridge-service-001.log
fi