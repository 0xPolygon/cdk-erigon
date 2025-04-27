#!/bin/bash

if [ $# -lt 1 ]; then
    echo "Usage: $0 <docker_registry_ip>"
    exit 1
fi
DOCKER_REGISTRY_IP_PORT=$1

IMAGES=$(cat docker-images.txt | tr '\n' ' ')

for IMAGE in $IMAGES; do
    docker pull $DOCKER_REGISTRY_IP_PORT/$IMAGE
    docker tag $DOCKER_REGISTRY_IP_PORT/$IMAGE $IMAGE
done