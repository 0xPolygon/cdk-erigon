// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import {UUPSUpgradeable} from "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";

/// @title AccessControlRBACChecker
/// @notice Read-only checker that consults an AccessControlRBACRegistry to answer
///         checkPermittedOrRevert(subject,target,data) for the client EVM ACL.
///         Deploy behind a transparent proxy and initialize with the Registry address.
interface IAccessControlRBACRegistryView {
    // ownership
    function owner() external view returns (address);

    // orgs/admins
    function orgExists(bytes32 orgId) external view returns (bool);
    function isOrgAdmin(bytes32 orgId, address who) external view returns (bool);

    // binding/policy
    function contractToOrg(address target) external view returns (bytes32);
    function requiredRoleDefault(address target) external view returns (uint8);

    // roles
    function effectiveRoles(bytes32 orgId, address user) external view returns (uint256);

    // role constants
    function ROLE_READER() external view returns (uint256);
    function ROLE_WRITER() external view returns (uint256);
    function ROLE_ADMIN()  external view returns (uint256);

    // policy constants
    function POLICY_PUBLIC() external view returns (uint8);
    function POLICY_READER() external view returns (uint8);
    function POLICY_WRITER() external view returns (uint8);
    function POLICY_ADMIN()  external view returns (uint8);

    // create
    function canCreate(address user) external view returns (bool);
}

contract AccessControlRBACChecker is Initializable, UUPSUpgradeable {
    uint64 internal constant REGISTRY_CALL_GAS = 50_000;
    // Transparent-proxy friendly initializer
    address public registry;
    event Initialized(address indexed registry);
    event TraceToggled(bool enabled);
    event CheckerTrace(
        address indexed subject,
        address indexed target,
        bytes32 orgId,
        uint8 policy,
        uint256 roleBits,
        bool permitted
    );

    bool public traceEnabled;
    uint8 internal constant TRACE_POLICY_CREATE = type(uint8).max;

    function initialize(address registryAddr) external initializer {
        require(registryAddr != address(0), "ACL: bad registry");
        registry = registryAddr;
        emit Initialized(registryAddr);
    }

    function setTraceEnabled(bool enabled) external {
        require(msg.sender == IAccessControlRBACRegistryView(registry).owner(), "ACL: not owner");
        traceEnabled = enabled;
        emit TraceToggled(enabled);
    }

    // OwnerBypass hook: the client queries owner() on the ACL address. We surface the registry owner.
    function owner() external view returns (address) {
        return IAccessControlRBACRegistryView(registry).owner();
    }

    // Boolean predicate (non-reverting)
    function isPermitted(address subject, address target, bytes calldata /*data*/ ) external view returns (bool) {
        return _isPermitted(subject, target);
    }

    // Reverting check: matches the selector expected by the client (0xbf5afe38)
    function checkPermittedOrRevert(address subject, address target, bytes calldata /*data*/ ) external view returns (bool) {
        require(_isPermitted(subject, target), "ACL: denied");
        return true;
    }

    function traceIsPermitted(address subject, address target) external returns (bool) {
        (bytes32 orgId, uint8 policy, uint256 roleBits, bool permitted) = _computeDecision(subject, target);
        if (traceEnabled) {
            emit CheckerTrace(subject, target, orgId, policy, roleBits, permitted);
        }
        return permitted;
    }

    function _isPermitted(address subject, address target) internal view returns (bool) {
        (, , , bool permitted) = _computeDecision(subject, target);
        return permitted;
    }

    function _computeDecision(
        address subject,
        address target
    ) internal view returns (bytes32 orgId, uint8 policy, uint256 roleBits, bool permitted) {
        if (target == address(0)) {
            bool allowedCreate = abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.canCreate.selector, subject)), (bool));
            return (bytes32(0), TRACE_POLICY_CREATE, 0, allowedCreate);
        }
        bytes32 resolvedOrg = abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.contractToOrg.selector, target)), (bytes32));
        if (!abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.orgExists.selector, resolvedOrg)), (bool))) {
            // Public by default if not bound to an existing org
            uint8 policyPublic = abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.POLICY_PUBLIC.selector)), (uint8));
            return (resolvedOrg, policyPublic, 0, true);
        }
        uint8 resolvedPolicy = abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.requiredRoleDefault.selector, target)), (uint8));
        uint8 policyPublicCache = abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.POLICY_PUBLIC.selector)), (uint8));
        if (resolvedPolicy == policyPublicCache) {
            return (resolvedOrg, resolvedPolicy, 0, true);
        }
        uint256 bits = abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.effectiveRoles.selector, resolvedOrg, subject)), (uint256));
        bool allowed;
        uint8 policyReader = abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.POLICY_READER.selector)), (uint8));
        uint8 policyWriter = abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.POLICY_WRITER.selector)), (uint8));
        uint8 policyAdmin = abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.POLICY_ADMIN.selector)), (uint8));
        uint256 roleReader = abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.ROLE_READER.selector)), (uint256));
        uint256 roleWriter = abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.ROLE_WRITER.selector)), (uint256));
        uint256 roleAdmin = abi.decode(_registryCall(abi.encodeWithSelector(IAccessControlRBACRegistryView.ROLE_ADMIN.selector)), (uint256));
        if (resolvedPolicy == policyReader) {
            allowed = (bits & roleReader) != 0;
        } else if (resolvedPolicy == policyWriter) {
            allowed = (bits & roleWriter) != 0;
        } else if (resolvedPolicy == policyAdmin) {
            allowed = (bits & roleAdmin) != 0;
        } else {
            allowed = false;
        }
        return (resolvedOrg, resolvedPolicy, bits, allowed);
    }

    function _registryCall(bytes memory data) internal view returns (bytes memory ret) {
        address target = registry;
        uint64 gasLimit = REGISTRY_CALL_GAS;
        assembly {
            let success := staticcall(gasLimit, target, add(data, 0x20), mload(data), 0, 0)
            let size := returndatasize()
            ret := mload(0x40)
            mstore(ret, size)
            returndatacopy(add(ret, 0x20), 0, size)
            mstore(0x40, add(add(ret, 0x20), size))
            if iszero(success) {
                revert(add(ret, 0x20), size)
            }
        }
    }

    function _authorizeUpgrade(address /*newImplementation*/ ) internal view override {
        require(msg.sender == IAccessControlRBACRegistryView(registry).owner(), "ACL: not owner");
    }
}
