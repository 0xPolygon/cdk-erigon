#!/bin/bash

CONFIG_FILE="./test/config/test.erigon.seq.config.yaml"

# Check if config file exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: Config file $CONFIG_FILE not found!"
    exit 1
fi

echo "Updating $CONFIG_FILE..."

# Create a temporary file
TEMP_FILE=$(mktemp)

# Copy original file to temp file
cp "$CONFIG_FILE" "$TEMP_FILE"

# Remove existing configurations
if [[ "$OSTYPE" == "darwin"* ]]; then
    # macOS version
    sed -i '' '/^zkevm.executor-urls:/d; /^zkevm.executor-strict:/d; /^zkevm.witness-full:/d; /^zkevm.executor-mock:/d; /^zkevm.sequencer-block-seal-time:/d' "$TEMP_FILE"
else
    # Linux version
    sed -i '/^zkevm.executor-urls:/d; /^zkevm.executor-strict:/d; /^zkevm.witness-full:/d; /^zkevm.executor-mock:/d; /^zkevm.sequencer-block-seal-time:/d' "$TEMP_FILE"
fi

# Add new configurations
cat << EOF >> "$TEMP_FILE"
zkevm.sequencer-block-seal-time: "5s"
zkevm.executor-urls: "xlayer-executor:50071"
zkevm.executor-strict: true
zkevm.witness-full: false
zkevm.executor-mock: false
EOF

# Move temp file back to original
mv "$TEMP_FILE" "$CONFIG_FILE"

# Clean up any backup files that might have been created
rm -f "${CONFIG_FILE}''"

echo "Finished updating $CONFIG_FILE."
echo "Current configuration:"
cat "$CONFIG_FILE"