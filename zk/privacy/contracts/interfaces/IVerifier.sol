// SPDX-License-Identifier: MIT
pragma solidity ^0.8.23;

interface IVerifier {
    /// @notice Verify a zk proof
    /// @param proof Encoded proof bytes (circuit-specific)
    /// @param pubSignals Public inputs (circuit-specific), encoded as bytes
    /// @return ok True if proof is valid
    function verify(bytes calldata proof, bytes calldata pubSignals) external view returns (bool ok);
}

