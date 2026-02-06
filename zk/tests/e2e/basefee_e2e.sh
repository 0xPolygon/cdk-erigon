#!/usr/bin/env bash
set -euo pipefail

# End-to-end sanity for basefee parity between sequencer and RPC nodes.
# Prereqs:
#  - built ./build/bin/cdk-erigon in repo root
#  - cast, jq available
#
# This script is intentionally verbose and defensive; it is not invoked automatically in CI.

CONFIG_PATH="${CONFIG_PATH:-.}"
CONFIG_YAML="${CONFIG_PATH}/dynamic-integration8.yaml"
CHAIN_SPEC="${CONFIG_PATH}/dynamic-integration-chainspec.json"
DATADIR_SEQ="$(pwd)/e2e-basefee/sequencer"
DATADIR_RPC="$(pwd)/e2e-basefee/rpc"
BIN="$(pwd)/build/bin/cdk-erigon"
SEQ_RPC_URL="http://127.0.0.1:9545"
RPC_RPC_URL="http://127.0.0.1:8545"
SEQ_LOG="$(pwd)/e2e-basefee/sequencer.log"
RPC_LOG="$(pwd)/e2e-basefee/rpc.log"
DATASTREAM_PORT=7003
TORRENT_PORT=7005
DIAG_PORT=7010
AUTHRPC_PORT=8552
PRIVATE_API_ADDR="127.0.0.1:8558"

if [[ ! -f "$CONFIG_YAML" ]]; then
  echo "Config file not found: $CONFIG_YAML" >&2
  exit 1
fi
if [[ ! -f "$CHAIN_SPEC" ]]; then
  echo "Chainspec not found: $CHAIN_SPEC" >&2
  exit 1
fi
if [[ ! -x "$BIN" ]]; then
  echo "Binary not found or not executable: $BIN" >&2
  exit 1
fi

mkdir -p "$(dirname "$SEQ_LOG")" "$DATADIR_SEQ" "$DATADIR_RPC"

stop_seq() {
  if [[ -n "${seq_pid:-}" ]] && kill -0 "$seq_pid" 2>/dev/null; then
    echo "[stop_seq] stopping PID $seq_pid" >&2
    kill -INT "$seq_pid" 2>/dev/null || true
    wait "$seq_pid" 2>/dev/null || true
  fi
  seq_pid=""
}

stop_rpc() {
  if [[ -n "${rpc_pid:-}" ]] && kill -0 "$rpc_pid" 2>/dev/null; then
    echo "[stop_rpc] stopping PID $rpc_pid" >&2
    kill -INT "$rpc_pid" 2>/dev/null || true
    wait "$rpc_pid" 2>/dev/null || true
  fi
  rpc_pid=""
}

stop_all_processes() {
  set +e
  stop_seq
  stop_rpc
  set -e
}
trap stop_all_processes EXIT

cleanup_datadirs() {
  rm -rf "$DATADIR_SEQ" "$DATADIR_RPC"
}

start_seq() {
  echo "[start_seq] begin halt_batch=$1 allow_free=$2"
  local halt_batch=$1
  local allow_free=$2
  local args=(
    --config="$CONFIG_YAML"
    --datadir="$DATADIR_SEQ"
    --http.addr=0.0.0.0 --http.port=9545
    --zkevm.shadow-sequencer=true
    --zkevm.sequencer-block-seal-time=1s
    --zkevm.sequencer-batch-seal-time=10s
    --zkevm.data-stream-host="127.0.0.1"
    --zkevm.data-stream-port="$DATASTREAM_PORT"
    --zkevm.sequencer-halt-on-batch-number="$halt_batch"
    --zkevm.allow-free-transactions="$allow_free"
  )
  echo "[start_seq] args: ${args[*]}" >&2
  CDK_ERIGON_SEQUENCER=1 "$BIN" "${args[@]}" >"$SEQ_LOG" 2>&1 &
  seq_pid=$!
  echo "[start_seq] pid=$seq_pid" >&2
}

