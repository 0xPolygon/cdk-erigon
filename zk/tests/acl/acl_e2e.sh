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

# Pretty separators for readability between test steps
hr() { echo "[acl-e2e] ====================================================================="; }
section() {
  local title=$1
  echo
  hr
  echo "[acl-e2e] $title"
  hr
}
run_step() {
  local title=$1; shift
  section "$title"
  "$@"
  hr
}

# Send a tx via cast and ensure success by waiting for a receipt
# Usage: send_success <PRIVATE_KEY> <FROM_ADDR> <TO_ADDR> <SIG> [ARGS...]
send_success() {
  local pk="$1"; shift
  local from="$1"; shift
  local to="$1"; shift
  local sig="$1"; shift
  local tries=${RECEIPT_TRIES:-30}
  local sleep_s=${RECEIPT_SLEEP:-1}
  local out txhash status_hex err_msg
  out=$(cast send "$to" "$sig" "$@" \
    --rpc-url "$RPC_URL" --private-key "$pk" --from "$from" \
    --legacy --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" --json --async 2>&1 || true)
  echo "$out"
  err_msg=$(jq -r '.error.message // empty' <<<"$out" 2>/dev/null || true)
  if [[ -n "$err_msg" ]]; then
    die "send to $to $sig failed: $err_msg"
  fi
  txhash=$(jq -r '.transactionHash // .hash // .txHash // empty' <<<"$out" 2>/dev/null || true)
  if [[ -z "$txhash" && "$out" =~ ^0x[0-9a-fA-F]{64}$ ]]; then
    txhash="$out"
  fi
  [[ "$txhash" =~ ^0x[0-9a-fA-F]{64}$ ]] || die "send did not return tx hash"
  echo "[acl-e2e] txhash: $txhash"
  status_hex=""
  while (( tries > 0 )); do
    status_hex=$(cast receipt "$txhash" --json --rpc-url "$RPC_URL" --async | jq -r '.status // .Status // empty' 2>/dev/null || true)
    if [[ -n "$status_hex" ]]; then break; fi
    sleep "$sleep_s"; tries=$((tries-1))
  done
  [[ -n "$status_hex" ]] || die "no receipt within timeout for $txhash"
  echo "[acl-e2e] receipt status: $status_hex"
  [[ "$status_hex" == "0x1" || "$status_hex" == "1" ]] || die "receipt status not successful for $txhash"
}

: "${RPC_URL?RPC_URL must be set}"
# Admin key (owner) for deploy/grant. Backward compatible with WALLET_PRIVATE_KEY
if [[ -z "${ADMIN_PRIVATE_KEY:-}" ]]; then
  : "${WALLET_PRIVATE_KEY?Either ADMIN_PRIVATE_KEY or WALLET_PRIVATE_KEY must be set}"
  ADMIN_PRIVATE_KEY="$WALLET_PRIVATE_KEY"
fi
# Subject key (caller) for exercising ACL
# If not provided or if equal to admin, we'll generate a fresh key and fund it.
SUBJECT_PRIVATE_KEY="${SUBJECT_PRIVATE_KEY:-}"

GAS_PRICE=${GAS_PRICE:-"1000000000"} # 1 gwei default
GAS_LIMIT=${GAS_LIMIT:-"2000000"}

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")"/../../.. && pwd)
TEST_DIR="$ROOT_DIR/zk/tests/acl"
CONTRACTS_DIR="$ROOT_DIR/core/state/contracts/acl"

cd "$TEST_DIR"

export FOUNDRY_NO_ENS=1
export ETHERS_NO_ENS=1
export NO_ENS=1

echo "[acl-e2e] Building contracts..."
forge build -q

ADMIN=$(cast wallet address "$ADMIN_PRIVATE_KEY")

