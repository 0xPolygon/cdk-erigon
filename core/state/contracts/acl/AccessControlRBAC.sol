// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

/// @title AccessControlRBAC
/// @notice Organisation-centric RBAC ACL implementing checkPermittedOrRevert(subject,target,data)
///         to integrate with the client-side EVM ACL enforcement. This contract does not gate
///         target contracts directly; it only answers admission queries.
///
/// Design notes:
/// - Public-by-default: if a target contract is not bound to an organisation, all calls are allowed.
/// - Contracts bound to an organisation require the caller to hold the organisation role specified
///   by the target's default policy (reader | writer | admin). Future extensions can add per-selector
///   overrides without changing the interface.
/// - Users may belong to multiple organisations. Role checks are performed against the org bound to
///   the target contract.
/// - Contract creation is gated via a simple allowlist (owner-controlled) to align with the client's
///   CREATE/CREATE2 enforcement (target == address(0)).
contract AccessControlRBAC {
    // --- Minimal Ownable ---
    address public owner;
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);

    modifier onlyOwner() {
        _onlyOwner();
        _;
    }

    function _onlyOwner() internal view {
        require(msg.sender == owner, "ACL: not owner");
    }

    constructor(address initialOwner) {
        require(initialOwner != address(0), "ACL: bad owner");
        owner = initialOwner;
        emit OwnershipTransferred(address(0), initialOwner);
    }

    function transferOwnership(address newOwner) external onlyOwner {
        require(newOwner != address(0), "ACL: bad owner");
        emit OwnershipTransferred(owner, newOwner);
        owner = newOwner;
    }

    // --- Types & Roles ---
    // Role bit positions (bitmask in uint256)
    uint256 private constant ROLE_READER = 1 << 0;
    uint256 private constant ROLE_WRITER = 1 << 1;
    uint256 private constant ROLE_ADMIN  = 1 << 2;

    // Policy required for a bound contract
    uint8 private constant POLICY_PUBLIC = 0; // everyone
    uint8 private constant POLICY_READER = 1; // requires READER
    uint8 private constant POLICY_WRITER = 2; // requires WRITER
    uint8 private constant POLICY_ADMIN  = 3; // requires ADMIN

    // --- Organisations & Admins ---
    // orgId is bytes32 keccak256(bytes(orgName))
    mapping(bytes32 => bool) public orgExists;
    // Org admins can manage membership, groups, roles and policies in their organisation
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
        require(msg.sender == owner || isOrgAdmin[orgId][msg.sender], "ACL: not org admin");
    }

    // --- Contract Binding & Policy ---
    // If a contract is not bound (orgExists[contractToOrg[target]] == false), it is PUBLIC by default.
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
        require(msg.sender == owner || isOrgAdmin[orgId][msg.sender], "ACL: not org admin");
        delete contractToOrg[target];
        emit ContractUnbound(target);
    }

    function setContractDefaultPolicy(address target, uint8 policy) external {
        bytes32 orgId = contractToOrg[target];
        require(orgId != bytes32(0), "ACL: target not bound");
        require(msg.sender == owner || isOrgAdmin[orgId][msg.sender], "ACL: not org admin");
        require(policy <= POLICY_ADMIN, "ACL: bad policy");
        requiredRoleDefault[target] = policy;
        emit ContractPolicySet(target, policy);
    }

    // --- Users, Groups, Roles ---
    // Effective role bitset per (orgId, user)
    mapping(bytes32 => mapping(address => uint256)) public effectiveRoles;

    // Direct role grants per (orgId, user)
    mapping(bytes32 => mapping(address => uint256)) public directRoles;

    // Groups (optional indirection). For O(1) checks we precompute effectiveRoles on membership/role changes.
    mapping(bytes32 => mapping(bytes32 => uint256)) public groupRoleBits; // orgId => groupId => roleBits
    mapping(bytes32 => mapping(bytes32 => mapping(address => bool))) public inGroup; // orgId => groupId => user => isMember

    event UserRoleSet(bytes32 indexed orgId, address indexed user, uint256 roleBits);
    event GroupRoleSet(bytes32 indexed orgId, bytes32 indexed groupId, uint256 roleBits);
    event GroupMembershipSet(bytes32 indexed orgId, bytes32 indexed groupId, address indexed user, bool enabled);

    function _recomputeEffective(bytes32 orgId, address user) internal {
        uint256 bits = directRoles[orgId][user];
        // To keep this O(1), we do not iterate groups here. Group grants should be reflected
        // by updating direct/effective roles when membership/role changes are made.
        // Consumers that use groups must call setGroupRoleBits / setGroupMember which update effectiveRoles.
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
        // Apply group roles to direct roles so effective checks remain O(1)
        uint256 bits = groupRoleBits[orgId][groupId];
        if (enabled) {
            directRoles[orgId][user] |= bits;
        } else {
            // Conservative: on removal, only clear bits that are fully provided by this group and not by others.
            // To keep O(1), we don't compute this here; operators should re-grant explicitly if needed.
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

    // --- Check API ---
    /// @notice Pure predicate, does not revert. Used by clients that want a boolean.
    function isPermitted(address subject, address target, bytes calldata /*data*/ ) external view returns (bool) {
        return _isPermitted(subject, target);
    }

    /// @notice Reverts if not permitted. Returns true if permitted.
    /// Selector: 0xbf5afe38 (keccak256("checkPermittedOrRevert(address,address,bytes)"))
    function checkPermittedOrRevert(address subject, address target, bytes calldata /*data*/ ) external view returns (bool) {
        require(_isPermitted(subject, target), "ACL: denied");
        return true;
    }

    function _isPermitted(address subject, address target) internal view returns (bool) {
        if (target == address(0)) {
            // Contract creation gate
            return canCreate[subject];
        }
        bytes32 orgId = contractToOrg[target];
        if (!orgExists[orgId]) {
            // Public by default if not bound to an existing org
            return true;
        }
        uint8 policy = requiredRoleDefault[target];
        if (policy == POLICY_PUBLIC) return true;
        uint256 bits = effectiveRoles[orgId][subject];
        if (policy == POLICY_READER) return (bits & ROLE_READER) != 0;
        if (policy == POLICY_WRITER) return (bits & ROLE_WRITER) != 0;
        if (policy == POLICY_ADMIN)  return (bits & ROLE_ADMIN)  != 0;
        return false;
    }

    // --- Role constants accessors (for off-chain tooling) ---
    function roleReaderBit() external pure returns (uint256) { return ROLE_READER; }
    function roleWriterBit() external pure returns (uint256) { return ROLE_WRITER; }
    function roleAdminBit() external pure returns (uint256) { return ROLE_ADMIN; }

    function policyPublicId() external pure returns (uint8) { return POLICY_PUBLIC; }
    function policyReaderId() external pure returns (uint8) { return POLICY_READER; }
    function policyWriterId() external pure returns (uint8) { return POLICY_WRITER; }
    function policyAdminId() external pure returns (uint8) { return POLICY_ADMIN; }
}
