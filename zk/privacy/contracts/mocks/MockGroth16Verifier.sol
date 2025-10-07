// SPDX-License-Identifier: MIT
pragma solidity 0.8.27;

import {IVerifier} from "@iden3/contracts/interfaces/IVerifier.sol";

contract MockGroth16Verifier is IVerifier {
    function verify(
        uint256[2] calldata,
        uint256[2][2] calldata,
        uint256[2] calldata,
        uint256[] calldata
    ) external pure returns (bool) {
        return true;
    }
}
