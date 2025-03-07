#!/bin/bash

ID=`docker ps | grep "xlayer-seq$" | tr -s ' ' | cut -d ' ' -f 1`
if [ -z "$ID" ]; then
	echo "No seq docker found!"
	exit 1
fi
docker logs $ID > logs.txt 2>&1
cat logs.txt | grep "err"
cat logs.txt | grep "EROR"