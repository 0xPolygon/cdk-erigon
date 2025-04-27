#!/bin/bash

cd /app/kurtosis-cdk
/usr/local/bin/polycli loadtest --rpc-url "$(kurtosis port print cdk-v1 cdk-erigon-rpc-001 rpc)" --private-key "0x12d7de8621a77640c9241b2595ba78ce443d05e94090365ab3bb5e19df82c625" --verbosity 700 --requests 500 --rate-limit 50  --mode uniswapv3 --legacy

if [ $? -ne 0 ]; then
    mkdir -p /logs
    cd /logs
    cp /app/xlayer-erigon/logs/evm-rpc-tests.log .
    kurtosis service logs cdk-v1 cdk-erigon-rpc-001 --all > cdk-erigon-rpc-001.log
    kurtosis service logs cdk-v1 cdk-erigon-sequencer-001 --all > cdk-erigon-sequencer-001.log
fi