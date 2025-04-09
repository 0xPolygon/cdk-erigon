#!/bin/bash

ID=`docker ps | grep "xlayer-rpc$" | tr -s ' ' | cut -d ' ' -f 1`
if [ -z "$ID" ]; then
	echo "No rpc docker container found!"
	exit 1
fi
LNAME="rpc-logs.txt"
docker logs $ID > $LNAME 2>&1
cat $LNAME | grep "err"
cat $LNAME | grep "EROR"