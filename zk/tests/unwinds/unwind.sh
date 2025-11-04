#!/bin/bash

set -euo pipefail
set -o errtrace

logFile="script.log"

exec > >(tee -i "$logFile")
exec 2> >(tee -a "$logFile" >&2)

SECONDS=0

log() {
    echo "[$(date)] $*"
}

log_err() {
    echo "[$(date)] $*" >&2
}

ERR_SUPPRESS=0

handle_err() {
    local exit_code=$?
    if (( ERR_SUPPRESS > 0 )); then
        return
    fi
    local err_line=${BASH_LINENO[0]:-0}
    log_err "Error at line ${err_line}: ${BASH_COMMAND} (exit ${exit_code})"
    stop_all_processes
}

configPath="."
sequencerConfigFile="${configPath}/dynamic-integration8.yaml"
rpcConfigFile="${configPath}/dynamic-integration8.yaml"
dataPath="./datadir"
sequencerDataDir="${dataPath}/sequencer"
rpcDataDir="${dataPath}/rpc-datadir"
datastreamBasePath="${dataPath}/datastream"
datastreamBackupRoot="${datastreamBasePath}/generated"
sequencerLogDir="${dataPath}/logs"
sequencerLogFile="${sequencerLogDir}/sequencer.log"
sequencerDataStreamPort=7003
datastreamHostPort=6900
rpcDatastreamHostPort=6901
SEQUENCER_RPC_PORT="${SEQUENCER_RPC_PORT:-8123}"
RPC_URL="${RPC_URL:-http://127.0.0.1:${SEQUENCER_RPC_PORT}}"
TXS_PER_ROUND="${TXS_PER_ROUND:-30}"
SLEEP_BETWEEN_TXS="${SLEEP_BETWEEN_TXS:-0.2}"
SMT_V2_ONLY="${SMT_V2_ONLY:-true}"

mkdir -p "$sequencerLogDir"
: > "$sequencerLogFile"

sequencer_pid=""
dspid=""
first_stop_block=""
first_stop_batch=""
second_stop_block=""
second_stop_batch=""
datastreamSnapshotDir=""
datastreamBinaryPath=""
unwindBatch=""
firstStop=""
secondStop=""

ensure_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        log_err "Required command '$1' not found in PATH"
        exit 1
    fi
}

# Stop all background processes we may have started, without deleting data
stop_all_processes() {
    # prevent recursive traps
    set +e
    if [[ -n "${sequencer_pid:-}" ]] && kill -0 "$sequencer_pid" 2>/dev/null; then
        log "Stopping sequencer (PID $sequencer_pid)"
        kill -INT "$sequencer_pid" 2>/dev/null || true
        wait "$sequencer_pid" 2>/dev/null || true
    fi
    if [[ -n "${dspid:-}" ]] && kill -0 "$dspid" 2>/dev/null; then
        log "Stopping datastream host (PID $dspid)"
        kill -INT "$dspid" 2>/dev/null || kill -TERM "$dspid" 2>/dev/null || true
        wait "$dspid" 2>/dev/null || true
    fi
    set -e
}

cleanup() {
    log "Cleaning up..."
    stop_all_processes
    rm -rf "$dataPath"
    log "Total execution time: $SECONDS seconds"
}


