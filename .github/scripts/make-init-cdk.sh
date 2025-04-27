#!/bin/bash
set -x
set -e

AC_SPLIT=${1:-false}
echo "Initial configuration:"
echo "AC_SPLIT=$AC_SPLIT"

# Setup paths
CURRENT_DIR=$(pwd)
MAIN_REPO_DIR="$CURRENT_DIR"
# Create test/app directory if it doesn't exist
TEST_APP_DIR="$CURRENT_DIR/test/app"
mkdir -p "$TEST_APP_DIR"
# Use the same relative path as in CI but in test/app directory
KURTOSIS_CDK_DIR="$TEST_APP_DIR/kurtosis-cdk"

# Ensure Kurtosis is installed
if ! command -v kurtosis &> /dev/null; then
  echo "Error: Kurtosis not found. Please install Kurtosis CLI first."
  exit 1
fi

# Start Kurtosis engine if not running
if ! kurtosis engine status &> /dev/null; then
  echo "Starting Kurtosis engine..."
  kurtosis engine start
  sleep 30
fi

# Clone or update kurtosis-cdk repository
if [ -d "$KURTOSIS_CDK_DIR" ]; then
  echo "Kurtosis CDK repo already exists, checking status..."
  cd "$KURTOSIS_CDK_DIR"
  
  # Check if in detached HEAD state or other abnormal states
  if ! git symbolic-ref -q HEAD >/dev/null; then
    echo "Repository is in detached HEAD state, fetching and checking out v0.2.24 branch..."
    git fetch origin
    git checkout v0.2.24 || git checkout main || git checkout master
  else
    # Normal update
    git pull
  fi
else
  echo "Cloning kurtosis-cdk repository..."
  git clone --branch v0.2.24 https://github.com/0xPolygon/kurtosis-cdk.git "$KURTOSIS_CDK_DIR"
  cd "$KURTOSIS_CDK_DIR"
fi

# Check if polycli is installed, if not install it
if [ ! -x /usr/local/bin/polycli ]; then
    echo "Installing polycli..."
    tmp_dir=$(mktemp -d)
    curl -L https://github.com/0xPolygon/polygon-cli/releases/download/v0.1.48/polycli_v0.1.48_linux_amd64.tar.gz | tar -xz -C "$tmp_dir"
    sudo mv "$tmp_dir"/* /usr/local/bin/polycli
    sudo chmod +x /usr/local/bin/polycli
    rm -rf "$tmp_dir"
    echo "polycli version:"
    /usr/local/bin/polycli version
fi

# Remove unused flags
echo "Removing unused flags from config.yml..."
sed -i '/zkevm.sequencer-batch-seal-time:/d' templates/cdk-erigon/config.yml
sed -i '/zkevm.sequencer-non-empty-batch-seal-time:/d' templates/cdk-erigon/config.yml
sed -i '/zkevm\.sequencer-initial-fork-id/d' ./templates/cdk-erigon/config.yml
sed -i '/sentry.drop-useless-peers:/d' templates/cdk-erigon/config.yml
sed -i '/zkevm\.pool-manager-url/d' ./templates/cdk-erigon/config.yml
sed -i '/zkevm.l2-datastreamer-timeout:/d' templates/cdk-erigon/config.yml

# Add specific configuration based on AC_SPLIT setting
if [ "$AC_SPLIT" = "ac-split" ]; then
  echo "Will use ac-split configuration"
  echo -e "\n" >> templates/cdk-erigon/config.yml
  echo "zkevm.standalone-smt-db: true" >> templates/cdk-erigon/config.yml
  echo "zkevm.enable-async-commit: true" >> templates/cdk-erigon/config.yml
fi

# Create params.yml with the same configuration as CI
echo "Creating params.yml for Kurtosis..."
echo 'args:' > params.yml
echo '  cdk_erigon_node_image: cdk-erigon:local' >> params.yml
echo '  el-1-geth-lighthouse: ethpandaops/lighthouse@sha256:4902d9e4a6b6b8d4c136ea54f0e51582a32f356f3dec7194a1adee13ed2d662e' >> params.yml

yq -i -y ".args.data_availability_mode = \"cdk-validium\"" params.yml

# Modify chainspec.json file (same as in CI)
echo "Modifying chainspec.json..."
sed -i 's/"londonBlock": [0-9]\+/"londonBlock": 0/' ./templates/cdk-erigon/chainspec.json
sed -i 's/"normalcyBlock": [0-9]\+/"normalcyBlock": 0/' ./templates/cdk-erigon/chainspec.json
sed -i 's/"shanghaiTime": [0-9]\+/"shanghaiTime": 0/' ./templates/cdk-erigon/chainspec.json
sed -i 's/"cancunTime": [0-9]\+/"cancunTime": 0/' ./templates/cdk-erigon/chainspec.json
sed -i '/"terminalTotalDifficulty"/d' ./templates/cdk-erigon/chainspec.json

# Ensure the image is built
cd "$MAIN_REPO_DIR"
if ! docker images | grep -q "cdk-erigon"; then
  echo "Building cdk-erigon Docker image..."
  docker build -t cdk-erigon:local --file Dockerfile .
fi

# Clean up existing enclave
if kurtosis enclave inspect cdk-v1 &> /dev/null; then
  echo "Removing existing enclave..."
  kurtosis enclave rm cdk-v1 --force
  sleep 10
fi

# Deploy Kurtosis CDK package (consistent with CI)
echo "Deploying Kurtosis CDK package..."
cd "$KURTOSIS_CDK_DIR"
kurtosis run --enclave cdk-v1 --args-file params.yml --image-download always . '{"args": {"erigon_strict_mode": false, "cdk_erigon_node_image": "cdk-erigon:local"}}'

sleep 10
echo "CDK initialization completed successfully!" 