// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

contract Guard {
    event Written(address indexed caller);

    function write() external {
        emit Written(msg.sender);
    }
}
