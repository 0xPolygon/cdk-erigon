#!/usr/bin/env bash
set -euo pipefail

# End-to-end ACL test against a running sequencer/node.
# Requires:
#   - forge and cast installed
#   - RPC_URL and WALLET_PRIVATE_KEY exported in the environment
# Optional:
#   - GAS_PRICE (wei), GAS_LIMIT

err() { echo "[acl-e2e] $*" >&2; }
die() { err "$*"; exit 1; }

: "${RPC_URL?RPC_URL must be set}"
: "${WALLET_PRIVATE_KEY?WALLET_PRIVATE_KEY must be set}"

GAS_PRICE=${GAS_PRICE:-"1000000000"} # 1 gwei default
GAS_LIMIT=${GAS_LIMIT:-"2000000"}

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")"/../../.. && pwd)
TEST_DIR="$ROOT_DIR/zk/tests/acl"
CONTRACTS_DIR="$ROOT_DIR/core/state/contracts/acl"

cd "$TEST_DIR"

echo "[acl-e2e] Building contracts..."
forge build -q

DEPLOYER=$(cast wallet address "$WALLET_PRIVATE_KEY")
echo "[acl-e2e] Deployer: $DEPLOYER"

echo "[acl-e2e] Deploying AccessControlFirewall logic..."
logic_json=$(forge create $CONTRACTS_DIR/AccessControlFirewall.sol:AccessControlFirewall \
  --rpc-url "$RPC_URL" \
  --private-key "$WALLET_PRIVATE_KEY" \
  --json)
logic_addr=$(jq -r '.deployedTo' <<<"$logic_json")
[[ "$logic_addr" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "Logic address parse failed: $logic_json"
echo "[acl-e2e] Logic: $logic_addr"

echo "[acl-e2e] Deploying AdminUpgradeableProxy initialized with owner=$DEPLOYER..."
init_data=$(cast calldata "initialize(address)" "$DEPLOYER")
proxy_json=$(forge create $CONTRACTS_DIR/AdminUpgradeableProxy.sol:AdminUpgradeableProxy \
  --rpc-url "$RPC_URL" \
  --private-key "$WALLET_PRIVATE_KEY" \
  --constructor-args "$logic_addr" "$DEPLOYER" "$init_data" \
  --json)
acl_proxy=$(jq -r '.deployedTo' <<<"$proxy_json")
[[ "$acl_proxy" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "Proxy address parse failed: $proxy_json"
echo "[acl-e2e] ACL Proxy: $acl_proxy"

echo "[acl-e2e] Deploying test contract A..."
a_json=$(forge create A.sol:A \
  --rpc-url "$RPC_URL" \
  --private-key "$WALLET_PRIVATE_KEY" \
  --json)
a_addr=$(jq -r '.deployedTo' <<<"$a_json")
[[ "$a_addr" =~ ^0x[0-9a-fA-F]{40}$ ]] || die "A address parse failed: $a_json"
echo "[acl-e2e] A: $a_addr"

echo "[acl-e2e] Sanity: ACL isPermitted(subject, A, clearX()) should be false"
clear_sel=$(cast sig "clearX()")
clear_data=$(cast calldata "clearX()")
is_perm=$(cast call "$acl_proxy" "isPermitted(address,address,bytes)(bool)" "$DEPLOYER" "$a_addr" "$clear_data" --rpc-url "$RPC_URL")
echo "[acl-e2e] isPermitted(clearX) => $is_perm"

echo "[acl-e2e] Granting setX(subject=$DEPLOYER,target=$a_addr,selector=setX(uint256))"
set_sel=$(cast sig "setX(uint256)")
cast send "$acl_proxy" "grantSelector(address,address,bytes4)" "$DEPLOYER" "$a_addr" "$set_sel" \
  --rpc-url "$RPC_URL" --private-key "$WALLET_PRIVATE_KEY" \
  --legacy --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" -q

echo "[acl-e2e] Calling A.setX(777) (should succeed)"
set_tx=$(cast send "$a_addr" "setX(uint256)" 777 \
  --rpc-url "$RPC_URL" --private-key "$WALLET_PRIVATE_KEY" \
  --legacy --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT")
echo "$set_tx"

sleep 1
val=$(cast call "$a_addr" "x()(uint256)" --rpc-url "$RPC_URL")
echo "[acl-e2e] A.x => $val"
[[ "$val" == "777" ]] || die "setX did not take effect"

echo "[acl-e2e] Calling A.clearX() (should be denied by ACL)"
clear_out=$(cast send "$a_addr" "clearX()" \
  --rpc-url "$RPC_URL" --private-key "$WALLET_PRIVATE_KEY" \
  --legacy --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" 2>&1 || true)
echo "$clear_out"

# Extract tx hash if present and verify receipt status, otherwise accept revert message
txhash=$(awk '/transactionHash/ {print $2}' <<<"$clear_out" | tr -d '\r')
if [[ "$txhash" =~ ^0x[0-9a-fA-F]{64}$ ]]; then
  echo "[acl-e2e] clearX txhash: $txhash"
  sleep 1
  status_hex=$(cast receipt "$txhash" --json --rpc-url "$RPC_URL" | jq -r '.status // .Status // empty')
  if [[ -z "$status_hex" ]]; then
    # Some clients return numeric; fallback
    status_hex=$(cast receipt "$txhash" --rpc-url "$RPC_URL" | awk '/status/ {print $2}')
  fi
  echo "[acl-e2e] receipt status: $status_hex"
  [[ "$status_hex" == "0x0" || "$status_hex" == "0" ]] || die "clearX was not denied (status=$status_hex)"
else
  # If we did not get a tx hash, require a revert error in output
  grep -qi "revert\|denied" <<<"$clear_out" || die "clearX did not produce revert/denied and no tx hash was returned"
fi

sleep 1
val2=$(cast call "$a_addr" "x()(uint256)" --rpc-url "$RPC_URL")
echo "[acl-e2e] A.x after denied clearX => $val2"
[[ "$val2" == "777" ]] || die "A.x changed despite denied clearX"

echo "[acl-e2e] Success: ACL allowed setX and denied clearX as expected"

