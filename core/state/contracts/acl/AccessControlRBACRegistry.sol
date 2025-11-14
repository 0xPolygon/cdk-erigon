// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import {UUPSUpgradeable} from "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";

/// @title AccessControlRBACRegistry
/// @notice Stores organisation membership, roles, group-role bundles, contract bindings and policies.
///         This contract holds the state for RBAC checks. A separate Checker contract can consult
///         this registry via view functions to implement checkPermittedOrRevert.
contract AccessControlRBACRegistry is Initializable, OwnableUpgradeable, UUPSUpgradeable {
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
    // Track aggregated group-derived role bits and bidirectional membership lists.
    mapping(bytes32 => mapping(address => uint256)) internal groupDerivedRoles;
    mapping(bytes32 => mapping(address => bytes32[])) internal userGroups;
    mapping(bytes32 => mapping(address => mapping(bytes32 => uint256))) internal userGroupIndex;
    mapping(bytes32 => mapping(bytes32 => address[])) internal groupMembers;
    mapping(bytes32 => mapping(bytes32 => mapping(address => uint256))) internal groupMemberIndex;

    event UserRoleSet(bytes32 indexed orgId, address indexed user, uint256 roleBits);
    event GroupRoleSet(bytes32 indexed orgId, bytes32 indexed groupId, uint256 roleBits);
    event GroupMembershipSet(bytes32 indexed orgId, bytes32 indexed groupId, address indexed user, bool enabled);

    function _recomputeEffective(bytes32 orgId, address user) internal {
        effectiveRoles[orgId][user] = directRoles[orgId][user] | groupDerivedRoles[orgId][user];
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
        address[] storage members = groupMembers[orgId][groupId];
        for (uint256 i = 0; i < members.length; ++i) {
            _recomputeGroupDerived(orgId, members[i]);
            _recomputeEffective(orgId, members[i]);
        }
        emit GroupRoleSet(orgId, groupId, roleBits);
    }

    function setGroupMember(bytes32 orgId, bytes32 groupId, address user, bool enabled) external onlyOrgAdminOrOwner(orgId) {
        bool currently = inGroup[orgId][groupId][user];
        if (currently == enabled) {
            return;
        }
        if (enabled) {
            inGroup[orgId][groupId][user] = true;
            userGroupIndex[orgId][user][groupId] = userGroups[orgId][user].length + 1;
            userGroups[orgId][user].push(groupId);
            groupMemberIndex[orgId][groupId][user] = groupMembers[orgId][groupId].length + 1;
            groupMembers[orgId][groupId].push(user);
        } else {
            inGroup[orgId][groupId][user] = false;
            _removeUserGroup(orgId, user, groupId);
            _removeGroupMember(orgId, groupId, user);
        }
        _recomputeGroupDerived(orgId, user);
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

    function _authorizeUpgrade(address /*newImplementation*/ ) internal override onlyOwner {}

    function _recomputeGroupDerived(bytes32 orgId, address user) internal {
        bytes32[] storage groups = userGroups[orgId][user];
        uint256 derived;
        for (uint256 i = 0; i < groups.length; ++i) {
            derived |= groupRoleBits[orgId][groups[i]];
        }
        groupDerivedRoles[orgId][user] = derived;
    }

    function _removeUserGroup(bytes32 orgId, address user, bytes32 groupId) internal {
        uint256 idxPlusOne = userGroupIndex[orgId][user][groupId];
        if (idxPlusOne == 0) {
            return;
        }
        uint256 idx = idxPlusOne - 1;
        bytes32[] storage groups = userGroups[orgId][user];
        uint256 last = groups.length - 1;
        if (idx != last) {
            bytes32 replacement = groups[last];
            groups[idx] = replacement;
            userGroupIndex[orgId][user][replacement] = idx + 1;
        }
        groups.pop();
        userGroupIndex[orgId][user][groupId] = 0;
    }

    function _removeGroupMember(bytes32 orgId, bytes32 groupId, address user) internal {
        uint256 idxPlusOne = groupMemberIndex[orgId][groupId][user];
        if (idxPlusOne == 0) {
            return;
        }
        uint256 idx = idxPlusOne - 1;
        address[] storage members = groupMembers[orgId][groupId];
        uint256 last = members.length - 1;
        if (idx != last) {
            address replacement = members[last];
            members[idx] = replacement;
            groupMemberIndex[orgId][groupId][replacement] = idx + 1;
        }
        members.pop();
        groupMemberIndex[orgId][groupId][user] = 0;
    }
}
