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
REGISTRY_KIND=$(echo "${ACL_REGISTRY_KIND:-rbac}" | tr '[:upper:]' '[:lower:]')
NODE_LOG_FILE=${NODE_LOG_FILE:-/tmp/acl-node.log}
CAST_BLOCK_ERR=${CAST_BLOCK_ERR:-/tmp/cast-block.err}
FORGE_DEPLOY_LOG=${FORGE_DEPLOY_LOG:-/tmp/forge-deploy.log}
STRANGER_LOG=${STRANGER_LOG:-/tmp/stranger.log}

log() {
  echo "[$(date -u '+%H:%M:%S')] $*"
}

function wait_rpc() {
  local prev_block=-1
  for i in {1..60}; do
    if block=$(cast block-number -r "$RPC_URL" 2>"$CAST_BLOCK_ERR"); then
      block=${block:-0}
      if (( block > prev_block )); then
        if (( prev_block >= 0 )); then
          log "block-number increasing from $prev_block to $block"
          return
        fi
        prev_block=$block
      fi
    else
      log "waiting for rpc: cast block-number failed (see $CAST_BLOCK_ERR)"
    fi
    sleep 1
  done
  cat "$NODE_LOG_FILE" >&2
  if [[ -f "$CAST_BLOCK_ERR" ]]; then
    cat "$CAST_BLOCK_ERR" >&2
  fi
  exit 1
}

function cleanup() {
  log "cleaning previous run artifacts"
  rm -rf "$DATADIR"
  rm -f "$NODE_LOG_FILE" "$CAST_BLOCK_ERR" "$FORGE_DEPLOY_LOG" "$STRANGER_LOG"
}

function start_node() {
  local args="$1"
  log "starting node with args: $args"
  mkdir -p "$DATADIR"
  ACL_TRACE=1 CDK_ERIGON_SEQUENCER=1 "$CDK/cdk-erigon" --config=$CONFIG --datadir="$DATADIR" $args >$NODE_LOG_FILE 2>&1 &
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
  if [ -f "$FORGE_DEPLOY_LOG" ]; then
    rm -f "$FORGE_DEPLOY_LOG"
  fi
  forge script script/deploy_prod.s.sol:DeployACL --private-key "$OWNER_KEY" --rpc-url "$RPC_URL" --broadcast >"$FORGE_DEPLOY_LOG"
  log "deployment log available at $FORGE_DEPLOY_LOG"
  popd
}

function configure_acl_rbac() {
  local registry=$1
  local guard=$2
  local org_id=$(cast keccak "acl.e2e")
  local writer_addr=$(cast wallet address "$WRITER_KEY")
  local owner_addr=$(cast wallet address "$OWNER_KEY")
  local admin_group=$(cast keccak "admins.group")
  local writer_group=$(cast keccak "writers.group")

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
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setGroupRoleBits(bytes32,bytes32,uint256)" "$org_id" "$admin_group" "$role_admin"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setGroupMember(bytes32,bytes32,address,bool)" "$org_id" "$admin_group" "$owner_addr" true
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "bindContractToOrg(address,bytes32)" "$guard" "$org_id"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setContractDefaultPolicy(address,uint8)" "$guard" "$policy_writer"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setGroupRoleBits(bytes32,bytes32,uint256)" "$org_id" "$writer_group" "$role_writer"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setGroupMember(bytes32,bytes32,address,bool)" "$org_id" "$writer_group" "$writer_addr" true

  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "bindContractToOrg(address,bytes32)" "$registry" "$org_id"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setContractDefaultPolicy(address,uint8)" "$registry" "$policy_admin"
  log "configured org $org_id with guard $guard, registry $registry, writer $writer_addr"
}

function configure_acl_claim() {
  local registry=$1
  local guard=$2
  local org_id=$(cast keccak "acl.e2e")
  local writer_addr=$(cast wallet address "$WRITER_KEY")
  local owner_addr=$(cast wallet address "$OWNER_KEY")

  local policy_writer
  policy_writer=$(cast call --rpc-url "$RPC_URL" "$registry" "POLICY_WRITER()(uint8)")
  local policy_admin
  policy_admin=$(cast call --rpc-url "$RPC_URL" "$registry" "POLICY_ADMIN()(uint8)")

  local claim_writer
  claim_writer=$(cast call --rpc-url "$RPC_URL" "$registry" "CLAIM_WRITER()(bytes32)")
  local claim_admin
  claim_admin=$(cast call --rpc-url "$RPC_URL" "$registry" "CLAIM_ADMIN()(bytes32)")

  log "configuring claim-based ACL registry $registry"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "addOrg(bytes32)" "$org_id"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setOrgAdmin(bytes32,address,bool)" "$org_id" "$owner_addr" true
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setOrgName(bytes32,string)" "$org_id" "acl.e2e"

  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setUserOrg(bytes32,address)" "$org_id" "$owner_addr"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setClaim(bytes32,address,bytes32,bool)" "$org_id" "$owner_addr" "$claim_admin" true

  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setUserOrg(bytes32,address)" "$org_id" "$writer_addr"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setClaim(bytes32,address,bytes32,bool)" "$org_id" "$writer_addr" "$claim_writer" true

  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "bindContractToOrg(address,bytes32)" "$guard" "$org_id"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setContractDefaultPolicy(address,uint8)" "$guard" "$policy_writer"

  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "bindContractToOrg(address,bytes32)" "$registry" "$org_id"
  cast send --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" "$registry" "setContractDefaultPolicy(address,uint8)" "$registry" "$policy_admin"

  # sanity checks
  local boundOrg
  boundOrg=$(cast call --rpc-url "$RPC_URL" "$registry" "contractToOrg(address)(bytes32)" "$guard")
  if [[ "$boundOrg" != "$org_id" ]]; then
    log "guard not bound to org (expected $org_id, got $boundOrg)"
    exit 1
  fi
  local effective
  effective=$(cast call --rpc-url "$RPC_URL" "$registry" "effectiveRoles(bytes32,address)(uint256)" "$org_id" "$writer_addr")
  if [[ "$effective" == "0x0" ]]; then
    log "writer missing effective roles for org $org_id"
    exit 1
  fi
  local hasWriterClaim
  hasWriterClaim=$(cast call --rpc-url "$RPC_URL" "$registry" "hasScopedClaim(bytes32,address,bytes32)(bool)" "$org_id" "$writer_addr" "$claim_writer")
  if [[ "$hasWriterClaim" != "true" ]]; then
    log "writer missing claim in org $org_id"
    exit 1
  fi
  log "configured claim org $org_id with guard $guard, registry $registry, writer $writer_addr"
}

