#!/bin/bash

ID=`docker ps -a | grep "xlayer-seq$" | tr -s ' ' | cut -d ' ' -f 1`
if [ -z "$ID" ]; then
	echo "No seq docker container found!"
	exit 1
fi
LNAME="seq-logs.txt"
docker logs $ID > $LNAME 2>&1
cat $LNAME | grep "err"
cat $LNAME | grep "EROR"