#!/bin/bash

# TODO - do not use this scritpt yet.

rm -f ./ci/utils/docker-images.txt
cat ./test/docker-compose.yml | grep "image:" | tr -s ' ' | cut -d ' ' -f 3 | sort | uniq | grep -v "cdk-erigon" > ./ci/utils/docker-images.txt