start_rpc() {
  echo "[start_rpc] begin" >&2
  local args=(
    --config="$CONFIG_YAML"
    --datadir="$DATADIR_RPC"
    --http --http.addr=0.0.0.0 --http.port=8545
    --authrpc.port="$AUTHRPC_PORT"
    --torrent.port="$TORRENT_PORT"
    --diagnostics.endpoint.port="$DIAG_PORT"
    --private.api.addr="$PRIVATE_API_ADDR"
    --zkevm.l2-sequencer-rpc-url="$SEQ_RPC_URL"
    --zkevm.l2-datastreamer-url="0.0.0.0:7003"
    --ws=false
  )
  echo "[start_rpc] args: ${args[*]}" >&2
  "$BIN" "${args[@]}" >"$RPC_LOG" 2>&1 &
  rpc_pid=$!
  echo "[start_rpc] pid=$rpc_pid" >&2
}

wait_for_block() {
  local url=$1 target=$2
  echo "[wait_for_block] begin url=$url target=$target"
  local attempts=0
  while :; do
    num=$(cast block-number -r "$url" 2>/dev/null || echo "")
    if [[ -n "$num" && "$num" =~ ^[0-9]+$ && "$num" -ge "$target" ]]; then
      echo "[wait_for_block] reached $num"
      break
    fi
    attempts=$((attempts + 1))
    if (( attempts > 180 )); then
      echo "Timed out waiting for $url to reach block $target (last=$num)"
      exit 1
    fi
    sleep 1
  done
  echo "[wait_for_block] end"
}

get_latest_block() {
  local url=$1
  cast block-number -r "$url" 2>/dev/null || echo ""
}

wait_for_block_production() {
  local url=$1
  echo "[wait_for_block_production] begin url=$url"
  local baseline attempts=0
  while :; do
    baseline=$(get_latest_block "$url")
    if [[ -n "$baseline" && "$baseline" =~ ^[2-9]+$ ]]; then
      break
    fi
    attempts=$((attempts + 1))
    if (( attempts > 180 )); then
      echo "[wait_for_block_production] $url not reachable"
      exit 1
    fi
    sleep 1
  done
  echo "[wait_for_block_production] baseline=$baseline"
  attempts=0
  while :; do
    sleep 1
    current=$(get_latest_block "$url")
    if [[ -n "$current" && "$current" =~ ^[0-9]+$ && "$current" -gt "$baseline" ]]; then
      echo "[wait_for_block_production] block production detected ($baseline->$current)"
      echo "[wait_for_block_production] end"
      return
    fi
    attempts=$((attempts + 1))
    if (( attempts > 180 )); then
      echo "[wait_for_block_production] timed out waiting for blocks on $url (last=$current)"
      exit 1
    fi
  done
}

compare_state_root() {
  echo "[compare_state_root] begin block=$1"
  local block=$1
  local seq_root rpc_root
  seq_root=$(cast block "$block" -r "$SEQ_RPC_URL" --json | jq -r .stateRoot)
  rpc_root=$(cast block "$block" -r "$RPC_RPC_URL" --json | jq -r .stateRoot)
  [[ "$seq_root" == "$rpc_root" ]] || { echo "State root mismatch at $block: seq=$seq_root rpc=$rpc_root"; exit 1; }
  echo "[compare_state_root] match at $block: $seq_root"
  echo "[compare_state_root] end"
}

compare_base_fee() {
  echo "[compare_base_fee] begin block=$1"
  local block=$1
  local seq_fee rpc_fee
  seq_fee=$(cast basefee "$block" -r "$SEQ_RPC_URL")
  rpc_fee=$(cast basefee "$block" -r "$RPC_RPC_URL")
  [[ "$seq_fee" == "$rpc_fee" ]] || { echo "BaseFee mismatch at $block: seq=$seq_fee rpc=$rpc_fee"; exit 1; }
  echo "[compare_base_fee] match at $block: $seq_fee"
  echo "[compare_base_fee] end"
}

