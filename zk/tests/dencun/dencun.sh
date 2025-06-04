#!/bin/bash

RPC_URL=$1
PRIVATE_KEY=$2
RUNDIR=$(dirname "$0")
CONTRACTS_DIR="$RUNDIR/../../debug_tools/test-contracts/contracts"

run() {
    local func_name=$1
    shift  # shift off the function name, leave only the arguments
    echo "--------------- Starting $func_name ---------------"
    $func_name "$@"
    local result=$?

    if [ $result -ne 0 ]; then
        echo "--------------- $func_name failed with exit code $result ---------------"
        exit $result
    else
        echo "--------------- Completed $func_name ---------------"
    fi
    return $result
}

# ------------------------------------
# EIP-6780: https://eips.ethereum.org/EIPS/eip-6780 Do not delete the contract
# EIP-4758: https://eips.ethereum.org/EIPS/eip-4758 Call SENDALL instead
# ------------------------------------
testSendAllEIP4758EIP6780() {
    local RPC_URL=$1
    local RECIPIENT=0x0123456789abcdef0123456789abcdef01234567
    $RUNDIR/test_selfdestruct.sh --rpc-url $RPC_URL --private-key $PRIVATE_KEY --recipient $RECIPIENT --contract $CONTRACTS_DIR/selfdestruct.sol:SelfDestruct

    if [ $? -ne 0 ]; then
        echo "SENDALL test failed."
        return 1
    fi
}

# ------------------------------------
# EIP 4844: https://eips.ethereum.org/EIPS/eip-4844 Point eval precompile only (L2 does not support blobs)
# ------------------------------------
testPointEvalPrecompileEIP4844() {
    local RPC_URL=$1
    $RUNDIR/test_precompile_prague_pointeval.sh --rpc-url $RPC_URL

    if [ $? -ne 0 ]; then
        echo "Point eval precompile test failed."
        return 1
    fi
}

# ------------------------------------
# EIP 5656: https://eips.ethereum.org/EIPS/eip-5656 MCOPY
# ------------------------------------
testMCopyEIP5656() {
    local RPC_URL=$1
    CONTRACT=$(forge create $CONTRACTS_DIR/MCopy.sol:MinimalMCopy --broadcast --rpc-url $RPC_URL --private-key $PRIVATE_KEY --json | jq -r '.deployedTo')
    if [ -z "$CONTRACT" ]; then
        echo "Failed to deploy MCopy contract."
        return 1
    fi

    echo "MCopy contract deployed at: $CONTRACT"

    EXPECTED_DATA="0x01020304"
    DATA=$(cast call $CONTRACT "copy(bytes)(bytes)" $EXPECTED_DATA -r $RPC_URL)
    if [ -z "$DATA" ]; then
        echo "MCOPY data verification failed: no data returned."
        return 1
    fi

    echo "MCOPY data returned: $DATA"

    if [ "$DATA" != $EXPECTED_DATA ]; then
        echo "MCOPY data verification failed: expected $EXPECTED_DATA, got $DATA"
        return 1
    fi

    echo "MCOPY data verification successful"
}

echo "=============== Running Dencun tests ==============="

run testSendAllEIP4758EIP6780 "$RPC_URL"
run testPointEvalPrecompileEIP4844 "$RPC_URL"
run testMCopyEIP5656 "$RPC_URL"

echo "=============== Dencun tests completed ==============="

