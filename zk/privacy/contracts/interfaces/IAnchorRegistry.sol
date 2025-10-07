// SPDX-License-Identifier: MIT
pragma solidity ^0.8.23;

interface IAnchorRegistry {
    function owner() external view returns (address);
    function latestStateRoot(address identity) external view returns (bytes32);
    function setStateRoot(address identity, bytes32 newRoot) external;
}

