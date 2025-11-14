// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

import "./Test.sol";
import "../AccessControlRBACRegistry.sol";
import "../AccessControlRBACChecker.sol";

contract ACLFlowTest is Test {
    AccessControlRBACRegistry public registry;
    AccessControlRBACChecker public checker;
    TestTarget public target;
    bytes32 public orgId;
    bytes32 public groupId;
    address public owner;
    address public reader;
    address public editor;
    address public stranger;

    function setUp() public {
        owner = address(this);
        reader = makeAddr("reader");
        editor = makeAddr("editor");
        stranger = makeAddr("stranger");

        registry = new AccessControlRBACRegistry();
        registry.initialize(owner);
        checker = new AccessControlRBACChecker();
        checker.initialize(address(registry));

        target = new TestTarget();
        orgId = keccak256("example.org");
        groupId = keccak256("writers.group");

        registry.addOrg(orgId);
        registry.setOrgAdmin(orgId, owner, true);
        registry.bindContractToOrg(address(target), orgId);
        registry.setContractDefaultPolicy(address(target), registry.POLICY_WRITER());
        registry.grantRole(orgId, reader, registry.ROLE_READER());
        registry.grantRole(orgId, editor, registry.ROLE_WRITER());
    }

    function testWriterPermitted() public {
        // The org policy for `target` requires ROLE_WRITER and `editor` holds that role,
        // so the checker should allow the call.
        vm.prank(editor);
        assertTrue(checker.isPermitted(editor, address(target), ""));
    }

    function testStrangerDenied() public {
        // `stranger` has no roles in the org, so the checker should reject access.
        vm.prank(stranger);
        assertFalse(checker.isPermitted(stranger, address(target), ""));
    }

    function testRegistryAdminCallAuthorization() public {
        // Bind the registry itself to the org and require ROLE_ADMIN for mutations.
        registry.bindContractToOrg(address(registry), orgId);
        registry.setContractDefaultPolicy(address(registry), registry.POLICY_ADMIN());
        registry.grantRole(orgId, owner, registry.ROLE_ADMIN());

        bytes memory callData = abi.encodeWithSignature(
            "grantRole(bytes32,address,uint256)", orgId, stranger, registry.ROLE_WRITER()
        );

        // Owner has ROLE_ADMIN, so the checker must allow the call.
        vm.prank(owner);
        assertTrue(checker.isPermitted(owner, address(registry), callData));

        // Stranger lacks ROLE_ADMIN, so the checker must block the same call.
        vm.prank(stranger);
        assertFalse(checker.isPermitted(stranger, address(registry), callData));
    }

    function testCreatePermission() public {
        // Only subjects granted create permission should be able to deploy (target = address(0)).
        registry.setCreatePermission(editor, true);

        vm.prank(editor);
        assertTrue(checker.isPermitted(editor, address(0), hex""));

        vm.prank(stranger);
        assertFalse(checker.isPermitted(stranger, address(0), hex""));
    }

    function testPublicPolicyAllowsEveryone() public {
        // Switch target to POLICY_PUBLIC so any subject should be admitted.
        registry.setContractDefaultPolicy(address(target), registry.POLICY_PUBLIC());
        vm.prank(stranger);
        assertTrue(checker.isPermitted(stranger, address(target), ""));
    }

    function testReaderPolicy() public {
        // Require ROLE_READER and ensure only the reader account is admitted.
        registry.setContractDefaultPolicy(address(target), registry.POLICY_READER());
        vm.prank(reader);
        assertTrue(checker.isPermitted(reader, address(target), ""));

        vm.prank(editor);
        assertFalse(checker.isPermitted(editor, address(target), ""));
    }

    function testPolicyTighteningToAdmin() public {
        // Elevate requirement to ROLE_ADMIN and grant only owner this role.
        registry.grantRole(orgId, owner, registry.ROLE_ADMIN());
        registry.setContractDefaultPolicy(address(target), registry.POLICY_ADMIN());

        vm.prank(editor);
        assertFalse(checker.isPermitted(editor, address(target), ""));

        vm.prank(owner);
        assertTrue(checker.isPermitted(owner, address(target), ""));
    }

    function testRoleRevocationIsEnforced() public {
        // Remove writer role and ensure access is revoked immediately.
        registry.revokeRole(orgId, editor, registry.ROLE_WRITER());
        vm.prank(editor);
        assertFalse(checker.isPermitted(editor, address(target), ""));
    }

    function testGroupRoleBitsFlow() public {
        // Give the group writer permissions and toggle membership on/off.
        registry.setGroupRoleBits(orgId, groupId, registry.ROLE_WRITER());
        registry.setGroupMember(orgId, groupId, stranger, true);
        vm.prank(stranger);
        assertTrue(checker.isPermitted(stranger, address(target), ""));

        registry.setGroupMember(orgId, groupId, stranger, false);
        vm.prank(stranger);
        assertFalse(checker.isPermitted(stranger, address(target), ""));
    }

    function testTraceEmitsDecision() public {
        registry.setContractDefaultPolicy(address(target), registry.POLICY_PUBLIC());
        checker.setTraceEnabled(true);
        vm.expectEmit(true, true, true, true);
        emit AccessControlRBACChecker.CheckerTrace(
            editor,
            address(target),
            orgId,
            registry.POLICY_PUBLIC(),
            0,
            true
        );
        vm.prank(editor);
        checker.traceIsPermitted(editor, address(target));
    }

    function testDirectRoleRevokedButGroupStillAllows() public {
        // Grant stranger direct writer role plus group membership.
        registry.grantRole(orgId, stranger, registry.ROLE_WRITER());
        registry.setGroupRoleBits(orgId, groupId, registry.ROLE_WRITER());
        registry.setGroupMember(orgId, groupId, stranger, true);

        // Access allowed with both sources of permission.
        vm.prank(stranger);
        assertTrue(checker.isPermitted(stranger, address(target), ""));

        // Revoke direct role, group membership should still allow access.
        registry.revokeRole(orgId, stranger, registry.ROLE_WRITER());
        vm.prank(stranger);
        assertTrue(checker.isPermitted(stranger, address(target), ""));

        // Once removed from the group, access must be denied.
        registry.setGroupMember(orgId, groupId, stranger, false);
        vm.prank(stranger);
        assertFalse(checker.isPermitted(stranger, address(target), ""));
    }

    function testGroupRemovalDoesNotAffectDirectRole() public {
        // Stranger has a direct writer role and is also in the group.
        registry.grantRole(orgId, stranger, registry.ROLE_WRITER());
        registry.setGroupRoleBits(orgId, groupId, registry.ROLE_WRITER());
        registry.setGroupMember(orgId, groupId, stranger, true);

        vm.prank(stranger);
        assertTrue(checker.isPermitted(stranger, address(target), ""));

        // Removing from the group should not impact the direct role.
        registry.setGroupMember(orgId, groupId, stranger, false);
        vm.prank(stranger);
        assertTrue(checker.isPermitted(stranger, address(target), ""));
    }

    function testMultipleOrganizationsIsolation() public {
        bytes32 otherOrg = keccak256("other.org");
        TestTarget otherTarget = new TestTarget();

        registry.addOrg(otherOrg);
        registry.setOrgAdmin(otherOrg, owner, true);
        registry.bindContractToOrg(address(otherTarget), otherOrg);
        registry.setContractDefaultPolicy(address(otherTarget), registry.POLICY_WRITER());

        // writer role only exists in the original org; no roles in otherOrg.
        vm.prank(editor);
        assertTrue(checker.isPermitted(editor, address(target), ""));

        vm.prank(editor);
        assertFalse(checker.isPermitted(editor, address(otherTarget), ""));

        // After granting writer role in otherOrg, access should succeed.
        registry.grantRole(otherOrg, editor, registry.ROLE_WRITER());
        vm.prank(editor);
        assertTrue(checker.isPermitted(editor, address(otherTarget), ""));
    }
}

contract TestTarget {}
