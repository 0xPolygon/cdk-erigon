#!/bin/bash
set -x
set -e

BRIDGE_BRANCH="v0.6.0-RC10"

# # Setup paths
CURRENT_DIR=$(pwd)
MAIN_REPO_DIR="$CURRENT_DIR"
# Create test/app directory if it doesn't exist
TEST_APP_DIR="$CURRENT_DIR/test/app"
mkdir -p "$TEST_APP_DIR"
# Use the same relative path as in CI but in test/app directory
KURTOSIS_CDK_DIR="$TEST_APP_DIR/kurtosis-cdk"

# Monitor verified batches
echo "Monitoring verified batches..."
cd "$KURTOSIS_CDK_DIR"
timeout 900s .github/scripts/monitor-verified-batches.sh --enclave cdk-v1 --rpc-url $(kurtosis port print cdk-v1 cdk-erigon-rpc-001 rpc) --target 3 --timeout 900

# Run polycli loadtest
echo "Running polycli loadtest..."
polycli loadtest --rpc-url "$(kurtosis port print cdk-v1 cdk-erigon-rpc-001 rpc)" --private-key "0x12d7de8621a77640c9241b2595ba78ce443d05e94090365ab3bb5e19df82c625" --verbosity 700 --requests 500 --rate-limit 50 --mode uniswapv3 --legacy
POLYCLI_EXIT_CODE=$?

echo "polycli loadtest completed with exit code: $POLYCLI_EXIT_CODE"

# Set up environment variables
echo "Setting up environment variables..."
cd "$CURRENT_DIR"
# Clean up existing directory if it exists
if [ -d "$TEST_APP_DIR/bridge-config-artifact" ]; then
  echo "Removing existing bridge-config-artifact directory..."
  rm -rf "$TEST_APP_DIR/bridge-config-artifact"
fi
mkdir -p "$TEST_APP_DIR"
cd "$TEST_APP_DIR"
kurtosis files download cdk-v1 bridge-config-artifact

# Extract configuration from TOML file
echo "Extracting bridge address from TOML file..."
if [ ! -f "$TEST_APP_DIR/bridge-config-artifact/bridge-config.toml" ]; then
  echo "Error: bridge-config.toml not found. Checking if files were downloaded properly..."
  ls -la "$TEST_APP_DIR/bridge-config-artifact/"
  echo "Failed to find bridge configuration file. Exiting."
  exit 1
fi

BRIDGE_ADDRESS=$(grep -o "PolygonBridgeAddress *= *\"[^\"]*\"" "$TEST_APP_DIR/bridge-config-artifact/bridge-config.toml" 2>/dev/null | cut -d'"' -f2 || echo "")

# Verify bridge address was extracted
if [ -z "$BRIDGE_ADDRESS" ]; then
  echo "Failed to extract bridge address. Using alternative method..."
  # Try direct grep as fallback for any Ethereum address format
  BRIDGE_ADDRESS=$(grep -o "0x[a-fA-F0-9]\{40\}" "$TEST_APP_DIR/bridge-config-artifact/bridge-config.toml" 2>/dev/null | head -1 || echo "")
  
  if [ -z "$BRIDGE_ADDRESS" ]; then
    echo "Warning: Failed to extract bridge address from configuration file"
    # Use a default address for testing if extraction failed
    BRIDGE_ADDRESS="0x0000000000000000000000000000000000000000"
    echo "Using default bridge address: $BRIDGE_ADDRESS for testing purposes"
  fi
fi

# Get port information with error handling
echo "Getting port information..."

# Get ETH L1 RPC URL
ETH_RPC_URL=$(kurtosis port print cdk-v1 el-1-geth-lighthouse rpc)

# Get Bridge API URL
BRIDGE_API_URL=$(kurtosis port print cdk-v1 zkevm-bridge-service-001 rpc)

# Get L2 RPC URL
L2_RPC_URL=$(kurtosis port print cdk-v1 cdk-erigon-rpc-001 rpc)

# Strip any existing protocol prefix to ensure clean URLs
ETH_RPC_URL=$(echo "$ETH_RPC_URL" | sed -E 's|^(https?:)?//||')
L2_RPC_URL=$(echo "$L2_RPC_URL" | sed -E 's|^(https?:)?//||')
BRIDGE_API_URL=$(echo "$BRIDGE_API_URL" | sed -E 's|^(https?:)?//||')

echo "BRIDGE_ADDRESS=$BRIDGE_ADDRESS"
echo "ETH_RPC_URL=$ETH_RPC_URL"
echo "BRIDGE_API_URL=$BRIDGE_API_URL"
echo "L2_RPC_URL=$L2_RPC_URL"

# Handle bridge repository
BRIDGE_REPO="$TEST_APP_DIR/bridge"

if [ -d "$BRIDGE_REPO" ]; then
  echo "Bridge repository already exists, updating to $BRIDGE_BRANCH..."
  cd "$BRIDGE_REPO"
  
  # Check if we are in detached HEAD state and handle it properly
  if ! git symbolic-ref -q HEAD >/dev/null; then
    echo "Repository is in detached HEAD state, fetching and checking out $BRIDGE_BRANCH branch..."
    git fetch origin
    git checkout $BRIDGE_BRANCH || git checkout master || git checkout main
  else
    # Normal branch update
    git fetch
    git checkout $BRIDGE_BRANCH
    git pull origin $BRIDGE_BRANCH
  fi
else
  echo "Cloning bridge repository..."
  git clone --recurse-submodules -j8 https://github.com/0xPolygonHermez/zkevm-bridge-service.git -b $BRIDGE_BRANCH "$BRIDGE_REPO"
  cd "$BRIDGE_REPO"
fi

