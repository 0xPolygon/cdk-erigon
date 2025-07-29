#!/bin/bash

set -eu
# set -x

CONST_COUNT=10000000
BRIDGE_ADDRESS="0x4B24266C13AFEf2bb60e2C69A4C08A482d81e3CA"
ACCOUNT="0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534" 
PRIVATE_KEY="0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2"
BRIDGE_VALUE_BIG="10"  # 1 ETH in wei
BRIDGE_VALUE_SMALL="1"  # 0.1 ETH in wei

L1_ETH_ADDRESS="0x0000000000000000000000000000000000000000"
L2_WETH="0x95076baf95000f2e67b2f88998a26d82140308ca"

L1RPC=http://127.0.0.1:8545
L2RPC=http://127.0.0.1:8123

for ((i=0; i<$CONST_COUNT; i++)); do
    TX_HASH=$(cast send \
    --legacy \
    --json \
    --rpc-url $L1RPC \
    --private-key $PRIVATE_KEY \
    --value $BRIDGE_VALUE_BIG \
    $BRIDGE_ADDRESS \
    'function bridgeAsset(uint32 destinationNetwork, address destinationAddress, uint256 amount, address token, bool forceUpdateGlobalExitRoot, bytes permitData) returns(bytes32)' \
    1 $ACCOUNT $BRIDGE_VALUE_BIG $L1_ETH_ADDRESS true "0x" \
    | jq -r '.transactionHash')
    echo "Bridge from L1 to L2 tx hash: $TX_HASH, $i"

    if [ $i -eq 0 ]; then
        echo "!!!!!!!!!!!! Should sleep 120s for first bridge txs"
        sleep 120
    fi

    TX_HASH=$(cast send \
        --legacy \
        --private-key $PRIVATE_KEY \
        --rpc-url $L2RPC \
        --json \
        $BRIDGE_ADDRESS \
        'function bridgeAsset(uint32 destinationNetwork, address destinationAddress, uint256 amount, address token, bool forceUpdateGlobalExitRoot, bytes permitData) returns(bytes32)' \
        0 $ACCOUNT $BRIDGE_VALUE_SMALL $L2_WETH true "0x" \
        | jq -r '.transactionHash')
    echo "Bridge from L2 to L1 tx hash: $TX_HASH, $i"
done



