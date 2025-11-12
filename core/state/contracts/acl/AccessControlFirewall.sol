// SPDX-License-Identifier: UNLICENSED
// forge-lint-disable-file unsafe-typecast asm-keccak256
pragma solidity ^0.8.23;

import {IAccessControlFirewall} from "./IAccessControlFirewall.sol";
import {CalldataMask} from "./CalldataMask.sol";

/// @title AccessControlFirewall (ACL) - upgradeable logic contract
/// @notice On-chain ACL using bitmaps for function selectors and optional calldata prefix mask/value constraints.
/// - Permissions are keyed by (subject, target, selector)
/// - Selector permissions are stored in a sparse bitmap: bucket = selector >> 8, bitIndex = selector & 0xff
/// - Optional calldata constraint: (calldata & mask) == value for mask/value length in bytes
/// - Special cases:
///    - value-only transfers (calldata length < 4) use selector 0x00000000
///    - contract creation uses target == address(0), governed by subject->create flag
contract AccessControlFirewall is IAccessControlFirewall {
    using CalldataMask for bytes;

    // --- Ownership / Admin ---
    address public owner;
    bool private _initialized;

    modifier onlyOwner() {
        _onlyOwner();
        _;
    }

    function _onlyOwner() internal view {
        require(msg.sender == owner, "ACL: not owner");
    }

    // --- Selector bitmap storage ---
    // subject => target => bucket => 256-bit bitmap
    mapping(address => mapping(address => mapping(uint256 => uint256))) private _selBitmap;

    // subject => target => allow any selector (fast path)
    mapping(address => mapping(address => bool)) private _anySelector;

    // subject => can create contracts
    mapping(address => bool) private _canCreate;

    // (subject, target, selector) => calldata constraint mask/value (prefix)
    struct Constraint { bytes mask; bytes value; }
    mapping(bytes32 => Constraint) private _constraints;

    // --- Events ---
    event Initialized(address indexed owner);
    event OwnershipTransferred(address indexed oldOwner, address indexed newOwner);
    event SelectorGranted(address indexed subject, address indexed target, bytes4 selector);
    event SelectorRevoked(address indexed subject, address indexed target, bytes4 selector);
    event AnySelectorGranted(address indexed subject, address indexed target);
    event AnySelectorRevoked(address indexed subject, address indexed target);
    event ConstraintSet(address indexed subject, address indexed target, bytes4 indexed selector, bytes mask, bytes value);
    event ConstraintCleared(address indexed subject, address indexed target, bytes4 indexed selector);
    event ContractCreationGranted(address indexed subject);
    event ContractCreationRevoked(address indexed subject);

    // --- Initializer ---
    function initialize(address initialOwner) external {
        require(!_initialized, "ACL: already init");
        require(initialOwner != address(0), "ACL: bad owner");
        owner = initialOwner;
        _initialized = true;
        emit Initialized(initialOwner);
    }

    // --- Owner mgmt ---
    function transferOwnership(address newOwner) external onlyOwner {
        require(newOwner != address(0), "ACL: bad owner");
        address old = owner;
        owner = newOwner;
        emit OwnershipTransferred(old, newOwner);
    }

    // --- Admin ops ---
    function grantSelector(address subject, address target, bytes4 selector) external override onlyOwner {
        _setSelector(subject, target, selector, true);
        emit SelectorGranted(subject, target, selector);
    }

    function revokeSelector(address subject, address target, bytes4 selector) external override onlyOwner {
        _setSelector(subject, target, selector, false);
        emit SelectorRevoked(subject, target, selector);
    }

    function grantAnySelector(address subject, address target) external override onlyOwner {
        require(subject != address(0) && target != address(0), "ACL: zero addr");
        _anySelector[subject][target] = true;
        emit AnySelectorGranted(subject, target);
    }

    function revokeAnySelector(address subject, address target) external override onlyOwner {
        require(subject != address(0) && target != address(0), "ACL: zero addr");
        _anySelector[subject][target] = false;
        emit AnySelectorRevoked(subject, target);
    }

    function setParamConstraint(
        address subject,
        address target,
        bytes4 selector,
        bytes calldata mask,
        bytes calldata value
    ) external override onlyOwner {
        require(subject != address(0) && target != address(0), "ACL: zero addr");
        bytes32 key = _constraintKey(subject, target, selector);
        if (mask.length == 0) {
            delete _constraints[key];
            emit ConstraintCleared(subject, target, selector);
        } else {
            require(mask.length == value.length, "ACL: bad lengths");
            // copy calldata -> memory to assign to storage
            bytes memory m = mask;
            bytes memory v = value;
            _constraints[key].mask = m;
            _constraints[key].value = v;
            emit ConstraintSet(subject, target, selector, mask, value);
        }
    }

    function grantContractCreation(address subject) external override onlyOwner {
        require(subject != address(0), "ACL: zero subject");
        _canCreate[subject] = true;
        emit ContractCreationGranted(subject);
    }

    function revokeContractCreation(address subject) external override onlyOwner {
        require(subject != address(0), "ACL: zero subject");
        _canCreate[subject] = false;
        emit ContractCreationRevoked(subject);
    }

    // --- View / check ---
    function isPermitted(address subject, address target, bytes calldata data) public view override returns (bool) {
        // Contract creation: target == address(0)
        if (target == address(0)) {
            return _canCreate[subject];
        }

        // Any selector fast-path
        if (_anySelector[subject][target]) return true;

        // Selector retrieval (value transfer if < 4 bytes)
        bytes4 selector = bytes4(0);
        if (data.length >= 4) {
            selector = bytes4(data[0:4]);
        }

        // Bitmap check
        (uint256 bucket, uint256 idx) = _bucketIndex(selector);
        uint256 bitmap = _selBitmap[subject][target][bucket];
        bool bitAllowed = ((bitmap >> idx) & 1) == 1;
        if (!bitAllowed) return false;

        // Constraint check (if any)
        bytes32 key = _constraintKey(subject, target, selector);
        Constraint storage c = _constraints[key];
        if (c.mask.length == 0) return true;
        return data.matches(c.mask, c.value);
    }

    function checkPermittedOrRevert(address subject, address target, bytes calldata data) external view override returns (bool) {
        require(isPermitted(subject, target, data), "ACL: denied");
        return true;
    }

    // --- Internal helpers ---
    function _setSelector(address subject, address target, bytes4 selector, bool allowed) internal {
        require(subject != address(0) && target != address(0), "ACL: zero addr");
        (uint256 bucket, uint256 idx) = _bucketIndex(selector);
        uint256 mask = (uint256(1) << idx);
        if (allowed) {
            _selBitmap[subject][target][bucket] |= mask;
        } else {
            _selBitmap[subject][target][bucket] &= ~mask;
        }
    }

    function _bucketIndex(bytes4 selector) internal pure returns (uint256 bucket, uint256 idx) {
        uint32 sel;
        assembly {
            sel := shr(224, selector)
        }
        bucket = uint256(sel >> 8);
        idx = uint256(sel & 0xff);
    }

    function _constraintKey(address subject, address target, bytes4 selector) internal pure returns (bytes32) {
        // forge-lint-disable-next-line asm-keccak256 -- abi.encodePacked is adequate for this small tuple
        return keccak256(abi.encodePacked(subject, target, selector));
    }
}
