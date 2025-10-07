// SPDX-License-Identifier: MIT
pragma solidity 0.8.27;

import {IStateTransitionVerifier} from "@iden3/contracts/interfaces/IStateTransitionVerifier.sol";

contract MockStateTransitionVerifier is IStateTransitionVerifier {
    function verifyProof(
        uint256[2] calldata,
        uint256[2][2] calldata,
        uint256[2] calldata,
        uint256[4] calldata
    ) external pure returns (bool r) {
        return true;
    }
}

