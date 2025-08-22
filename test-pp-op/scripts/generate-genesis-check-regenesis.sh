#!/bin/bash

# This should be run in the root directory of the repo
ROOT_DIR=$(git rev-parse --show-toplevel)
TEST_DIR="$ROOT_DIR/test-pp-op"

# 1. Generate genesis
cd $ROOT_DIR
go install ./cmd/hack/
cd $TEST_DIR
cp ./config-op/genesis.json ./config-op/genesis-op-raw.json
hack -action migrateGenesis -chaindata ./data_state0/seq/chaindata/ -input ./config-op/genesis-op-raw.json -output ./config-op/genesis.json
cp ./config-op/genesis.json ./config-op/state0.json

# 2. Generate state1.json
#hack -action migrateGenesis -chaindata ./data_state1/seq/chaindata/ -input ./config-op/genesis-op-raw.json -output ./config-op/state1.json

# 3. Generate state2.json
hack -action migrateGenesis -chaindata ./data_state2/seq/chaindata/ -input ./config-op/genesis-op-raw.json -output ./config-op/state2.json