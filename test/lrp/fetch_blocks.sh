#!/bin/bash

if [ -z "$1" ] || [ -z "$2" ] || [ -z "$3" ] || [ -z "$4" ]; then
    echo "Usage: $0 --to <end_blocknumber> --rpc-url <url> [--from <start_blocknumber>] [-f <target_file>]"
    exit 1
fi

START_BLOCKNUMBER=0
TARGET_FILE="blocks_info.txt"

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --from) START_BLOCKNUMBER="$2"; shift ;;
        --to) END_BLOCKNUMBER="$2"; shift ;;
        --rpc-url) RPC_URL="$2"; shift ;;
        -f) TARGET_FILE="$2"; shift ;;
        *) echo "Unknown parameter passed: $1"; exit 1 ;;
    esac
    shift
done

if [ -z "$END_BLOCKNUMBER" ] || [ -z "$RPC_URL" ]; then
    echo "Usage: $0 --to <end_blocknumber> --rpc-url <url> [--from <start_blocknumber>] [-f <target_file>]"
    exit 1
fi

> $TARGET_FILE

for blocknumber in $(seq $START_BLOCKNUMBER $END_BLOCKNUMBER)
do
    echo "Fetching block $blocknumber..."
    cast block $blocknumber --rpc-url $RPC_URL >> $TARGET_FILE
    echo "" >> $TARGET_FILE
done

echo "All blocks fetched and saved to $TARGET_FILE"