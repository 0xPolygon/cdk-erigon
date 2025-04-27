#!/bin/bash

GIT_ROOT=`git rev-parse --show-toplevel`
cd $GIT_ROOT/ci/utils/

IMAGES=$(cat docker-images.txt | tr '\n' ' ')

docker run -d -p 5000:5000 --name registry registry:latest
REGIP=$(docker exec registry sh -c "ifconfig eth0 | grep 'inet addr' | tr -s ' ' | cut -d ' ' -f 3 | cut -d ':' -f 2")

echo "Docker registry IP: $REGIP:5000"

for IMAGE in $IMAGES; do
    docker pull $IMAGE
    docker tag $IMAGE localhost:5000/$IMAGE
    docker push localhost:5000/$IMAGE
    docker rmi localhost:5000/$IMAGE
done