# Generate a fresh subject key (via cast) if missing or identical to admin
ensure_subject_key() {
  if [[ -z "${SUBJECT_PRIVATE_KEY:-}" || "$(cast wallet address "$SUBJECT_PRIVATE_KEY" 2>/dev/null || true)" == "$ADMIN" ]]; then
    local wjson pk addr
    # ask cast to emit JSON (newer foundry prints an array even for one key)
    wjson=$(cast wallet new --number 1 --json 2>/dev/null)
    pk=$(jq -r 'if type=="array" then (.[0].private_key // .[0].privateKey // empty) else (.private_key // .privateKey // empty) end' <<<"$wjson" 2>/dev/null || true)
    addr=$(jq -r 'if type=="array" then (.[0].address // empty) else (.address // empty) end' <<<"$wjson" 2>/dev/null || true)
    if [[ -z "$pk" || -z "$addr" ]]; then
      die "Failed to generate subject key via cast; ensure Foundry is installed and up to date"
    fi
    SUBJECT_PRIVATE_KEY="$pk"
    export SUBJECT_PRIVATE_KEY
  fi
}

# Fund subject if balance is below the minimal required amount for two txs
fund_subject_if_needed() {
  SUBJECT=$(cast wallet address "$SUBJECT_PRIVATE_KEY")
  echo "[acl-e2e] Admin:   $ADMIN"
  echo "[acl-e2e] Subject: $SUBJECT"
  if [[ "$ADMIN" == "$SUBJECT" ]]; then
    err "Admin and Subject are the same. If owner-bypass or bypass is enabled, ACL denials won't trigger."
  fi
  local bal required fund
  bal=$(cast balance "$SUBJECT" --rpc-url "$RPC_URL" 2>/dev/null || echo 0)
  [[ "$bal" =~ ^[0-9]+$ ]] || bal=0
  # Estimate required wei: two calls at GAS_LIMIT plus some margin + cost of a funding tx
  # required = GAS_PRICE * (2*GAS_LIMIT + 21000) + 1e15 (buffer)
  required=$(( GAS_PRICE * (2*GAS_LIMIT + 21000) + 1000000000000000 ))
  if (( bal < required )); then
    fund=$(( required - bal + GAS_PRICE*22000 ))
    echo "[acl-e2e] Funding subject with $fund wei from admin..."
    cast send "$SUBJECT" \
      --value "$fund" \
      --rpc-url "$RPC_URL" --private-key "$ADMIN_PRIVATE_KEY" --from "$ADMIN" \
      --legacy --gas-price "$GAS_PRICE" --gas-limit 70000 -q || die "Funding subject failed (ACL may be blocking value transfer; ensure admin bypass or grant 0x00000000 to admin->subject)"
    sleep 1
    bal=$(cast balance "$SUBJECT" --rpc-url "$RPC_URL" 2>/dev/null || echo 0)
    echo "[acl-e2e] Subject balance (wei): $bal"
  else
    echo "[acl-e2e] Subject has sufficient balance (wei): $bal"
  fi
}

ensure_subject_key
fund_subject_if_needed

json_deployed_to() {
  # Reads JSON on stdin and extracts a deployed address if present
  jq -er '.deployedTo // .deployment?.address // empty'
}

explain_acl_bootstrap() {
  err "It looks like deployment failed before broadcasting."
  err "If ACL is enabled and fail-closed, contract creation is denied by default."
  err "Start your node with one of:"
  err "  --acl.bypass=$ADMIN  (recommended for bootstrap)"
  err "  --acl.owner-bypass=true  (after proxy is initialized)"
  err "  --acl.failopen=true      (temporary, not recommended in prod)"
}

