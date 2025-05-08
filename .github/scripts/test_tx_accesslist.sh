#!/usr/bin/env bash
set -euo pipefail

RPC_URL="$1"
PRIVATE_KEY="$2"
CONTRACT_ADDR="$3"
CALL_DATA="$4"
NEW_VALUE="$5"

declare -a gas_used_arr

from_addr=$(cast wallet address --private-key "$PRIVATE_KEY")

# get chain info
nonce=$(cast nonce "$from_addr" --rpc-url "$RPC_URL")
chain_id=$(cast chain-id --rpc-url "$RPC_URL")
gas_price=$(cast gas-price --rpc-url "$RPC_URL")

bump_data=$(cast calldata "$CALL_DATA" "$NEW_VALUE")
bump_data=$(cast calldata "bumpMany(uint256[])" "[1,2,3]")

# storage slot
pad() { printf "%064s" "$1" | tr ' ' '0'; }
slot_index_padded=$(pad "$(printf '%x' 1)")
addr_padded=$(pad "${from_addr#0x}")
slot_key=$(cast keccak "0x${addr_padded}${slot_index_padded}")

echo "• from: $from_addr"
echo "• nonce: $nonce"
echo "• chain-id: $chain_id"
echo "• gas-price: $gas_price"
echo "• slot-key: $slot_key"

# access list tx
declare -a names=("no access list" "with access list")
for i in 0 1; do
  echo
  echo "Sending tx #$((i+1)): ${names[i]}"
  # build send command
  cmd=(cast send --async
    --rpc-url "$RPC_URL"
    --private-key "$PRIVATE_KEY"
    --nonce $((nonce + i))
    --gas-price "$gas_price"
    "$CONTRACT_ADDR"
    "$bump_data"
  )
  if (( i == 1 )); then
    # add access-list JSON
    al_json=$(jq -nc \
      --arg a "$CONTRACT_ADDR" \
      --arg k "$slot_key" \
      '[{address:$a,storageKeys:[$k]}]')
    cmd+=(--access-list "$al_json")
  fi

  read -r tx_hash < <("${cmd[@]}")
  tx_hash=${tx_hash//$'\r'/}
  echo "tx hash: $tx_hash"

  gas_used_hex=$(cast receipt "$tx_hash" gasUsed --rpc-url "$RPC_URL")
  ghex=${gas_used_hex#0x}
  gas_used=$((16#$ghex))

  gas_used_arr[$i]=$gas_used

  echo "mined! gas used: $gas_used"
done

no_al=${gas_used_arr[0]}
with_al=${gas_used_arr[1]}

diff=$((no_al - with_al))
if (( diff > 0 )); then
  echo
  echo "Access list saved $diff gas (from $no_al → $with_al)"
else
  diff=$(( -diff ))
  echo
  echo "Access list actually *cost* $diff extra gas (from $no_al → $with_al)"
  exit 1
fi