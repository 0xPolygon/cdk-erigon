#!/bin/bash

CONFIG_FILE="./test/config/test.erigon.seq.config.yaml"

# Check if config file exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: Config file $CONFIG_FILE not found!"
    exit 1
fi

echo "Updating $CONFIG_FILE..."

# Clean up any backup files that might have been created
rm -f "${CONFIG_FILE}''"

echo "Finished updating $CONFIG_FILE."
echo "Current configuration:"
cat "$CONFIG_FILE"