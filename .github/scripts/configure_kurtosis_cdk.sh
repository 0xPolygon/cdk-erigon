#!/bin/bash

# This script is used to set up the Kurtosis environment for testing
# It takes one argument (optional): database mode ("default" - not split, no async commit, "ac-split" - split with async commit)

cd /app/kurtosis-cdk
sed -i '/zkevm.sequencer-batch-seal-time:/d' templates/cdk-erigon/config.yml
sed -i '/zkevm.sequencer-non-empty-batch-seal-time:/d' templates/cdk-erigon/config.yml
sed -i '/zkevm\.sequencer-initial-fork-id/d' ./templates/cdk-erigon/config.yml
sed -i '/sentry.drop-useless-peers:/d' templates/cdk-erigon/config.yml
sed -i '/zkevm\.pool-manager-url/d' ./templates/cdk-erigon/config.yml
sed -i '/zkevm.l2-datastreamer-timeout:/d' templates/cdk-erigon/config.yml
if [ "$1" = "ac-split" ]; then
  echo "Will use ac-split"
  echo -e "\n"  >> templates/cdk-erigon/config.yml
  echo "zkevm.standalone-smt-db: true" >> templates/cdk-erigon/config.yml
  echo "zkevm.enable-async-commit: true" >> templates/cdk-erigon/config.yml
fi

echo 'args:' > params.yml
echo '  cdk_erigon_node_image: cdk-erigon:local' >> params.yml
echo '  el-1-geth-lighthouse: ethpandaops/lighthouse@sha256:4902d9e4a6b6b8d4c136ea54f0e51582a32f356f3dec7194a1adee13ed2d662e' >> params.yml
/usr/local/bin/yq -i '.args.data_availability_mode = "${{ matrix.da-mode }}"' params.yml
sed -i 's/"londonBlock": [0-9]\+/"londonBlock": 0/' ./templates/cdk-erigon/chainspec.json
sed -i 's/"normalcyBlock": [0-9]\+/"normalcyBlock": 0/' ./templates/cdk-erigon/chainspec.json
sed -i 's/"shanghaiTime": [0-9]\+/"shanghaiTime": 0/' ./templates/cdk-erigon/chainspec.json
sed -i 's/"cancunTime": [0-9]\+/"cancunTime": 0/' ./templates/cdk-erigon/chainspec.json
sed -i '/"terminalTotalDifficulty"/d' ./templates/cdk-erigon/chainspec.json