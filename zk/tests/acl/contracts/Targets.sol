// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

// Local targets used by ACL integration tests

contract ATarget {
    uint256 public x;
    uint256 public y;
    uint256 public z;

    event SetX(uint256 v);
    event SetY(uint256 v);
    event SetZ(uint256 v);

    // Simple setter used for allowed-call test
    function setX(uint256 v) external {
        x = v;
        emit SetX(v);
    }

    // Used for param-mask constraint tests
    function setY(uint256 v) external {
        y = v;
        emit SetY(v);
    }

    // Another method, not granted unless explicitly allowed
    function setZ(uint256 v) external {
        z = v;
        emit SetZ(v);
    }

    // A method we never grant in tests to demonstrate selector-deny
    function clearX() external {
        x = 0;
    }
}

contract Dummy {
    uint256 public n;
    constructor() payable {
        n = 1;
    }
}

contract BTarget {
    event NestedCalled(address target);
    event Created(address addr);

    // Calls ATarget.setZ(123) to exercise nested CALL ACL enforcement
    function nestedCall(address a) external {
        ATarget(a).setZ(123);
        emit NestedCalled(a);
    }

    // Deploys a Dummy contract to exercise CREATE ACL enforcement
    function doCreate() external returns (address addr) {
        Dummy d = new Dummy();
        addr = address(d);
        emit Created(addr);
    }

    // A simple function, useful to demonstrate selector-deny
    function noop() external {}
}

