#!/bin/bash
set -e

L2_BRIDGE_ADDRESS="0x4B24266C13AFEf2bb60e2C69A4C08A482d81e3CA"
L2_RPC="http://127.0.0.1:8123"
PRE_MIN_BALANCE="340282366920938463463374607431768211455"

# Query current bridge balance and calculate mined balance
CURRENT_BALANCE=$(cast balance $L2_BRIDGE_ADDRESS --rpc-url $L2_RPC)
MINED_BALANCE=$(echo "$PRE_MIN_BALANCE - $CURRENT_BALANCE" | bc)

# Print formatted log with line breaks for better visualization
echo "=== Bridge Balance Check ==="
echo "RPC:        $L2_RPC"
echo "BRIDGE:     $L2_BRIDGE_ADDRESS"
echo "PRE_MINT:   $PRE_MIN_BALANCE"
echo "CURRENT:    $CURRENT_BALANCE"
echo "MINED:      $MINED_BALANCE, $(echo "scale=18; $MINED_BALANCE / 10^18" | bc) ETH"
echo "=========================="