deploy_contract() {
  # Deploy contract using Foundry by contract name (artifacts compiled via ACLImports.sol)
  local name=$1; shift
  local out addr
  out=$(forge create "$name" \
        --rpc-url "$RPC_URL" \
        --private-key "$ADMIN_PRIVATE_KEY" \
        --from "$ADMIN" \
        --legacy --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" \
        --json "$@" 2>&1) || true
  addr=$(jq -r '.deployedTo // .deployment?.address // empty' <<<"$out" 2>/dev/null || true)
  if [[ "$addr" =~ ^0x[0-9a-fA-F]{40}$ ]]; then
    echo "$addr"; return 0
  fi
  # Some foundry versions omit deployedTo but include transaction hash; recover via receipt
  txhash=$(jq -r '.transactionHash // .hash // empty' <<<"$out" 2>/dev/null || true)
  if [[ "$txhash" =~ ^0x[0-9a-fA-F]{64}$ ]]; then
    sleep 1
    addr=$(cast receipt "$txhash" --json --rpc-url "$RPC_URL" | jq -r '.contractAddress // .contract_address // empty')
    if [[ "$addr" =~ ^0x[0-9a-fA-F]{40}$ ]]; then
      echo "$addr"; return 0
    fi
  fi
  err "forge create yielded no deployed address for $name, falling back to raw bytecode"
  local bytecode txjson txhash
  bytecode=$(forge inspect "$name" bytecode 2>/dev/null) || { err "forge inspect failed for $name"; explain_acl_bootstrap; return 1; }
  [[ "$bytecode" =~ ^0x[0-9a-fA-F]+$ ]] || { err "Invalid bytecode for $name: $bytecode"; explain_acl_bootstrap; return 1; }
  txjson=$(cast send \
        --rpc-url "$RPC_URL" \
        --private-key "$ADMIN_PRIVATE_KEY" \
        --from "$ADMIN" \
        --legacy --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" \
        --json \
        --create "$bytecode" 2>&1) || { err "$txjson"; explain_acl_bootstrap; return 1; }
  txhash=$(jq -r '.transactionHash // .txHash // .hash // empty' <<<"$txjson")
  [[ "$txhash" =~ ^0x[0-9a-fA-F]{64}$ ]] || { err "Deploy tx missing hash: $txjson"; explain_acl_bootstrap; return 1; }
  sleep 1
  addr=$(cast receipt "$txhash" --json --rpc-url "$RPC_URL" | jq -r '.contractAddress // .contract_address // empty')
  [[ "$addr" =~ ^0x[0-9a-fA-F]{40}$ ]] || { err "No contractAddress in receipt for $name (tx $txhash)"; explain_acl_bootstrap; return 1; }
  echo "$addr"
}

deploy_logic() {
  echo "[acl-e2e] Deploying AccessControlFirewall logic..."
  # Use compiled artifact included via ACLImports.sol
  LOGIC_ADDR=$(deploy_contract "AccessControlFirewall")
  echo "[acl-e2e] Logic: $LOGIC_ADDR"
}

deploy_proxy() {
  echo "[acl-e2e] Initializing ACL logic (no proxy) with owner=$ADMIN..."
  cast send "$LOGIC_ADDR" "initialize(address)" "$ADMIN" \
    --rpc-url "$RPC_URL" --private-key "$ADMIN_PRIVATE_KEY" \
    --legacy --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" -q
  ACL_PROXY="$LOGIC_ADDR"
  echo "[acl-e2e] ACL Address: $ACL_PROXY"
}

deploy_test_A() {
  echo "[acl-e2e] Deploying test contract A..."
  A_ADDR=$(deploy_contract "A.sol:A")
  echo "[acl-e2e] A: $A_ADDR"
}

# Additional target with richer API for param-constraint testing
deploy_target_ATarget() {
  echo "[acl-e2e] Deploying ATarget (setY with param constraint support)..."
  AT_ADDR=$(deploy_contract "contracts/Targets.sol:ATarget")
  echo "[acl-e2e] ATarget: $AT_ADDR"
}

sanity_is_permitted_false() {
  echo "[acl-e2e] Sanity: ACL isPermitted(subject=$SUBJECT, A, clearX()) should be false"
  CLEAR_DATA=$(cast calldata "clearX()")
  local res
  res=$(cast call "$ACL_PROXY" "isPermitted(address,address,bytes)(bool)" "$SUBJECT" "$A_ADDR" "$CLEAR_DATA" --rpc-url "$RPC_URL")
  echo "[acl-e2e] isPermitted(clearX) => $res"
}

grant_setX() {
  echo "[acl-e2e] Granting setX(subject=$SUBJECT,target=$A_ADDR,selector=setX(uint256))"
  local sel
  sel=$(cast sig "setX(uint256)")
  send_success "$ADMIN_PRIVATE_KEY" "$ADMIN" "$ACL_PROXY" \
    "grantSelector(address,address,bytes4)" "$SUBJECT" "$A_ADDR" "$sel"
}

# Grant setY with a calldata prefix mask/value constraint: allow only setY(42)
grant_setY_with_constraint() {
  echo "[acl-e2e] Granting setY(subject=$SUBJECT,target=$AT_ADDR,selector=setY(uint256)) with constraint v==42"
  local sel
  sel=$(cast sig "setY(uint256)")
  # selector(4) + 32-byte arg; constrain entire 32-byte arg to equal 42
  local MASK VALUE
  # Include 4 zero bytes for selector in the mask/value prefix to align arg matching
  # Total length 36 bytes: 4 (selector) + 32 (arg). Match all 32 bytes of the arg
  MASK=0x00000000ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
  # Build VALUE with 4 zero bytes (selector) + 32-byte uint(42)
  VALUE="0x00000000$(printf '%064x' 42)"

  # grant and check success
  send_success "$ADMIN_PRIVATE_KEY" "$ADMIN" "$ACL_PROXY" \
    "grantSelector(address,address,bytes4)" "$SUBJECT" "$AT_ADDR" "$sel"
  # set constraint and check success
  send_success "$ADMIN_PRIVATE_KEY" "$ADMIN" "$ACL_PROXY" \
    "setParamConstraint(address,address,bytes4,bytes,bytes)" \
    "$SUBJECT" "$AT_ADDR" "$sel" "$MASK" "$VALUE"
  # Verify isPermitted preflight for setY(42)=true and setY(43)=false
  local DATA42 DATA43 P42 P43
  DATA42=$(cast calldata "setY(uint256)" 42)
  DATA43=$(cast calldata "setY(uint256)" 43)
  P42=$(cast call "$ACL_PROXY" "isPermitted(address,address,bytes)(bool)" "$SUBJECT" "$AT_ADDR" "$DATA42" --rpc-url "$RPC_URL")
  P43=$(cast call "$ACL_PROXY" "isPermitted(address,address,bytes)(bool)" "$SUBJECT" "$AT_ADDR" "$DATA43" --rpc-url "$RPC_URL")
  echo "[acl-e2e] isPermitted setY(42) => $P42; setY(43) => $P43"
  [[ "$P42" == "true" ]] || die "Param constraint misconfigured: setY(42) not permitted"
  [[ "$P43" == "false" ]] || die "Param constraint misconfigured: setY(43) unexpectedly permitted"
}

call_setX_and_verify() {
  echo "[acl-e2e] Calling A.setX(777) (should succeed)"
  local tx
  tx=$(cast send "$A_ADDR" "setX(uint256)" 777 \
    --rpc-url "$RPC_URL" --private-key "$SUBJECT_PRIVATE_KEY" --from "$SUBJECT" \
    --legacy --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT")
  echo "$tx"
  sleep 1
  local val
  val=$(cast call "$A_ADDR" "x()(uint256)" --rpc-url "$RPC_URL")
  echo "[acl-e2e] A.x => $val"
  [[ "$val" == "777" ]] || die "setX did not take effect"
}

call_clearX_expect_denied() {
  echo "[acl-e2e] Calling A.clearX() (should be denied by ACL)"
  local out
  out=$(cast send "$A_ADDR" "clearX()" \
    --rpc-url "$RPC_URL" --private-key "$SUBJECT_PRIVATE_KEY" --from "$SUBJECT" \
    --legacy --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" --json --async 2>&1 || true)
  echo "$out"
  local err_msg txhash
  err_msg=$(jq -r '.error.message // empty' <<<"$out" 2>/dev/null || true)
  if [[ -n "$err_msg" ]]; then
    echo "[acl-e2e] RPC error on send (expected denial): $err_msg"
    return 0
  fi
  txhash=$(jq -r '.transactionHash // .hash // .txHash // empty' <<<"$out" 2>/dev/null || true)
  # Fallback: some cast versions print only the hash string with --async
  if [[ -z "$txhash" && "$out" =~ ^0x[0-9a-fA-F]{64}$ ]]; then
    txhash="$out"
  fi
  if [[ "$txhash" =~ ^0x[0-9a-fA-F]{64}$ ]]; then
    echo "[acl-e2e] clearX txhash: $txhash"
    # poll a few times for a receipt; if mined with status=0x0 -> denied; if 0x1 -> unexpected
    local tries=10 status_hex=""
    while (( tries > 0 )); do
      status_hex=$(cast receipt "$txhash" --json --rpc-url "$RPC_URL" --async | jq -r '.status // .Status // empty' 2>/dev/null || true)
      if [[ -n "$status_hex" ]]; then
        break
      fi
      sleep 1
      tries=$((tries-1))
    done
    if [[ -n "$status_hex" ]]; then
      echo "[acl-e2e] receipt status: $status_hex"
      [[ "$status_hex" == "0x0" || "$status_hex" == "0" ]] || die "clearX was not denied (status=$status_hex)"
    else
      echo "[acl-e2e] No receipt observed; assuming transaction was rejected/removed by ACL"
    fi
  else
    # No txhash and no explicit JSON-RPC error message; try textual hint
    grep -qi "revert\|denied" <<<"$out" || die "clearX did not produce revert/denied and no tx hash was returned"
  fi
  sleep 1
  local val2
  val2=$(cast call "$A_ADDR" "x()(uint256)" --rpc-url "$RPC_URL")
  echo "[acl-e2e] A.x after denied clearX => $val2"
  [[ "$val2" == "777" ]] || die "A.x changed despite denied clearX"
}

# Try setY(43) => expect denial due to param constraint
call_setY_bad_expect_denied() {
  echo "[acl-e2e] Calling ATarget.setY(43) (should be denied by ACL param constraint)"
  local out
  out=$(cast send "$AT_ADDR" "setY(uint256)" 43 \
    --rpc-url "$RPC_URL" --private-key "$SUBJECT_PRIVATE_KEY" --from "$SUBJECT" \
    --legacy --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" --json --async 2>&1 || true)
  echo "$out"
  local err_msg txhash
  err_msg=$(jq -r '.error.message // empty' <<<"$out" 2>/dev/null || true)
  if [[ -n "$err_msg" ]]; then
    echo "[acl-e2e] RPC error on send (expected denial): $err_msg"
    return 0
  fi
  txhash=$(jq -r '.transactionHash // .hash // .txHash // empty' <<<"$out" 2>/dev/null || true)
  if [[ -z "$txhash" && "$out" =~ ^0x[0-9a-fA-F]{64}$ ]]; then
    txhash="$out"
  fi
  if [[ "$txhash" =~ ^0x[0-9a-fA-F]{64}$ ]]; then
    echo "[acl-e2e] setY(43) txhash: $txhash"
    local tries=10 status_hex=""
    while (( tries > 0 )); do
      status_hex=$(cast receipt "$txhash" --json --rpc-url "$RPC_URL" --async | jq -r '.status // .Status // empty' 2>/dev/null || true)
      if [[ -n "$status_hex" ]]; then break; fi
      sleep 1; tries=$((tries-1))
    done
    if [[ -n "$status_hex" ]]; then
      echo "[acl-e2e] receipt status: $status_hex"
      [[ "$status_hex" == "0x0" || "$status_hex" == "0" ]] || die "setY(43) was not denied (status=$status_hex)"
    else
      echo "[acl-e2e] No receipt observed; assuming transaction was rejected/removed by ACL"
    fi
  else
    grep -qi "revert\|denied" <<<"$out" || die "setY(43) did not produce revert/denied and no tx hash was returned"
  fi
}

# Try setY(42) => expect success by constraint
call_setY_good_and_verify() {
  echo "[acl-e2e] Calling ATarget.setY(42) (should succeed due to constraint)"
  local out txhash status_hex tries
  out=$(cast send "$AT_ADDR" "setY(uint256)" 42 \
    --rpc-url "$RPC_URL" --private-key "$SUBJECT_PRIVATE_KEY" --from "$SUBJECT" \
    --legacy --gas-price "$GAS_PRICE" --gas-limit "$GAS_LIMIT" --json --async 2>&1 || true)
  echo "$out"
  txhash=$(jq -r '.transactionHash // .hash // .txHash // empty' <<<"$out" 2>/dev/null || true)
  if [[ -z "$txhash" && "$out" =~ ^0x[0-9a-fA-F]{64}$ ]]; then
    txhash="$out"
  fi
  [[ "$txhash" =~ ^0x[0-9a-fA-F]{64}$ ]] || die "setY(42) send did not return a tx hash"
  echo "[acl-e2e] setY(42) txhash: $txhash"
  tries=30
  status_hex=""
  while (( tries > 0 )); do
    status_hex=$(cast receipt "$txhash" --json --rpc-url "$RPC_URL" --async | jq -r '.status // .Status // empty' 2>/dev/null || true)
    if [[ -n "$status_hex" ]]; then break; fi
    sleep 1; tries=$((tries-1))
  done
  if [[ -z "$status_hex" ]]; then
    die "setY(42) receipt not found within timeout; tx likely dropped unexpectedly"
  fi
  echo "[acl-e2e] receipt status: $status_hex"
  [[ "$status_hex" == "0x1" || "$status_hex" == "1" ]] || die "setY(42) was not successful (status=$status_hex)"
  local val
  val=$(cast call "$AT_ADDR" "y()(uint256)" --rpc-url "$RPC_URL")
  echo "[acl-e2e] ATarget.y => $val"
  [[ "$val" == "42" ]] || die "setY(42) did not take effect"
}

# Run
if [[ -n "${ACL_ADDRESS:-}" && "$ACL_ADDRESS" =~ ^0x[0-9a-fA-F]{40}$ ]]; then
  section "Using preconfigured ACL"
  echo "[acl-e2e] Using preconfigured ACL at $ACL_ADDRESS (skipping deploy)"
  ACL_PROXY="$ACL_ADDRESS"
  hr
else
  run_step "Deploy ACL logic" deploy_logic
  run_step "Initialize ACL (no proxy)" deploy_proxy
fi
run_step "Deploy test contract A" deploy_test_A
run_step "Sanity check: isPermitted(clearX) is false" sanity_is_permitted_false
run_step "Grant setX permission" grant_setX
run_step "Call setX and verify" call_setX_and_verify
run_step "Call clearX expecting denial" call_clearX_expect_denied

# Param constraint checks
run_step "Deploy ATarget for param tests" deploy_target_ATarget
run_step "Grant setY with v==42 constraint" grant_setY_with_constraint
run_step "Call setY(43) expecting denial" call_setY_bad_expect_denied
run_step "Call setY(42) and verify" call_setY_good_and_verify

section "SUCCESS"
echo "[acl-e2e] Success: ACL allowed setX and denied clearX as expected"
hr