# Build docker image
echo "Building docker image..."
cd "$BRIDGE_REPO"
make build-docker-e2e-real_network

# Run test ERC20 Bridge
echo "Running ERC20 Bridge test..."
mkdir -p "$BRIDGE_REPO/tmp"

# On Linux, we need to check if we're running inside Docker
if grep -q docker /proc/1/cgroup 2>/dev/null; then
  # Inside Docker container, use host networking
  DOCKER_NETWORK_PARAM="--network=host"
  
  # Create test configuration file with standard URLs
  cat <<EOF > "$BRIDGE_REPO/tmp/test.toml"
TestL1AddrPrivate="0x12d7de8621a77640c9241b2595ba78ce443d05e94090365ab3bb5e19df82c625"
TestL2AddrPrivate="0x12d7de8621a77640c9241b2595ba78ce443d05e94090365ab3bb5e19df82c625"
[ConnectionConfig]
L1NodeURL="http://${ETH_RPC_URL}"
L2NodeURL="http://${L2_RPC_URL}"
BridgeURL="http://${BRIDGE_API_URL}"
L1BridgeAddr="${BRIDGE_ADDRESS}"
L2BridgeAddr="${BRIDGE_ADDRESS}"
EOF
else
  # Not in Docker, determine the host IP for container to host communication
  HOST_IP=$(ip -4 addr show scope global dev docker0 2>/dev/null | grep inet | awk '{print $2}' | cut -d/ -f1)
  if [ -z "$HOST_IP" ]; then
    # Try alternative network interfaces
    HOST_IP=$(ip -4 route get 1 2>/dev/null | awk '{print $7}')
  fi
  
  if [ -z "$HOST_IP" ]; then
    echo "Warning: Could not determine host IP, using host networking"
    DOCKER_NETWORK_PARAM="--network=host"
    
    # Create test configuration with standard URLs
    cat <<EOF > "$BRIDGE_REPO/tmp/test.toml"
TestL1AddrPrivate="0x12d7de8621a77640c9241b2595ba78ce443d05e94090365ab3bb5e19df82c625"
TestL2AddrPrivate="0x12d7de8621a77640c9241b2595ba78ce443d05e94090365ab3bb5e19df82c625"
[ConnectionConfig]
L1NodeURL="http://${ETH_RPC_URL}"
L2NodeURL="http://${L2_RPC_URL}"
BridgeURL="http://${BRIDGE_API_URL}"
L1BridgeAddr="${BRIDGE_ADDRESS}"
L2BridgeAddr="${BRIDGE_ADDRESS}"
EOF
  else
    echo "Using host IP: $HOST_IP for Docker networking"
    # Use bridge networking with extra hosts
    DOCKER_NETWORK_PARAM="--add-host=host.docker.internal:$HOST_IP"
    
    # Extract ports from cleaned URLs for host IP configuration
    cat <<EOF > "$BRIDGE_REPO/tmp/test.toml"
TestL1AddrPrivate="0x12d7de8621a77640c9241b2595ba78ce443d05e94090365ab3bb5e19df82c625"
TestL2AddrPrivate="0x12d7de8621a77640c9241b2595ba78ce443d05e94090365ab3bb5e19df82c625"
[ConnectionConfig]
L1NodeURL="http://${HOST_IP}:$(echo $ETH_RPC_URL | cut -d: -f2)"
L2NodeURL="http://${HOST_IP}:$(echo $L2_RPC_URL | cut -d: -f2)"
BridgeURL="http://${HOST_IP}:$(echo $BRIDGE_API_URL | cut -d: -f2)"
L1BridgeAddr="${BRIDGE_ADDRESS}"
L2BridgeAddr="${BRIDGE_ADDRESS}"
EOF
  fi
fi

echo "Docker network parameter: $DOCKER_NETWORK_PARAM"
echo "Contents of test.toml:"
cat "$BRIDGE_REPO/tmp/test.toml"

# Ensure Docker is installed and running
if ! command -v docker &> /dev/null; then
  echo "Error: Docker is not installed or not in PATH"
  exit 1
fi

if ! docker info &> /dev/null; then
  echo "Error: Docker daemon is not running or current user doesn't have permission"
  exit 1
fi

# Run the Docker container with appropriate network settings
echo "Running ERC20 Bridge test container..."
cd "$BRIDGE_REPO"

# Print Docker configuration for debugging
echo "Docker configuration:"
echo "Network parameter: $DOCKER_NETWORK_PARAM"
echo "Config volume: $BRIDGE_REPO/tmp/:/config/"
echo "Docker image information:"
docker images | grep bridge-e2e-realnetwork-erc20 || echo "Warning: Image not found!"

echo "Test config file contents:"
cat "$BRIDGE_REPO/tmp/test.toml"

# Run Docker with absolute path to avoid path resolution issues
echo "Starting bridge test container..."
docker run $DOCKER_NETWORK_PARAM --volume "$(realpath "$BRIDGE_REPO/tmp/"):/config/" --env BRIDGE_TEST_CONFIG_FILE=/config/test.toml bridge-e2e-realnetwork-erc20
DOCKER_EXIT_CODE=$?

if [ $DOCKER_EXIT_CODE -ne 0 ]; then
  echo "Bridge test container exited with code $DOCKER_EXIT_CODE"
  echo "Attempting fallback run with host networking..."
  docker run --network=host --volume "$(realpath "$BRIDGE_REPO/tmp/"):/config/" --env BRIDGE_TEST_CONFIG_FILE=/config/test.toml bridge-e2e-realnetwork-erc20
  DOCKER_EXIT_CODE=$?
fi

echo "Bridge test completed with exit code: $DOCKER_EXIT_CODE"

echo "All tests completed successfully!" 