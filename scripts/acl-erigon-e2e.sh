#!/usr/bin/env bash
set -euo pipefail

ROOT=$(pwd)
CDK="$ROOT/build/bin/"
DATADIR="$ROOT/.acl-data"
HOST="127.0.0.1"
PORT=62644 # extract from config
RPC_URL="http://$HOST:$PORT"
OWNER_KEY=${OWNER_KEY:?}
WRITER_KEY=${WRITER_KEY:?}
STRANGER_KEY=${STRANGER_KEY:?}
NODE_PID=0
CONFIG=$1

log() {
  echo "[$(date -u '+%H:%M:%S')] $*"
}

function wait_rpc() {
  local prev_block=-1
  for i in {1..60}; do
    if block=$(cast block-number -r "$RPC_URL" 2>/tmp/cast-block.err); then
      block=${block:-0}
      if (( block > prev_block )); then
        if (( prev_block >= 0 )); then
          log "block-number increasing from $prev_block to $block"
          return
        fi
        prev_block=$block
      fi
    else
      log "waiting for rpc: cast block-number failed (see /tmp/cast-block.err)"
    fi
    sleep 1
  done
  cat /tmp/acl-node.log >&2
  if [[ -f /tmp/cast-block.err ]]; then
    cat /tmp/cast-block.err >&2
  fi
  exit 1
}

function start_node() {
  local args="$1"
  local clean="${2:-true}"
  log "starting node with args: $args (clean=$clean)"
  if [[ "$clean" == "true" ]]; then
    log "resetting datadir $DATADIR"
    rm -rf "$DATADIR"
  fi
  mkdir -p "$DATADIR"
  CDK_ERIGON_SEQUENCER=1 "$CDK/cdk-erigon" --config=$CONFIG --datadir="$DATADIR" $args >/tmp/acl-node.log 2>&1 &
  NODE_PID=$!
  log "node pid $NODE_PID"
  wait_rpc
  log "node ready on $RPC_URL"
}

function stop_node() {
  if [[ "$NODE_PID" -ne 0 ]]; then
    log "stopping node pid $NODE_PID"
    kill "$NODE_PID" 2>/dev/null || true
    wait "$NODE_PID" 2>/dev/null || true
    NODE_PID=0
  fi
}

function deploy_acl() {
  log "deploying ACL stack"
  pushd "$ROOT/core/state/contracts/acl"
  mkdir -p out
  forge script script/deploy_prod.s.sol:DeployACL --private-key "$OWNER_KEY" --rpc-url "$RPC_URL" --broadcast >/tmp/forge-deploy.log
  log "deployment log available at /tmp/forge-deploy.log"
  popd
}

function configure_acl() {
  local registry=$1
  local guard=$2
  local org_id=$(cast keccak "acl.e2e")
  local writer_addr=$(cast wallet address "$WRITER_KEY")
  local owner_addr=$(cast wallet address "$OWNER_KEY")

  local policy_writer
  policy_writer=$(cast call --rpc-url "$RPC_URL" "$registry" "POLICY_WRITER()(uint8)")
  local policy_admin
  policy_admin=$(cast call --rpc-url "$RPC_URL" "$registry" "POLICY_ADMIN()(uint8)")
  local role_writer
  role_writer=$(cast call --rpc-url "$RPC_URL" "$registry" "ROLE_WRITER()(uint256)")
  local role_admin
  role_admin=$(cast call --rpc-url "$RPC_URL" "$registry" "ROLE_ADMIN()(uint256)")

  log "configuring ACL registry $registry"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "addOrg(bytes32)" "$org_id"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setOrgAdmin(bytes32,address,bool)" "$org_id" "$owner_addr" true
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "grantRole(bytes32,address,uint256)" "$org_id" "$owner_addr" "$role_admin"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "bindContractToOrg(address,bytes32)" "$guard" "$org_id"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setContractDefaultPolicy(address,uint8)" "$guard" "$policy_writer"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "grantRole(bytes32,address,uint256)" "$org_id" "$writer_addr" "$role_writer"

  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "bindContractToOrg(address,bytes32)" "$registry" "$org_id"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setContractDefaultPolicy(address,uint8)" "$registry" "$policy_admin"
  log "configured org $org_id with guard $guard, registry $registry, writer $writer_addr"
}

function fund() {
  local to=$1
  log "funding $to"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" --value 1000000000000000000 "$to"
}

function run_tests() {
  local guard=$1
  log "running writer access test"
  echo "> writer should succeed"
  cast send --rpc-url "$RPC_URL" --private-key "$WRITER_KEY" --gas-limit 200000 "$guard" "write()"
  log "writer succeeded"
  echo "> stranger should fail"
  set +e
  log "running stranger access test"
  if cast send --rpc-url "$RPC_URL" --private-key "$STRANGER_KEY" --gas-limit 200000 "$guard" "write()" >/tmp/stranger.log 2>&1; then
    echo "Stranger unexpectedly succeeded" >&2
    cat /tmp/stranger.log >&2
    exit 1
  fi
  set -e
  log "stranger access rejected"
}

trap stop_node EXIT

start_node "" true
deploy_acl
ACL_JSON="$ROOT/core/state/contracts/acl/out/acl.addresses.json"
PROXY=$(jq -r .proxy "$ACL_JSON")
REGISTRY=$(jq -r .registry "$ACL_JSON")
GUARD=$(jq -r .guard "$ACL_JSON")

configure_acl "$REGISTRY" "$GUARD"
fund $(cast wallet address "$WRITER_KEY")
fund $(cast wallet address "$STRANGER_KEY")

stop_node
# "$CDK/cdk-erigon" --datadir "$DATADIR" --http.addr $HOST --http.port $PORT --http.api eth,net,web3 --acl.enable --acl.address "$PROXY" --acl.failopen=false >/tmp/acl-node.log 2>&1 &
ACL_RUNTIME_ARGS="--acl.enable --acl.address=$PROXY --acl.failopen=false --acl.owner-bypass"
start_node "$ACL_RUNTIME_ARGS" false
NODE_PID=$!
wait_rpc
run_tests "$GUARD"
