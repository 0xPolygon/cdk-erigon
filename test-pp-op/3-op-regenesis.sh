set -e
set -x


docker-compose down xlayer-seq
docker-compose down xlayer-rpc

docker-compose down xlayer-bridge-service
docker-compose down xlayer-bridge-ui
docker-compose down xlayer-cdk-node

docker-compose down xlayer-agglayer
docker-compose down xlayer-agglayer-prover



sed_inplace() {
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

PWD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$PWD_DIR")"

cd $ROOT_DIR

go install ./cmd/hack/

cd $PWD_DIR

cp ./config-op/genesis.json ./config-op/genesis-op-raw.json

hack -action migrateGenesis -chaindata ./data/seq/chaindata/ -input ./config-op/genesis-op-raw.json   -output ./config-op/genesis.json