#!/bin/bash

set -x
set -e
# This should be run in the root directory of the repo
ROOT_DIR=$(git rev-parse --show-toplevel)
TEST_DIR="$ROOT_DIR/test-pp-op"
TMP_DIR="$TEST_DIR/tmp"
SA_BENCH_DIR="$TMP_DIR/SA-Benchmark"
SCRIPTS_DIR="$TEST_DIR/scripts"

RPC_URL="ws://localhost:7546"

TIME_STAMP=$(date +%Y%m%d-%H%M%S)
RESULT_FILE="check-regenesis-result-$TIME_STAMP.txt"

source .env
TX_VALUE=10
GAS_PRICE=1000000000
if [ $# -gt 0 ] && [ "$1" == "mainnet" ]; then
    GAS_PRICE=1
fi

# Load nvm
export NVM_DIR="$HOME/.nvm"
[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"
#nvm install v22
nvm use v22
npm install -g yarn

if [ -d venv ]; then
    echo "env already created"
else
    echo "creating python env"
    python3 -m venv venv
fi

source venv/bin/activate
pip install web3 aiohttp tqdm

# 1. Run state-check state0
cd $ROOT_DIR
go install ./cmd/state-check/
cd $TEST_DIR
echo "*** State 0 ***" > $RESULT_FILE
state-check -dump-state-file config-op/state0.json -rpc-url $RPC_URL --progress-bar=false -connection-count=40 | tee $RESULT_FILE
#python ${SCRIPTS_DIR}/check_genesis.py --genesis ./config-op/state0.json --rpc http://localhost:8123 --batch-size 50

# 8. Run state-check state1
#cd $SA_BENCH_DIR
#PRIVATE_KEY=$(cat .env | grep "PRIVATE_KEY" | cut -d '=' -f 2)
##GAS_PRICE=$(cat .env | grep "GAS_PRICE" | cut -d '=' -f 2)
#cast send 0xa03666Fb51Aa9aD2DE70e0434072A007b3C91A9E --value $TX_VALUE \
#--private-key 0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2 \
#--legacy --gas-price $GAS_PRICE \
#--rpc-url $RPC_URL
#sleep 5
#cd $TEST_DIR
#echo -e "\n\n*** State 1 ***" >> $RESULT_FILE
#state-check -dump-state-file config-op/state1.json -rpc-url $RPC_URL --progress-bar=false | tee -a $RESULT_FILE

# 9. Run state-check state2
cd $SA_BENCH_DIR
TX_RESULT_FILE="$TEST_DIR/sa-tx-after.txt"
FEE_FILE="$TEST_DIR/tx-fee-after.txt"
yarn
yarn run senduop:deterministicop > $TX_RESULT_FILE
sleep 5
cd $TEST_DIR
scripts/calc-total-fee-and-value.sh $TX_RESULT_FILE > $FEE_FILE
echo -e "\n\n*** State 2 ***" >> $RESULT_FILE
state-check -dump-state-file config-op/state2.json -rpc-url $RPC_URL --progress-bar=false -connection-count=40 | tee -a $RESULT_FILE
#python ${SCRIPTS_DIR}/check_genesis.py --genesis ./config-op/state2.json --rpc http://localhost:8123 --batch-size 50
deactivate