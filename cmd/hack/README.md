# Hack

Hack is a set of developer focussed tools for dealing with the node and it's data.

## Tools
- [RPC Cache](rpc_cache/README.md)

## Developer Flags
- [Debug Flags](debug/README.md) - for limiting the node block height


## build
```
make hack
sudo DIST=/usr/local/bin make install # optional on mac
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