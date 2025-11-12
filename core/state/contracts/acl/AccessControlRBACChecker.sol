// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";

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

contract AccessControlRBACChecker is Initializable {
    // Transparent-proxy friendly initializer
    address public registry;
    event Initialized(address indexed registry);

    function initialize(address registryAddr) external initializer {
        require(registryAddr != address(0), "ACL: bad registry");
        registry = registryAddr;
        emit Initialized(registryAddr);
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

    function _isPermitted(address subject, address target) internal view returns (bool) {
        IAccessControlRBACRegistryView R = IAccessControlRBACRegistryView(registry);
        if (target == address(0)) {
            return R.canCreate(subject);
        }
        bytes32 orgId = R.contractToOrg(target);
        if (!R.orgExists(orgId)) {
            // Public by default if not bound to an existing org
            return true;
        }
        uint8 policy = R.requiredRoleDefault(target);
        if (policy == R.POLICY_PUBLIC()) return true;
        uint256 bits = R.effectiveRoles(orgId, subject);
        if (policy == R.POLICY_READER()) return (bits & R.ROLE_READER()) != 0;
        if (policy == R.POLICY_WRITER()) return (bits & R.ROLE_WRITER()) != 0;
        if (policy == R.POLICY_ADMIN())  return (bits & R.ROLE_ADMIN())  != 0;
        return false;
    }
}