cast_rpc_result() {
    local response exit_code parsed error_payload
    ERR_SUPPRESS=$((ERR_SUPPRESS+1))
    trap 'ERR_SUPPRESS=$((ERR_SUPPRESS-1)); trap - RETURN' RETURN
    local attempt=1
    local max_attempts=10
    local sleep_interval=5
    while true; do
        set +e
        response=$(cast rpc --rpc-url "$RPC_URL" "$@" 2>&1)
        exit_code=$?
        set -e

        response=$(printf "%s" "$response" | tr -d '\r')
        if (( exit_code != 0 )); then
            log_err "cast_rpc_result attempt $attempt for ($*) failed: $response"
        fi

        if (( exit_code == 0 )); then
            break
        fi

        if (( attempt >= max_attempts )); then
            log_err "cast rpc error for '$*' after $attempt attempts: $response"
            return 1
        fi

        log_err "cast rpc error for '$*'; retrying in ${sleep_interval}s..."
        sleep "$sleep_interval"
        ((attempt+=1))
    done

    if [[ -z "$response" ]]; then
        return 0
    fi

    if [[ "${response}" == Error:* ]]; then
        log_err "RPC call '$*' returned error: $response"
        return 1
    fi

    if [[ "${response}" == \{* || "${response}" == \[* ]]; then
        if command -v jq >/dev/null 2>&1; then
            set +e
            error_payload=$(jq -cer 'try .error // empty' <<<"$response")
            parsed=$(jq -er 'try .result // empty' <<<"$response")
            exit_code=$?
            set -e
            if [[ -n "$error_payload" && "$error_payload" != "null" ]]; then
                log_err "RPC call '$*' returned JSON error: $error_payload"
                return 1
            fi
            if (( exit_code == 0 )) && [[ -n "$parsed" ]]; then
                response=$parsed
            elif (( exit_code != 0 )); then
                log_err "Failed to parse RPC response for '$*': $response"
                return 1
            fi
        else
            log "Received JSON payload for '$*'; jq not available, returning raw response"
        fi
    fi

    response=$(printf "%s" "$response" | tr -d '\n')

    if [[ "$response" =~ ^\".*\"$ ]]; then
        response=${response#\"}
        response=${response%\"}
    fi

    if [[ "$response" =~ ^(0x[0-9a-fA-F]+|[0-9]+)$ ]]; then
        echo "$response"
        return 0
    fi

    if [[ "$response" == "null" ]]; then
        return 1
    fi

    if [[ -n "$response" ]]; then
        echo "$response"
        return 0
    fi

    return 1
}

wait_for_rpc() {
    log "Waiting for RPC endpoint $RPC_URL to become ready"
    local retries=0
    local max_retries=300
    local sleep_interval=5
    while true; do
        local attempt=$((retries + 1))
        log "Checking RPC readiness (attempt $attempt)..."
        local block_hex=""
        if block_hex=$(cast_rpc_result eth_blockNumber); then
            log "RPC readiness attempt $attempt returned block value '$block_hex'"
            local block_dec=0
            if [[ -n "$block_hex" ]]; then
                if ! block_dec=$(printf '%d' "$block_hex" 2>/dev/null); then
                    log_err "RPC readiness attempt $attempt could not parse block value '$block_hex'; retrying"
                    sleep 1
                    ((retries+=1))
                    continue
                fi
            fi
            if (( block_dec >= 2 )); then
                log "RPC endpoint $RPC_URL is ready at block $block_dec"
                return
            fi
            log "RPC endpoint $RPC_URL responded but block height $block_dec < 2; retrying"
        else
            log_err "RPC readiness attempt $attempt failed; retrying"
        fi
        sleep "$sleep_interval"
        ((retries+=1))
        if (( retries > max_retries )); then
            log_err "RPC endpoint $RPC_URL did not become ready in time"
            exit 1
        fi
    done
    log "RPC endpoint $RPC_URL is not ready"
}

get_latest_block() {
    local result
    result=$(cast_rpc_result eth_blockNumber || true)
    if [[ -z "$result" ]]; then
        echo 0
    else
        echo $((result))
    fi
}

get_latest_batch() {
    local result
    result=$(cast_rpc_result zkevm_batchNumber || true)
    if [[ -z "$result" ]]; then
        echo 0
    else
        echo $((result))
    fi
}

get_batch_by_block() {
    local block=$1
    local hex_block
    hex_block=$(printf "0x%x" "$block")
    local result
    result=$(cast_rpc_result zkevm_batchNumberByBlockNumber "$hex_block" || true)
    if [[ -z "$result" ]]; then
        echo -1
    else
        echo $((result))
    fi
}

wait_for_block_production() {
    log "Waiting for block production on $RPC_URL"
    wait_for_rpc
    local baseline
    baseline=$(get_latest_block)
    log "Waiting for block production on $RPC_URL (baseline block $baseline)"
    local attempts=0
    local max_attempts=300
    while true; do
        log "Checking for new blocks (attempt $((attempts+1)))..."
        sleep 1
        local current
        current=$(get_latest_block)
        if (( current > baseline )); then
            log "Block production detected at block $current"
            break
        fi
        ((attempts+=1))
        if (( attempts > max_attempts )); then
            log_err "Timed out waiting for block production"
            exit 1
        fi
    done
    log "Block production is active"
}

wait_for_rpc_port() {
    local max_retries=${1:-120}
    local retries=0
    while ! bash -c "</dev/tcp/127.0.0.1/${SEQUENCER_RPC_PORT}" 2>/dev/null; do
        sleep 1
        ((retries+=1))
        if (( retries >= max_retries )); then
            log_err "RPC endpoint $RPC_URL did not start listening on port ${SEQUENCER_RPC_PORT}"
            return 1
        fi
    done
    log "RPC endpoint $RPC_URL is accepting connections"
    return 0
}

send_transactions() {
    log "Preparing to send $1 transactions to $RPC_URL"
    local total=$1
    if (( total <= 0 )); then
        log "Transaction count set to zero; skipping submission"
        return
    fi

    local sender
    sender=$(cast wallet address --private-key "$PRIVATE_KEY")
    local current_nonce
    current_nonce=$(cast nonce --rpc-url "$RPC_URL" "$sender")
    if [[ -z "$current_nonce" ]]; then
        log_err "Failed to retrieve nonce for $sender"
        exit 1
    fi
    log "Submitting $total transactions via cast to $RPC_URL starting from nonce $current_nonce"

    current_nonce=$((current_nonce))

    for ((i=1; i<=total; i++)); do
        local recipient="0x$(openssl rand -hex 20)"
        local attempt=0
        local max_attempts=5
        while true; do
            if cast send \
                --async \
                --legacy \
                --nonce "$current_nonce" \
                --rpc-url "$RPC_URL" \
                --private-key "$PRIVATE_KEY" \
                --gas-price 1000000000 \
                --gas-limit 21000 \
                --value 0.01ether \
                "$recipient" >/dev/null; then
                break
            fi
            ((attempt+=1))
            if (( attempt >= max_attempts )); then
                log_err "Failed to send transaction $i after $max_attempts attempts (nonce $current_nonce)"
                exit 1
            fi
            log "Retrying transaction $i (attempt $((attempt+1))) at nonce $current_nonce"
            sleep 1
        done
        ((current_nonce+=1))
        if (( i % 10 == 0 || i == total )); then
            log "Sent $i/$total transactions"
        fi
        if (( i < total )); then
            sleep "$SLEEP_BETWEEN_TXS"
        fi
    done
    log "All $total transactions submitted"
}

find_last_block_of_batch() {
    local target_batch=$1
    local latest_block
    latest_block=$(get_latest_block)
    local last_block=-1
    for ((block=latest_block; block>=0; block--)); do
        local batch
        batch=$(get_batch_by_block "$block")
        if (( batch == target_batch )); then
            last_block=$block
            break
        fi
    done
    if (( last_block == -1 )); then
        log_err "Unable to locate boundary for batch $target_batch"
        exit 1
    fi
    echo "$last_block"
}

start_sequencer() {
    local halt_batch=${1:-}
    local -a extra_flags=()
    if [[ -n "$halt_batch" ]]; then
        extra_flags+=("--zkevm.sequencer-halt-on-batch-number=$halt_batch")
    fi

    log "Launching sequencer (halt batch: ${halt_batch:-none})"
    CDK_ERIGON_SEQUENCER=1 ./build/bin/cdk-erigon \
        --datadir="$sequencerDataDir" \
        --config="$sequencerConfigFile" \
        --zkevm.shadow-sequencer=true \
        --zkevm.data-stream-host="127.0.0.1" \
        --zkevm.data-stream-port="$sequencerDataStreamPort" \
        --zkevm.sequencer-block-seal-time=100ms \
        --zkevm.sequencer-batch-seal-time=1s \
        ${extra_flags:+"${extra_flags[@]}"} \
        >> "$sequencerLogFile" 2>&1 &
    sequencer_pid=$!
    log "Sequencer started with PID $sequencer_pid"
}

stop_sequencer() {
    if [[ -n "${sequencer_pid:-}" ]] && kill -0 "$sequencer_pid" 2>/dev/null; then
        log "Stopping sequencer (PID $sequencer_pid)"
        kill -INT "$sequencer_pid" 2>/dev/null || true
        wait "$sequencer_pid" 2>/dev/null || true
    fi
    sequencer_pid=""
}

wait_for_batch_reach() {
    local target=$1
    local stable_secs=${2:-2}
    local max_wait=${3:-600}
    local waited=0
    local last
    local current
    local observable_target=$target

    if (( target > 0 )); then
        observable_target=$((target - 1))
    fi

    log "Waiting for batch $target to be reached (observing $observable_target)" >&2

    while (( waited < max_wait )); do
        current=$(get_latest_batch)
        if (( current >= observable_target )); then
            log "Batch observable target reached at $current" >&2
            last=$current
            break
        fi
        log "Current batch $current; waiting for observable target $observable_target" >&2
        sleep 1
        ((waited+=1))
    done

    if (( waited >= max_wait && current < observable_target )); then
        log_err "Timed out waiting for batch $target (observable $observable_target); last observed $current"
        exit 1
    fi

    local stable=0
    while (( stable < stable_secs && waited < max_wait )); do
        sleep 1
        ((waited+=1))
        current=$(get_latest_batch)
        if (( current == last )); then
            ((stable+=1))
        else
            last=$current
            stable=0
            log "Batch advanced to $current; resetting stability counter" >&2
        fi
    done

    if (( stable < stable_secs )); then
        log_err "Timed out waiting for batch stability at $last"
        exit 1
    fi

    echo "$last"
}

stop_datastream_host() {
    local pid=$1
    local timeout=${2:-30}

    if ! kill -0 "$pid" 2>/dev/null; then
        return 0
    fi

    kill -INT "$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
    local waited=0
    while kill -0 "$pid" 2>/dev/null; do
        if (( waited >= timeout )); then
            log "Datastream host (PID $pid) did not exit in ${timeout}s; sending SIGKILL"
            kill -9 "$pid" 2>/dev/null || true
            break
        fi
        sleep 1
        ((waited+=1))
    done
}

dump_data() {
    local stop=$1
    local label=$2
    log "Dumping buckets for $label (block $stop)"
    go run ./cmd/hack --action=dumpAll --chaindata="${rpcDataDir}/chaindata" --output="${dataPath}/${stop}" || {
        log_err "Failed to dump data for $label"
        exit 1
    }
}

is_in_array() {
    local element="$1"
    shift
    for candidate in "$@"; do
        if [[ "$candidate" == "$element" ]]; then
            return 0
        fi
    done
    return 1
}

compare_dumps() {
    local original_dir=$1
    local comparison_dir=$2
    local label=$3
    shift 3
    local expected_diffs=("$@")

    log "Comparing dumps: $original_dir vs $comparison_dir ($label)"
    for file in "$original_dir"/*; do
        local filename
        filename=$(basename "$file")
        local target="$comparison_dir/$filename"

        if [[ ! -f "$target" ]]; then
            log_err "File $filename missing in $comparison_dir"
            exit 1
        fi

        if cmp -s "$file" "$target"; then
            continue
        fi

        if is_in_array "$filename" "${expected_diffs[@]}"; then
            continue
        fi

        log_err "$label - Unexpected differences in $filename"
        diff -u "$file" "$target" >&2 || true
        exit 1
    done
}

backup_datastream_artifacts() {
    local label=$1
    local dest="${datastreamBackupRoot}/${label}"
    log "Backing up datastream artifacts ($label) from $sequencerDataDir to $dest" >&2
    rm -rf "$dest"
    mkdir -p "$dest"

    local data_stream_file=""
    if [[ -f "${sequencerDataDir}/data-stream.bin" ]]; then
        data_stream_file="${sequencerDataDir}/data-stream.bin"
    elif [[ -f "${sequencerDataDir}/data-stream.bn" ]]; then
        data_stream_file="${sequencerDataDir}/data-stream.bn"
    else
        log_err "Datastream binary not found in $sequencerDataDir"
        exit 1
    fi

    if [[ ! -d "${sequencerDataDir}/data-stream.db" ]]; then
        log_err "Datastream DB directory not found in $sequencerDataDir"
        exit 1
    fi

    cp -a "$data_stream_file" "$dest/"
    cp -a "${sequencerDataDir}/data-stream.db" "$dest/"

    echo "$(pwd)/$dest"
}

resolve_datastream_binary() {
    local dir=$1
    if [[ -f "$dir/data-stream.bin" ]]; then
        echo "$dir/data-stream.bin"
        return
    fi
    if [[ -f "$dir/data-stream.bn" ]]; then
        echo "$dir/data-stream.bn"
        return
    fi
    log_err "No datastream binary found in $dir"
    exit 1
}

# Initial cleanup of previous datadir contents
cleanup
# Ensure we stop processes on any error or interrupt, but keep dumps intact
trap handle_err ERR
trap 'log_err "Interrupted"; stop_all_processes; exit 1' SIGINT SIGTERM
trap 'stop_all_processes' EXIT

log "Starting unwind workflow with RPC URL $RPC_URL"

ensure_command cast
ensure_command yq
ensure_command openssl
ensure_command go
ensure_command lsof

mkdir -p "$sequencerLogDir"
mkdir -p "$datastreamBasePath"
mkdir -p "$datastreamBackupRoot"

if [[ -z "${PRIVATE_KEY:-}" ]]; then
    log_err "PRIVATE_KEY environment variable must be set"
    exit 1
fi

chainName=$(yq '.chain' "$rpcConfigFile")
chainName=${chainName//\"/}
chainName=${chainName//\'/}
log "Using chain name: $chainName"

log "Resetting data directory at $dataPath"
rm -rf "$dataPath"
mkdir -p "$sequencerDataDir"
mkdir -p "$sequencerLogDir"
: > "$sequencerLogFile"

# Prime datastream with live sequencer activity
start_sequencer ""
wait_for_block_production
send_transactions "$TXS_PER_ROUND"
sleep 2
latest_batch=$(get_latest_batch)
log "Latest batch after transaction burst: $latest_batch"

stop_sequencer
sleep 3

halt_batch_target=$((latest_batch + 2))
log "Restarting sequencer with halt batch target $halt_batch_target"
start_sequencer "$halt_batch_target"
wait_for_rpc_port
halted_batch=$(wait_for_batch_reach "$halt_batch_target" 2)
second_stop_batch=$halted_batch
second_stop_block=$(get_latest_block)
log "Sequencer halted at batch $second_stop_batch, latest block $second_stop_block"

first_stop_batch=$((second_stop_batch / 2))
if (( first_stop_batch < 1 )); then
    first_stop_batch=1
fi
first_stop_block=$(find_last_block_of_batch "$first_stop_batch")
log "Midpoint stop selected at batch $first_stop_batch, block $first_stop_block"

if [[ -z "$first_stop_block" || -z "$first_stop_batch" ]]; then
    log_err "Failed to capture first stop information"
    exit 1
fi

if [[ -z "$second_stop_block" || -z "$second_stop_batch" ]]; then
    log_err "Failed to capture second stop information"
    exit 1
fi

stop_sequencer
sleep 3

datastreamSnapshotDir=$(backup_datastream_artifacts "halted")
datastreamBinaryPath=$(resolve_datastream_binary "$datastreamSnapshotDir")

firstStop=$first_stop_block
secondStop=$second_stop_block
unwindBatch=$first_stop_batch

log "First stop block: $firstStop (batch $unwindBatch)"
log "Second stop block: $secondStop (batch $second_stop_batch)"
log "Datastream snapshot stored at $datastreamSnapshotDir"
if (( secondStop <= firstStop )); then
    log_err "Second stop block ($secondStop) must be greater than first stop block ($firstStop)"
    exit 1
fi

# Prepare fresh workspace for RPC node
rm -rf "$rpcDataDir"
mkdir -p "$rpcDataDir"
rm -rf "$sequencerDataDir"

# Step 9: start datastream host
existing_pids=$(lsof -ti :"$datastreamHostPort" || true)
if [[ -n "$existing_pids" ]]; then
    log "Terminating existing datastream host processes on port $datastreamHostPort"
    for pid in $existing_pids; do
        kill -9 "$pid" 2>/dev/null || true
    done
fi

log "Starting datastream host on port $datastreamHostPort"
log "Using datastream binary: $datastreamBinaryPath"
go run ./zk/debug_tools/datastream-host --file="$datastreamBinaryPath"  2>&1 &
dspid=$!

log "Waiting for datastream host to accept connections"
waited=0
max_wait=120
while ! bash -c "</dev/tcp/localhost/$datastreamHostPort" 2>/dev/null; do
    if ! kill -0 "$dspid" 2>/dev/null; then
        log_err "Datastream host process $dspid exited unexpectedly"
        exit 1
    fi
    if (( waited >= max_wait )); then
        log_err "Timed out waiting for datastream host on port $datastreamHostPort"
        exit 1
    fi
    sleep 1
    ((waited+=1))
done
log "Datastream host ready (PID $dspid)"

smtV2Only="$SMT_V2_ONLY"

# Step 9 continued: run RPC node to first stop
log "Running cdk-erigon to first stop block $firstStop"
timeout 300 ./build/bin/cdk-erigon \
    --datadir="$rpcDataDir" \
    --config="$rpcConfigFile" \
    --debug.limit="$firstStop" \
    --zkevm.data-stream-port="$rpcDatastreamHostPort" \
    --zkevm.data-stream-host="127.0.0.1" \
    --zkevm.shadow-sequencer=false \
    --zkevm.l1-rpc-url="http://127.0.0.1:6969?chainid=779&endpoint=https://rpc.eu-central-8.gateway.fm/v4/ethereum/non-archival/sepolia"
    

log "Completed sync to first stop"
dump_data "$firstStop" "sync to first stop"

sleep 60

# Step 10 and 11: run to second stop and dump
log "Running cdk-erigon to second stop block $secondStop"
timeout 300 ./build/bin/cdk-erigon \
    --datadir="$rpcDataDir" \
    --config="$rpcConfigFile" \
    --debug.limit="$secondStop" \
    --zkevm.data-stream-port="$rpcDatastreamHostPort" \
    --zkevm.data-stream-host="127.0.0.1" \
    --zkevm.shadow-sequencer=false

log "Completed sync to second stop"
dump_data "$secondStop" "sync to second stop"

# Step 12: unwind to first stop
log "Unwinding to batch $unwindBatch"
go run ./cmd/integration state_stages_zkevm \
    --datadir="$rpcDataDir" \
    --config="$rpcConfigFile" \
    --chain="$chainName" \
    --only-smt-v2="$smtV2Only" \
    --unwind-batch-no="$unwindBatch" || {
    log_err "Failed to execute unwind"
    exit 1
}

log "Completed unwind to batch $unwindBatch"
dump_data "${firstStop}-unwound" "after unwind"

# Compare first stop dumps
different_files=(
    "Code.txt"
    "HashedCodeHash.txt"
    "hermez_l1Sequences.txt"
    "hermez_l1Verifications.txt"
    "HermezSmt.txt"
    "PlainCodeHash.txt"
    "SyncStage.txt"
    "BadHeaderNumber.txt"
    "CallToIndex.txt"
    "bad_tx_hashes_lookup.txt"
    "DbInfo.txt"
)

compare_dumps "$dataPath/${firstStop}" "$dataPath/${firstStop}-unwound" "Unwind Check" "${different_files[@]}"

# Step 13: resync to second stop after unwind
log "Resyncing cdk-erigon to second stop block $secondStop"
timeout 300 ./build/bin/cdk-erigon \
    --datadir="$rpcDataDir" \
    --config="$rpcConfigFile" \
    --debug.limit="$secondStop" \
    --zkevm.data-stream-port="$rpcDatastreamHostPort" \
    --zkevm.data-stream-host="127.0.0.1" \
    --zkevm.shadow-sequencer=false

log "Completed resync to second stop"
dump_data "${secondStop}-sync-again" "after resyncing to second stop"

second_comparison_expected_diffs=(
    "BadHeaderNumber.txt"
    "bad_tx_hashes_lookup.txt"
    "DbInfo.txt"
)

compare_dumps "$dataPath/${secondStop}" "$dataPath/${secondStop}-sync-again" "Sync forward again" "${second_comparison_expected_diffs[@]}"

if [[ -n "${dspid:-}" ]]; then
    if kill -0 "$dspid" 2>/dev/null; then
        log "Stopping datastream host (PID $dspid)"
        stop_datastream_host "$dspid"
    fi
    dspid=""
fi

log "Unwind workflow completed successfully"
