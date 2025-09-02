#!/bin/bash
set -e
set -x

source .env



ROOT_DIR="$(dirname "$PWD_DIR")"


build_patched_zkevm_bridge_service_image() {
  echo "build patched zkevm bridge service image"
  PWD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  rm -rf $PWD_DIR/tmp/zkevm-bridge-service
  cd $PWD_DIR/tmp/
  git clone -b v0.6.0-RC16 https://github.com/0xPolygon/zkevm-bridge-service.git
    # it has docker file
  cd zkevm-bridge-service

  # patch zkevm-bridge-service
  git apply $PWD_DIR/patch/xlayer-bridge-service-0001-support-sync-L2-block-at-given-number.patch

  docker build -t $XLAYER_BRIDGE_SERVICE_IMAGE_TAG .
  cd $PWD_DIR
}


build_aggkit_image() {
  echo "build aggkit image"
  PWD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  rm -rf $PWD_DIR/tmp/aggkit
  cd $PWD_DIR/tmp/


  echo "Cloning contract repository..."
  git clone -b feature/0.1.0 https://github.com/okx/aggkit.git
  cd ./aggkit
  echo "Cleaning and resting contract repository..."
  git reset --hard; git checkout feature/0.1.0;git pull
  make build-docker
  cd $PWD_DIR
}

build_op_geth_image() {
  echo "build op-geth image"
  PWD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  rm -rf $PWD_DIR/tmp/op-geth
  cd $PWD_DIR/tmp/
  echo "Cloning op-geth repository..."
  git clone -b dev-op https://github.com/okx/op-geth.git 
  cp $PWD_DIR/op-docker/Dockerfile-opgeth op-geth/Dockerfile
  cd op-geth

  # patch op-geth
  # git checkout 6005dd53e1b50fe5a3f59764e3e2056a639eff2f # optimism v1.13.4 relies on this commit
  # git apply $PWD_DIR/patch/op-geth-0001-support-load-genesis-at-a-given-number.patch

  docker build -t $OP_GETH_IMAGE_TAG .
  cd $PWD_DIR
}

build_op_stack_image() {
  echo "build op stack image"
  PWD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  rm -rf $PWD_DIR/tmp/optimism
  cd $PWD_DIR/tmp/
  echo "Cloning Optimism repository..."
  git clone -b yxq/regenesis-op-mainnet https://github.com/okx/optimism.git
  cp $PWD_DIR/op-docker/Dockerfile-contracts optimism/Dockerfile-contracts
  cp $PWD_DIR/op-docker/Dockerfile-opstack optimism/Dockerfile-opstack

  # cp Transactor.sol to optimism, which is used for addGameType
  cp $PWD_DIR/contracts/Transactor.sol optimism/packages/contracts-bedrock/src/periphery/Transactor.sol

  # To support making prestate for our custom op-geth
  mv op-geth optimism/op-geth
  ln -s optimism/op-geth ./
#  cd optimism
#  git apply $PWD_DIR/patch/optimism-0001-support-regenesis-op-geth-prestate.patch
#  cd -

  cd optimism
  docker build -t $OP_CONTRACTS_IMAGE_TAG -f Dockerfile-contracts .
  docker build -t $OP_STACK_IMAGE_TAG -f Dockerfile-opstack .
  cd $PWD_DIR
}