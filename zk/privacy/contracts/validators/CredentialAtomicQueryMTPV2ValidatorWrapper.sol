// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.27;

import {CredentialAtomicQueryMTPV2Validator as ExternalMTPV2} from "@iden3/contracts/validators/CredentialAtomicQueryMTPV2Validator.sol";

/// @notice Thin wrapper to reference the audited Iden3 MTP V2 validator
contract CredentialAtomicQueryMTPV2Validator is ExternalMTPV2 {}
