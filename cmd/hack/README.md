# Hack

Hack is a set of developer focussed tools for dealing with the node and it's data.

## Tools
- [RPC Cache](rpc_cache/README.md)

## Developer Flags
- [Debug Flags](debug/README.md) - for limiting the node block height


## build
```
go install ./cmd/hack 

# alteratively
make hack
sudo DIST=/usr/local/bin make install 
```

## run dump genesis
```
export chaindata_dir=/data1/chaindata
hack -action migrateGenesis -chaindata ${chaindata_dir} -input empty.json -output pre_xlayer_dump_file.json -log-level info
```

## re-genesis state check
```
cd test && make min-run
# make txs fill blockchain states
make pause
export chaindata_dir=$(pwd)/data/seq/chaindata
export smtdata_dir=$(pwd)/data/seq/smt
hack -action migrateGenesis -chaindata ${chaindata_dir} -input empty.json -output xlayer_dump_file.json
hack -action checkStateRoot -chaindata ${chaindata_dir} -smt-db-path ${smtdata_dir} -standalone-smt-db=true -ignore-scalable=true -input xlayer_dump_file.json
```

## run differential smt verify

```
export pre_chaindata_dir=$(pwd)/data_state0/seq/chaindata
export pre_smtdata_dir=$(pwd)/data_state0/seq/smt
export post_chaindata_dir=$(pwd)/data_state2/seq/chaindata
export post_smtdata_dir=$(pwd)/data_state2/seq/smt

hack -action migrateGenesis -chaindata ${pre_chaindata_dir} -input empty.json -output pre_xlayer_dump_file.json
hack -action migrateGenesis -chaindata ${post_chaindata_dir} -input empty.json -output post_xlayer_dump_file.json


## verifySmtWithStateDiff parameters
# - `preSmtData` is the previous smt data path
# - `preChainData` is the previous chain data path
# - `preStateSnapshotFilePath` is the path to the pre-state snapshot file.
# - `postSmtData` is the post smt data path
# - `postStateSnapshotFilePath` is the path to the post-state snapshot file.
# - `outputStateDiffFilePath` is the path to the output state diff file.
hack -action verifySmtWithStateDiff -pre-chain-data ${pre_chaindata_dir} -pre-smt-data ${pre_smtdata_dir} -pre-state-snapshot pre_xlayer_dump_file.json \
 -post-state-snapshot post_xlayer_dump_file.json -post-smt-data ${post_smtdata_dir} -state-diff-output state_diff.json
```
