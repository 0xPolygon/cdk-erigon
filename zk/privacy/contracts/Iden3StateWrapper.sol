// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.27;

import {State as ExternalState} from "@iden3/contracts/state/State.sol";

/// @notice Thin wrapper to reference the audited Iden3 State contract
contract Iden3State is ExternalState {}