function configure_acl() {
  local registry=$1
  local guard=$2
  if [[ "$REGISTRY_KIND" == "claim" ]]; then
    configure_acl_claim "$registry" "$guard"
  else
    configure_acl_rbac "$registry" "$guard"
  fi
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
  # determine a safe gas price (fallback to legacy if 1559/tips look odd)
  local gp
  gp=$(cast gas-price -r "$RPC_URL" 2>/dev/null || echo 0)
  if [[ -z "$gp" || "$gp" == "0x0" ]]; then
    gp="0x3b9aca00" # 1 gwei fallback
  fi
  log "using gasPrice=$gp"
  cast send --rpc-url "$RPC_URL" --private-key "$WRITER_KEY" --gas-limit 200000 --legacy --gas-price "$gp" "$guard" "write()"
  log "writer succeeded"
  echo "> stranger should fail"
  set +e
  log "running stranger access test"

  if cast send --rpc-url "$RPC_URL" --private-key "$STRANGER_KEY" --gas-limit 200000 "$guard" "write()" >"$STRANGER_LOG" 2>&1; then
    echo "Stranger unexpectedly succeeded" >&2
    cat "$STRANGER_LOG" >&2
    exit 1
  fi
  set -e
  log "stranger access rejected"
}

trap stop_node EXIT

cleanup
start_node ""
deploy_acl
ACL_JSON="$ROOT/core/state/contracts/acl/out/acl.addresses.json"
PROXY=$(jq -r .proxy "$ACL_JSON")
REGISTRY=$(jq -r .registry "$ACL_JSON")
GUARD=$(jq -r .guard "$ACL_JSON")

log "registry kind selected: $REGISTRY_KIND"
log "using ACL proxy $PROXY (registry $REGISTRY)"
configure_acl "$REGISTRY" "$GUARD"
fund $(cast wallet address "$WRITER_KEY")
fund $(cast wallet address "$STRANGER_KEY")

stop_node
# "$CDK/cdk-erigon" --datadir "$DATADIR" --http.addr $HOST --http.port $PORT --http.api eth,net,web3 --acl.enable --acl.address "$PROXY" --acl.failopen=false >/tmp/acl-node.log 2>&1 &
ACL_RUNTIME_ARGS="--acl.enable --acl.address=$PROXY --acl.failopen=false --acl.owner-bypass" # Enable when needed and fixed
start_node "$ACL_RUNTIME_ARGS"
# start_node ""
NODE_PID=$!
wait_rpc
writer_addr=$(cast wallet address "$WRITER_KEY")
owner_addr=$(cast wallet address "$OWNER_KEY")
log "ACL check writer→guard: $(cast call --rpc-url "$RPC_URL" "$PROXY" \
    "isPermitted(address,address,bytes)(bool)" "$writer_addr" "$GUARD" 0x)"
log "ACL check owner→guard: $(cast call --rpc-url "$RPC_URL" "$PROXY" \
    "isPermitted(address,address,bytes)(bool)" "$owner_addr" "$GUARD" 0x)"

org_id=$(cast keccak "acl.e2e")
claim_writer=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" "CLAIM_WRITER()(bytes32)")
writer_claim=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" \
    "hasScopedClaim(bytes32,address,bytes32)(bool)" "$org_id" "$writer_addr" "$claim_writer")
log "writer claim status: $writer_claim"

# extra diagnostics: inspect registry-side view used by runtime preflight
policy_public=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" "POLICY_PUBLIC()(uint8)")
policy_reader=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" "POLICY_READER()(uint8)")
policy_writer=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" "POLICY_WRITER()(uint8)")
policy_admin=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" "POLICY_ADMIN()(uint8)")
role_reader=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" "ROLE_READER()(uint256)")
role_writer=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" "ROLE_WRITER()(uint256)")
role_admin=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" "ROLE_ADMIN()(uint256)")

bound_org=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" "contractToOrg(address)(bytes32)" "$GUARD")
default_policy=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" "requiredRoleDefault(address)(uint8)" "$GUARD")
eff_roles=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" "effectiveRoles(bytes32,address)(uint256)" "$org_id" "$writer_addr")
org_exists=$(cast call --rpc-url "$RPC_URL" "$REGISTRY" "orgExists(bytes32)(bool)" "$org_id")

log "diag: orgExists=$org_exists boundOrg=$bound_org requiredRoleDefault(guard)=$default_policy"
log "diag: policies public=$policy_public reader=$policy_reader writer=$policy_writer admin=$policy_admin"
log "diag: roles reader=$role_reader writer=$role_writer admin=$role_admin effective(writer)=$eff_roles"

run_tests "$GUARD"
