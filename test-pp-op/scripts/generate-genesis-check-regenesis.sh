#!/bin/bash

# This should be run in the root directory of the repo
ROOT_DIR=$(git rev-parse --show-toplevel)
TEST_DIR="$ROOT_DIR/test-pp-op"

# 1. Generate genesis
cd $ROOT_DIR
go install ./cmd/hack/
cd $TEST_DIR
cp ./config-op/genesis.json ./config-op/genesis-op-raw.json
hack -action migrateGenesis -chaindata ./data_state0/seq/chaindata/ -input ./config-op/genesis-op-raw.json -output ./config-op/genesis.json -ignore-scalable
cp ./config-op/genesis.json ./config-op/state0.json

sed_inplace() {
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

ADDR="a03666fb51aa9ad2de70e0434072a007b3c91a9e"
CURRENT_BALANCE=$(jq -r ".alloc[\"$ADDR\"].balance" ./config-op/state0.json)
echo "Current balance: $CURRENT_BALANCE"

# NEW_BALANCE=$CURRENT_BALANCE + 10^24
NEW_BALANCE=$(python3 -c "print(hex($CURRENT_BALANCE + 1000000000000000000000000))")
echo "Adding: 10^24"
echo "New balance will be: $NEW_BALANCE"

# update balance to genesis file
sed_inplace '/'"$ADDR"'/,/balance/s/"balance": "0x[^"]*"/"balance": "'$NEW_BALANCE'"/' ./config-op/genesis.json

# 2. Generate state1.json
#hack -action migrateGenesis -chaindata ./data_state1/seq/chaindata/ -input ./config-op/genesis-op-raw.json -output ./config-op/state1.json

# 3. Generate state2.json
hack -action migrateGenesis -chaindata ./data_state2/seq/chaindata/ -input ./config-op/genesis-op-raw.json -output ./config-op/state2.json -ignore-scalable