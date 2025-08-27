set -e
set -x

source .env
./scripts/prepare-check-regenesis.sh $CHECK_TYPE

export chaindata_dir=$(pwd)/data_state2/seq/chaindata
export smtdata_dir=$(pwd)/data_state2/seq/smt
hack -action migrateGenesis -chaindata ${chaindata_dir} -input empty.json -output xlayer_dump_file.json

nohup hack -action checkStateRoot -chaindata ${chaindata_dir} -smt-db-path ${smtdata_dir} -standalone-smt-db=true -input xlayer_dump_file.json -ignore-scalable 2>&1 >rebuild.out &
