// SPDX-License-Identifier: MIT
pragma solidity ^0.8.18;

contract Storage {
    uint256 private _value;

    mapping(uint256 => uint256) private _slots;

    function setValue(uint256 newValue) external {
        _value = newValue;
    }

    function getValue() external view returns (uint256) {
        return _value;
    }

    function bumpMany(uint256[] calldata keys) external {
        for (uint256 i = 0; i < keys.length; i++) {
            uint256 key = keys[i];
            uint256 val = _slots[key];
            _slots[key] = val + 1;
        }
    }

    function getSlot(uint256 key) external view returns (uint256) {
        return _slots[key];
    }
}
