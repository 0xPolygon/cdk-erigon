// SPDX-License-Identifier: MIT
pragma solidity ^0.8.23;

interface ICredentialValidator {
    /// @notice Validate a credential query proof (circuit-specific semantics)
    /// @param subject Identity (address) the proof pertains to
    /// @param target Application/contract target (optional, app-specific)
    /// @param proof Encoded zk proof bytes
    /// @param pubSignals Encoded circuit public inputs
    /// @return ok True if validation passes
    function validate(
        address subject,
        address target,
        bytes calldata proof,
        bytes calldata pubSignals
    ) external view returns (bool ok);
}

