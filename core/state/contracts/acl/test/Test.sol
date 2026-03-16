// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

/// @notice Minimal subset of Forge's `Test` contract needed for ACLFlow tests.
contract Test {
    /// @dev Foundry cheatcode proxy.
    Vm internal constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

    function makeAddr(string memory name) internal pure returns (address) {
        return address(uint160(uint256(keccak256(abi.encodePacked(name)))));
    }

    function assertTrue(bool condition) internal pure {
        require(condition, "Test: assertion failed");
    }

    function assertFalse(bool condition) internal pure {
        require(!condition, "Test: assertion failed");
    }
}

/// @dev Minimal interface covering only the cheats used in ACLFlow tests.
interface Vm {
    function prank(address) external;
    function expectEmit(bool checkTopic1, bool checkTopic2, bool checkTopic3, bool checkData) external;
}
