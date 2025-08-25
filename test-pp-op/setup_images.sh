#!/bin/bash
set -e
set -x

source .env
source ./utils.sh

if docker image inspect $XLAYER_BRIDGE_SERVICE_IMAGE_TAG >/dev/null 2>&1; then
    echo "Image $XLAYER_BRIDGE_SERVICE_IMAGE_TAG exist"
else
    echo "Image $XLAYER_BRIDGE_SERVICE_IMAGE_TAG does not exist"
    build_patched_zkevm_bridge_service_image
fi


if docker image inspect aggkit:local >/dev/null 2>&1; then
    echo "Image aggkit:local exist"
else
    echo "Image aggkit:local does not exist"
    build_aggkit_image
fi


if docker image inspect $OP_GETH_IMAGE_TAG >/dev/null 2>&1; then
    echo "Image $OP_GETH_IMAGE_TAG exist"
else
    build_op_geth_image
fi

if docker image inspect $OP_CONTRACTS_IMAGE_TAG >/dev/null 2>&1 && \
 docker image inspect $OP_STACK_IMAGE_TAG >/dev/null 2>&1; then
    echo "Image $OP_STACK_IMAGE_TAG & $OP_CONTRACTS_IMAGE_TAG exist"
else
    build_op_stack_image
fi