verify_base_fee_delta() {
  local block=$1 url=$2 multiplier=$3
  echo "[verify_base_fee_delta] begin block=$block url=$url multiplier=$multiplier"
  local parent_json current_json
  parent_json=$(cast block "$((block - 1))" -r "$url" --json)
  current_json=$(cast block "$block" -r "$url" --json)

  decode_int() {
    local v=$1
    if [[ "$v" == 0x* ]]; then
      echo $((v))
    else
      echo $((10#$v))
    fi
  }

  local parent_base_fee current_base_fee parent_gas_used parent_gas_limit
  parent_base_fee=$(decode_int "$(jq -r .baseFeePerGas <<<"$parent_json")")
  current_base_fee=$(decode_int "$(jq -r .baseFeePerGas <<<"$current_json")")
  parent_gas_used=$(decode_int "$(jq -r .gasUsed <<<"$parent_json")")
  parent_gas_limit=$(decode_int "$(jq -r .gasLimit <<<"$parent_json")")

  if [[ -z "$parent_base_fee" || -z "$current_base_fee" || -z "$parent_gas_used" || -z "$parent_gas_limit" ]]; then
    echo "[verify_base_fee_delta] missing basefee/gas data" >&2
    exit 1
  fi

  local elasticity=2 denominator=8
  # convert multiplier string (e.g., 0.01 or 1) into numerator/denominator integers
  local multiplier_num multiplier_den
  if [[ "$multiplier" == *.* ]]; then
    local int_part=${multiplier%%.*}
    local frac_part=${multiplier#*.}
    local den=1
    for ((i=0; i<${#frac_part}; i++)); do den=$((den * 10)); done
    local int_num=0
    [[ -n "$int_part" && "$int_part" != "0" ]] && int_num=$((10#$int_part * den))
    multiplier_num=$((int_num + 10#$frac_part))
    multiplier_den=$den
  else
    multiplier_num=$((10#$multiplier))
    multiplier_den=1
  fi
  local target=$((parent_gas_limit / elasticity))
  local usage_delta=$((parent_gas_used - target))

  abs() { local v=$1; ((v < 0)) && echo $((-v)) || echo $v; }

  # expected_delta = parent_base_fee * usage_delta / target / denominator * multiplier
  local num=$((parent_base_fee * usage_delta * multiplier_num))
  local den=$((target * denominator * multiplier_den))
  local expected_delta
  if (( num >= 0 )); then
    expected_delta=$(( (num + den / 2) / den )) # nearest int
  else
    expected_delta=$(( - ( (-num + den / 2) / den ) ))
  fi

  local actual_delta=$((current_base_fee - parent_base_fee))
  local tolerance_base=$(( $(abs "$expected_delta") / 20 )) # 5%
  local tolerance=$((tolerance_base > 1 ? tolerance_base : 1))
  local diff_val=$((actual_delta - expected_delta))
  local diff=$(abs "$diff_val")

  echo "[verify_base_fee_delta] parent_base_fee=$parent_base_fee current_base_fee=$current_base_fee"
  echo "[verify_base_fee_delta] parent_gas_used=$parent_gas_used parent_gas_limit=$parent_gas_limit target=$target"
  echo "[verify_base_fee_delta] expected_delta=$expected_delta actual_delta=$actual_delta tolerance=$tolerance"

  if (( diff > tolerance )); then
    echo "[verify_base_fee_delta] BaseFee delta outside tolerance" >&2
    exit 1
  fi
  echo "[verify_base_fee_delta] end"
}

send_txs() {
  echo "[send_txs] begin url=$1 count=$2"
  local url=$1 count=$2

  local from start_nonce
  from=$(cast wallet address --private-key "$PRIVATE_KEY")
  start_nonce=$(cast nonce "$from" -r "$url")

  if [[ -z "$start_nonce" || ! "$start_nonce" =~ ^[0-9]+$ ]]; then
    echo "[send_txs] unable to fetch nonce for $from on $url" >&2
    exit 1
  fi

  for ((i=0; i<count; i++)); do
    local args=(
      -q
      --async
      --nonce $((start_nonce + i))
      --private-key "$PRIVATE_KEY"
      --value 0
      --gas-limit 21000
      0x0000000000000000000000000000000000000000
      -r "$url"
    )
    cast send "${args[@]}" || true
  done
  echo "[send_txs] end"
}

# Phase 1: allow-free, halt at batch 10
cleanup_datadirs
echo "== Phase 1: start sequencer (allow-free) halt@10 =="
start_seq 10 true
wait_for_block_production "$SEQ_RPC_URL"
echo "== Start RPC node =="
start_rpc
wait_for_block_production "$RPC_RPC_URL"

# Send a test tx to avoid NaN response due to no fee history
send_txs "$RPC_RPC_URL" 1
sleep 3

echo "Sending 10 txs (EIP-1559) via RPC node"
send_txs "$RPC_RPC_URL" 10

phase1_target=30
wait_for_block "$SEQ_RPC_URL" "$phase1_target"
wait_for_block "$RPC_RPC_URL" "$phase1_target"

echo "== Compare state root/basefee in Phase 1 =="
compare_state_root "$phase1_target"
compare_base_fee "$phase1_target"

stop_seq

# Phase 2: disable allow-free, halt at batch 20
echo "== Phase 2: start sequencer (paid) halt@20 =="
start_seq 20 false
wait_for_block_production "$SEQ_RPC_URL"
echo "Sending 1000 txs via RPC node (async)"
send_txs "$RPC_RPC_URL" 100
phase2_target=60
wait_for_block "$SEQ_RPC_URL" "$phase2_target"
wait_for_block "$RPC_RPC_URL" "$phase2_target"

latest=$(cast block latest -r "$SEQ_RPC_URL" --json | jq -r .number)
echo "== Compare state root/basefee in Phase 2 (latest) =="
compare_state_root "$latest"
compare_base_fee "$latest"

stop_seq

# Phase 3: modify chainspec multiplier future block and restart
future_block=$((latest + 100))
tmp_spec="$(mktemp)"
jq ".baseFeeChangeMultipliers.\"$future_block\"=0.01" "$CHAIN_SPEC" >"$tmp_spec"
mv "$tmp_spec" "$CHAIN_SPEC"

start_seq 20 false
phase3_target=$((future_block + 20))
phase3_check=$((future_block + 15))
phase3_start=$((future_block + 5))
wait_for_block "$SEQ_RPC_URL" "$phase3_start"
echo "Sending 5000 txs to exercise new multiplier"
send_txs "$RPC_RPC_URL" 5000
wait_for_block "$SEQ_RPC_URL" "$phase3_target"
wait_for_block "$RPC_RPC_URL" "$phase3_target"

compare_state_root "$phase3_check"
compare_base_fee "$phase3_check"
verify_base_fee_delta "$phase3_check" "$RPC_RPC_URL" 0.01

stop_rpc

echo "Restart RPC node fresh to ensure it syncs tip"
rm -rf "$DATADIR_RPC"
start_rpc
wait_for_block "$RPC_RPC_URL" "$phase3_target"
compare_state_root "$phase3_check"
compare_base_fee "$phase3_check"

echo "Basefee e2e test completed OK"

stop_seq
stop_rpc


echo "== Waiting for background processes to exit =="
# Use explicit PIDs because command substitutions run functions in subshells (jobs would not be tracked).
pids=()
[[ -n "${seq_pid:-}" ]] && pids+=("$seq_pid")
[[ -n "${rpc_pid:-}" ]] && pids+=("$rpc_pid")
if ((${#pids[@]} > 0)); then
  echo "Waiting on: ${pids[*]}"
  wait "${pids[@]}" || true
else
  echo "No background processes recorded"
fi
