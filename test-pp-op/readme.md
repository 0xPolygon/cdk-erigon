# How to run
```shell
cd test-pp-op;
# only one stop
./0-all.sh

# or step by step
./1-pp-setup.sh
./2-op-prepare.sh
./3-op-start-service.sh
./4-pp-bridge-start.sh


cast send -f 0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534  --private-key 0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2 --value 0.01ether 0xA6f7A6b2E9B4d41C582D4Aaf907F45321e2Ca847 --legacy --rpc-url http://127.0.0.1:8124
```

# How to use bridge
```
http://127.0.0.1:8090/
L1 OKB Token: 0x5FbDB2315678afecb367f032d93F642f64180aa3
L2 WETH Token: 0xd80e5a44dc9628fae9b432eac67873238504ea29
L2 admin: 0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534

```

# check with RPC
```
cd xlayer-erigon/test-pp-op
# for testnet: add the following line to .env
CHECK_REGENESIS="true"
# for mainnet: add the following 2 lines to .env
CHECK_REGENESIS="true"
CHECK_TYPE="mainnet"
# ---
./1-pp-setup.sh 
./2-op-prepare.sh 
./3-op-start-service.sh
```

# check with differential smt rebuilt
```
export pre_chaindata_dir=$(pwd)/data_state0/seq/chaindata
export pre_smtdata_dir=$(pwd)/data_state0/seq/smt
export post_chaindata_dir=$(pwd)/data_state2/seq/chaindata
export post_smtdata_dir=$(pwd)/data_state2/seq/smt

hack -action migrateGenesis -chaindata ${pre_chaindata_dir} -input empty.json -output pre_xlayer_dump_file.json
hack -action migrateGenesis -chaindata ${post_chaindata_dir} -input empty.json -output post_xlayer_dump_file.json
hack -action verifySmtWithStateDiff -pre-chain-data ${pre_chaindata_dir} -pre-smt-data ${pre_smtdata_dir} -pre-state-snapshot pre_xlayer_dump_file.json \
 -post-state-snapshot post_xlayer_dump_file.json -post-smt-data ${post_smtdata_dir} -state-diff-output state_diff.json
```