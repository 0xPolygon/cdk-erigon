// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

import {OwnableUpgradeable} from "@openzeppelin/contracts-upgradeable/access/OwnableUpgradeable.sol";
import {Initializable} from "@openzeppelin/contracts-upgradeable/proxy/utils/Initializable.sol";
import {UUPSUpgradeable} from "@openzeppelin/contracts-upgradeable/proxy/utils/UUPSUpgradeable.sol";

/// @title AccessControlClaimRegistry
/// @notice Claim-oriented variant of the ACL Registry that satisfies the Step 3 requirements.
///         Organisations manage members and issue scoped claims (e.g. reader/writer/admin) per org.
///         Claims are folded into role bitmasks so the existing checker API continues to function.
contract AccessControlClaimRegistry is Initializable, OwnableUpgradeable, UUPSUpgradeable {
    // --- Initializer ---
    function initialize(address initialOwner) public initializer {
        require(initialOwner != address(0), "ACL: bad owner");
        __Ownable_init(initialOwner);
        claimRoleBit[CLAIM_READER] = ROLE_READER;
        claimRoleBit[CLAIM_WRITER] = ROLE_WRITER;
        claimRoleBit[CLAIM_ADMIN] = ROLE_ADMIN;
    }

    // --- Roles & Claims ---
    uint256 public constant ROLE_READER = 1 << 0;
    uint256 public constant ROLE_WRITER = 1 << 1;
    uint256 public constant ROLE_ADMIN  = 1 << 2;

    bytes32 public constant CLAIM_READER = keccak256("reader");
    bytes32 public constant CLAIM_WRITER = keccak256("writer");
    bytes32 public constant CLAIM_ADMIN  = keccak256("admin");

    mapping(bytes32 => uint256) internal claimRoleBit;

    // --- Organisations & Admins ---
    mapping(bytes32 => bool) public orgExists;
    mapping(bytes32 => string) public orgNames;
    bytes32[] internal orgIndex;
    mapping(bytes32 => bool) internal orgSeen;
    mapping(bytes32 => mapping(address => bool)) public isOrgAdmin;

    event OrgAdded(bytes32 indexed orgId);
    event OrgRemoved(bytes32 indexed orgId);
    event OrgAdminSet(bytes32 indexed orgId, address indexed admin, bool enabled);

    function addOrg(bytes32 orgId) external onlyOwner {
        require(orgId != bytes32(0), "ACL: bad orgId");
        require(!orgExists[orgId], "ACL: org exists");
        orgExists[orgId] = true;
        if (!orgSeen[orgId]) {
            orgSeen[orgId] = true;
            orgIndex.push(orgId);
        }
        emit OrgAdded(orgId);
    }

    function removeOrg(bytes32 orgId) external onlyOwner {
        require(orgExists[orgId], "ACL: no org");
        orgExists[orgId] = false;
        emit OrgRemoved(orgId);
    }

    function setOrgName(bytes32 orgId, string calldata name) external onlyOrgAdminOrOwner(orgId) {
        orgNames[orgId] = name;
    }

    function getOrgIds() external view returns (bytes32[] memory) {
        return orgIndex;
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
    mapping(bytes32 => address[]) internal orgContracts;
    mapping(bytes32 => mapping(address => uint256)) internal orgContractIndex; // index + 1
    mapping(address => uint8)   public requiredRoleDefault; // POLICY_*

    uint8 public constant POLICY_PUBLIC = 0;
    uint8 public constant POLICY_READER = 1;
    uint8 public constant POLICY_WRITER = 2;
    uint8 public constant POLICY_ADMIN  = 3;

    event ContractBound(address indexed target, bytes32 indexed orgId);
    event ContractUnbound(address indexed target);
    event ContractRebound(address indexed target, bytes32 indexed previousOrgId, bytes32 indexed newOrgId);
    event ContractPolicySet(address indexed target, uint8 policy);

    function bindContractToOrg(address target, bytes32 orgId) external onlyOrgAdminOrOwner(orgId) {
        require(target != address(0), "ACL: bad target");
        bytes32 previousOrg = contractToOrg[target];
        if (previousOrg == orgId) {
            return;
        }
        if (previousOrg != bytes32(0)) {
            _removeOrgContract(previousOrg, target);
            emit ContractRebound(target, previousOrg, orgId);
        }
        contractToOrg[target] = orgId;
        _addOrgContract(orgId, target);
        emit ContractBound(target, orgId);
    }

    function unbindContract(address target) external {
        bytes32 orgId = contractToOrg[target];
        require(orgId != bytes32(0), "ACL: target not bound");
        require(msg.sender == owner() || isOrgAdmin[orgId][msg.sender], "ACL: not org admin");
        delete contractToOrg[target];
        _removeOrgContract(orgId, target);
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

    function getOrgContractCount(bytes32 orgId) external view returns (uint256) {
        return orgContracts[orgId].length;
    }

    function getOrgContractAt(bytes32 orgId, uint256 index) external view returns (address) {
        return orgContracts[orgId][index];
    }

    function getOrgContracts(bytes32 orgId) external view returns (address[] memory) {
        return orgContracts[orgId];
    }

    // --- Membership & Claims ---
    mapping(address => bytes32) public userOrg;
    mapping(bytes32 => address[]) internal orgMembers;
    mapping(bytes32 => mapping(address => uint256)) internal orgMemberIndex; // index + 1
    mapping(bytes32 => mapping(address => uint256)) public effectiveRoles;
    mapping(bytes32 => mapping(address => mapping(bytes32 => bool))) public hasClaim;
    mapping(address => bytes32[]) internal userClaimOrgs;
    mapping(address => mapping(bytes32 => bool)) internal userClaimOrgSeen;

    event UserOrgSet(address indexed user, bytes32 indexed previousOrg, bytes32 indexed newOrg);
    event OrgMemberSet(bytes32 indexed orgId, address indexed user, bool enabled);
    event ClaimSet(bytes32 indexed orgId, address indexed user, bytes32 indexed claimId, bool enabled);
    event UserRoleSet(bytes32 indexed orgId, address indexed user, uint256 roleBits);

    function setUserOrg(bytes32 orgId, address user) external onlyOrgAdminOrOwner(orgId) {
        require(user != address(0), "ACL: bad user");
        bytes32 previous = userOrg[user];
        if (previous == orgId) {
            return;
        }
        if (previous != bytes32(0)) {
            _removeOrgMember(previous, user);
        }
        userOrg[user] = orgId;
        _addOrgMember(orgId, user);
        emit UserOrgSet(user, previous, orgId);
    }

    function clearUserOrg(address user) external {
        bytes32 orgId = userOrg[user];
        require(orgId != bytes32(0), "ACL: no org");
        require(msg.sender == owner() || isOrgAdmin[orgId][msg.sender], "ACL: not org admin");
        delete userOrg[user];
        _removeOrgMember(orgId, user);
        emit UserOrgSet(user, orgId, bytes32(0));
    }

    function setClaim(bytes32 orgId, address user, bytes32 claimId, bool enabled) external onlyOrgAdminOrOwner(orgId) {
        require(userOrg[user] == orgId, "ACL: user not member");
        hasClaim[orgId][user][claimId] = enabled;
        uint256 roleBit = claimRoleBit[claimId];
        if (roleBit != 0) {
            if (enabled) {
                effectiveRoles[orgId][user] |= roleBit;
            } else {
                effectiveRoles[orgId][user] &= ~roleBit;
            }
            emit UserRoleSet(orgId, user, effectiveRoles[orgId][user]);
        }
        if (enabled && !userClaimOrgSeen[user][orgId]) {
            userClaimOrgSeen[user][orgId] = true;
            userClaimOrgs[user].push(orgId);
        }
        emit ClaimSet(orgId, user, claimId, enabled);
    }

    function hasScopedClaim(bytes32 orgId, address user, bytes32 claimId) external view returns (bool) {
        return hasClaim[orgId][user][claimId];
    }

    function scopedClaim(bytes32 orgId, bytes32 claimId) external pure returns (bytes32) {
        return keccak256(abi.encode(orgId, claimId));
    }

    function getOrgMemberCount(bytes32 orgId) external view returns (uint256) {
        return orgMembers[orgId].length;
    }

    function getOrgMemberAt(bytes32 orgId, uint256 index) external view returns (address) {
        return orgMembers[orgId][index];
    }

    function getOrgMembers(bytes32 orgId) external view returns (address[] memory) {
        return orgMembers[orgId];
    }

    function getUserClaimOrgs(address user) external view returns (bytes32[] memory) {
        return userClaimOrgs[user];
    }

    // --- Create permissions ---
    mapping(address => bool) public canCreate;
    event CreatePermissionSet(address indexed user, bool enabled);

    function setCreatePermission(address user, bool enabled) external onlyOwner {
        require(user != address(0), "ACL: bad user");
        canCreate[user] = enabled;
        emit CreatePermissionSet(user, enabled);
    }

    // --- View helpers for checker parity ---
    function orgMembersContains(bytes32 orgId, address user) external view returns (bool) {
        return orgMemberIndex[orgId][user] != 0;
    }

    // --- Internal bookkeeping ---
    function _addOrgContract(bytes32 orgId, address target) internal {
        if (orgContractIndex[orgId][target] != 0) {
            return;
        }
        orgContractIndex[orgId][target] = orgContracts[orgId].length + 1;
        orgContracts[orgId].push(target);
    }

    function _removeOrgContract(bytes32 orgId, address target) internal {
        uint256 idxPlusOne = orgContractIndex[orgId][target];
        if (idxPlusOne == 0) {
            return;
        }
        uint256 idx = idxPlusOne - 1;
        address[] storage contracts = orgContracts[orgId];
        uint256 lastIndex = contracts.length - 1;
        if (idx != lastIndex) {
            address replacement = contracts[lastIndex];
            contracts[idx] = replacement;
            orgContractIndex[orgId][replacement] = idx + 1;
        }
        contracts.pop();
        orgContractIndex[orgId][target] = 0;
    }

    function _addOrgMember(bytes32 orgId, address user) internal {
        if (orgMemberIndex[orgId][user] != 0) {
            return;
        }
        orgMemberIndex[orgId][user] = orgMembers[orgId].length + 1;
        orgMembers[orgId].push(user);
        emit OrgMemberSet(orgId, user, true);
    }

    function _removeOrgMember(bytes32 orgId, address user) internal {
        uint256 idxPlusOne = orgMemberIndex[orgId][user];
        if (idxPlusOne == 0) {
            return;
        }
        uint256 idx = idxPlusOne - 1;
        address[] storage members = orgMembers[orgId];
        uint256 lastIndex = members.length - 1;
        if (idx != lastIndex) {
            address replacement = members[lastIndex];
            members[idx] = replacement;
            orgMemberIndex[orgId][replacement] = idx + 1;
        }
        members.pop();
        orgMemberIndex[orgId][user] = 0;
        emit OrgMemberSet(orgId, user, false);
        effectiveRoles[orgId][user] = 0;
    }

    // OZ UUPS hook
    function _authorizeUpgrade(address /*newImplementation*/ ) internal view override onlyOwner {}
}
