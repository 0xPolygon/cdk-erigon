// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";

/// @title AccessControlRBACRegistry
/// @notice Stores organisation membership, roles, group-role bundles, contract bindings and policies.
///         This contract holds the state for RBAC checks. A separate Checker contract can consult
///         this registry via view functions to implement checkPermittedOrRevert.
contract AccessControlRBACRegistry is Initializable, OwnableUpgradeable {
    // --- Initializer ---
    function initialize(address initialOwner) public initializer {
        require(initialOwner != address(0), "ACL: bad owner");
        __Ownable_init(initialOwner);
    }

    // --- Roles & Policies ---
    // Role bit positions (bitmask in uint256)
    uint256 public constant ROLE_READER = 1 << 0;
    uint256 public constant ROLE_WRITER = 1 << 1;
    uint256 public constant ROLE_ADMIN  = 1 << 2;

    // Default policy per target
    uint8 public constant POLICY_PUBLIC = 0; // everyone
    uint8 public constant POLICY_READER = 1; // requires READER
    uint8 public constant POLICY_WRITER = 2; // requires WRITER
    uint8 public constant POLICY_ADMIN  = 3; // requires ADMIN

    // --- Organisations & Admins ---
    mapping(bytes32 => bool) public orgExists;
    mapping(bytes32 => mapping(address => bool)) public isOrgAdmin;

    event OrgAdded(bytes32 indexed orgId);
    event OrgRemoved(bytes32 indexed orgId);
    event OrgAdminSet(bytes32 indexed orgId, address indexed admin, bool enabled);

    function addOrg(bytes32 orgId) external onlyOwner {
        require(orgId != bytes32(0), "ACL: bad orgId");
        require(!orgExists[orgId], "ACL: org exists");
        orgExists[orgId] = true;
        emit OrgAdded(orgId);
    }

    function removeOrg(bytes32 orgId) external onlyOwner {
        require(orgExists[orgId], "ACL: no org");
        orgExists[orgId] = false;
        emit OrgRemoved(orgId);
    }

    function setOrgAdmin(bytes32 orgId, address admin, bool enabled) external onlyOwner {
        require(orgExists[orgId], "ACL: no org");
        require(admin != address(0), "ACL: bad admin");
        isOrgAdmin[orgId][admin] = enabled;
        emit OrgAdminSet(orgId, admin, enabled);
    }

    modifier onlyOrgAdminOrOwner(bytes32 orgId) {
        _onlyOrgAdminOrOwner(orgId);
        _;
    }

    function _onlyOrgAdminOrOwner(bytes32 orgId) internal view {
        require(orgExists[orgId], "ACL: no org");
        require(msg.sender == owner() || isOrgAdmin[orgId][msg.sender], "ACL: not org admin");
    }

    // --- Contract Binding & Policy ---
    mapping(address => bytes32) public contractToOrg;
    mapping(address => uint8)   public requiredRoleDefault; // POLICY_*

    event ContractBound(address indexed target, bytes32 indexed orgId);
    event ContractUnbound(address indexed target);
    event ContractPolicySet(address indexed target, uint8 policy);

    function bindContractToOrg(address target, bytes32 orgId) external onlyOrgAdminOrOwner(orgId) {
        require(target != address(0), "ACL: bad target");
        contractToOrg[target] = orgId;
        emit ContractBound(target, orgId);
    }

    function unbindContract(address target) external {
        bytes32 orgId = contractToOrg[target];
        require(msg.sender == owner() || isOrgAdmin[orgId][msg.sender], "ACL: not org admin");
        delete contractToOrg[target];
        emit ContractUnbound(target);
    }

    function setContractDefaultPolicy(address target, uint8 policy) external {
        bytes32 orgId = contractToOrg[target];
        require(orgId != bytes32(0), "ACL: target not bound");
        require(msg.sender == owner() || isOrgAdmin[orgId][msg.sender], "ACL: not org admin");
        require(policy <= POLICY_ADMIN, "ACL: bad policy");
        requiredRoleDefault[target] = policy;
        emit ContractPolicySet(target, policy);
    }

    // --- Users, Groups, Roles ---
    mapping(bytes32 => mapping(address => uint256)) public effectiveRoles;
    mapping(bytes32 => mapping(address => uint256)) public directRoles;
    mapping(bytes32 => mapping(bytes32 => uint256)) public groupRoleBits;
    mapping(bytes32 => mapping(bytes32 => mapping(address => bool))) public inGroup;

    event UserRoleSet(bytes32 indexed orgId, address indexed user, uint256 roleBits);
    event GroupRoleSet(bytes32 indexed orgId, bytes32 indexed groupId, uint256 roleBits);
    event GroupMembershipSet(bytes32 indexed orgId, bytes32 indexed groupId, address indexed user, bool enabled);

    function _recomputeEffective(bytes32 orgId, address user) internal {
        uint256 bits = directRoles[orgId][user];
        effectiveRoles[orgId][user] = bits;
    }

    function setUserRoleBits(bytes32 orgId, address user, uint256 roleBits) external onlyOrgAdminOrOwner(orgId) {
        directRoles[orgId][user] = roleBits;
        _recomputeEffective(orgId, user);
        emit UserRoleSet(orgId, user, roleBits);
    }

    function grantRole(bytes32 orgId, address user, uint256 roleBit) external onlyOrgAdminOrOwner(orgId) {
        directRoles[orgId][user] |= roleBit;
        _recomputeEffective(orgId, user);
        emit UserRoleSet(orgId, user, directRoles[orgId][user]);
    }

    function revokeRole(bytes32 orgId, address user, uint256 roleBit) external onlyOrgAdminOrOwner(orgId) {
        directRoles[orgId][user] &= ~roleBit;
        _recomputeEffective(orgId, user);
        emit UserRoleSet(orgId, user, directRoles[orgId][user]);
    }

    function setGroupRoleBits(bytes32 orgId, bytes32 groupId, uint256 roleBits) external onlyOrgAdminOrOwner(orgId) {
        groupRoleBits[orgId][groupId] = roleBits;
        emit GroupRoleSet(orgId, groupId, roleBits);
    }

    function setGroupMember(bytes32 orgId, bytes32 groupId, address user, bool enabled) external onlyOrgAdminOrOwner(orgId) {
        inGroup[orgId][groupId][user] = enabled;
        uint256 bits = groupRoleBits[orgId][groupId];
        if (enabled) {
            directRoles[orgId][user] |= bits;
        } else {
            directRoles[orgId][user] &= ~bits;
        }
        _recomputeEffective(orgId, user);
        emit GroupMembershipSet(orgId, groupId, user, enabled);
    }

    // --- Create permissions ---
    mapping(address => bool) public canCreate;
    event CreatePermissionSet(address indexed user, bool enabled);

    function setCreatePermission(address user, bool enabled) external onlyOwner {
        require(user != address(0), "ACL: bad user");
        canCreate[user] = enabled;
        emit CreatePermissionSet(user, enabled);
    }
}
