#!/bin/bash
set -e
set -x

SA_BENCH_BRANCH="dumi/senddet"

# This should be run in the root directory of the repo
ROOT_DIR=$(git rev-parse --show-toplevel)
TEST_DIR="$ROOT_DIR/test-pp-op"
TMP_DIR="$TEST_DIR/tmp"
SA_BENCH_DIR="$TMP_DIR/SA-Benchmark"

SEQ_NAME="xlayer-seq"
SLEEP_TIME=5
DATA_DIR="data"
EXENV_FILE="example.env"
TX_VALUE=10
GAS_PRICE=1000000000
if [ $# -gt 0 ] && [ "$1" == "mainnet" ]; then
  SEQ_NAME="xlayer-mainnet-seq"
  SLEEP_TIME=30
  DATA_DIR="mainnet"
  EXENV_FILE="example-fm.env"
  GAS_PRICE=1
fi

# Clone relevant repos
function clone_repos {
    cd $TMP_DIR
    if [ ! -d $SA_BENCH_DIR ]; then
        git clone -b $SA_BENCH_BRANCH git@github.com:okx/SA-Benchmark.git
    fi
    cd $ROOT_DIR
}

clone_repos

function cleanup {
    cd $TEST_DIR
    rm -rf data_*
}

cleanup

# 1. Run SA-Benchmark setup only for state0
cd $SA_BENCH_DIR
git checkout $SA_BENCH_BRANCH
cp $EXENV_FILE .env
PRIVATE_KEY=$(cat .env | grep "PRIVATE_KEY" | cut -d '=' -f 2)
export NVM_DIR="$HOME/.nvm"
if ! [ -s "$NVM_DIR/nvm.sh" ]; then
    echo "nvm not found, installing..."
    curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash
fi
. "$NVM_DIR/nvm.sh"
# Try to use the required version, install if not available
if ! nvm use v22 2>/dev/null; then
    echo "Node.js v22 not found, installing..."
    nvm install 22
    nvm use v22
fi
./1-setup.sh
sleep 5
cd $TEST_DIR
docker compose stop $SEQ_NAME --timeout 600
cp -a -P $DATA_DIR data_state0

# 2. Send one tx and save state1.json
#cd $TEST_DIR
#docker compose start $SEQ_NAME
#sleep $SLEEP_TIME
#cast send 0xa03666Fb51Aa9aD2DE70e0434072A007b3C91A9E --value $TX_VALUE \
#--private-key $PRIVATE_KEY \
#--legacy --gas-price $GAS_PRICE \
#--rpc-url http://localhost:8123
#sleep 3
#cd $TEST_DIR
#docker compose stop $SEQ_NAME --timeout 600
#cp -a -P $DATA_DIR data_state1

# 3. Send deterministic tx and save state2.json
cd $TEST_DIR
docker compose start $SEQ_NAME
sleep $SLEEP_TIME
cd $SA_BENCH_DIR
git checkout $SA_BENCH_BRANCH
TX_RESULT_FILE="$TEST_DIR/sa-tx-before.txt"
FEE_FILE="$TEST_DIR/tx-fee-before.txt"
yarn run senduop:local > $TX_RESULT_FILE
sleep 5
cd $TEST_DIR
scripts/calc-total-fee-and-value.sh $TX_RESULT_FILE > $FEE_FILE
docker compose stop $SEQ_NAME --timeout 600
cp -a -P $DATA_DIR data_state2
