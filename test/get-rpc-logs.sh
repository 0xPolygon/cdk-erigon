#!/bin/bash

ID=`docker ps | grep "xlayer-rpc$" | tr -s ' ' | cut -d ' ' -f 1`
if [ -z "$ID" ]; then
	echo "No rpc docker container found!"
	exit 1
fi
docker logs $ID > logs.txt 2>&1
cat logs.txt | grep "err"
cat logs.txt | grep "EROR"