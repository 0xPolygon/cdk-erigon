// SPDX-License-Identifier: MIT
pragma solidity 0.8.27;

import {ICrossChainProofValidator} from "@iden3/contracts/interfaces/ICrossChainProofValidator.sol";
import {IState} from "@iden3/contracts/interfaces/IState.sol";

contract MockCrossChainProofValidator is ICrossChainProofValidator {
    function processGlobalStateProof(
        bytes calldata /*globalStateProof*/
    ) external pure returns (IState.GlobalStateProcessResult memory) {
        return IState.GlobalStateProcessResult({idType: bytes2(0), root: 0, replacedAtTimestamp: 0});
    }

    function processIdentityStateProof(
        bytes calldata /*identityStateProof*/
    ) external pure returns (IState.IdentityStateProcessResult memory) {
        return IState.IdentityStateProcessResult({id: 0, state: 0, replacedAtTimestamp: 0});
    }
}

