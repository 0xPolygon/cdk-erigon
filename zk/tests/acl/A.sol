// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

contract A {
    uint256 public x;

    function setX(uint256 v) external {
        x = v;
    }

    function clearX() external {
        x = 0;
    }
}

