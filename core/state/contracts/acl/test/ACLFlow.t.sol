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
    bytes32 public readerGroupId;
    bytes32 public writerGroupId;
    bytes32 public adminGroupId;
    bytes32 public backupWriterGroupId;
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
        readerGroupId = keccak256("readers.group");
        writerGroupId = keccak256("writers.group");
        adminGroupId = keccak256("admins.group");
        backupWriterGroupId = keccak256("writers.backup");

        registry.addOrg(orgId);
        registry.setOrgAdmin(orgId, owner, true);
        registry.bindContractToOrg(address(target), orgId);
        registry.setContractDefaultPolicy(address(target), registry.POLICY_WRITER());
        registry.setGroupRoleBits(orgId, readerGroupId, registry.ROLE_READER());
        registry.setGroupRoleBits(orgId, writerGroupId, registry.ROLE_WRITER());
        registry.setGroupRoleBits(orgId, adminGroupId, registry.ROLE_ADMIN());
        registry.setGroupRoleBits(orgId, backupWriterGroupId, registry.ROLE_WRITER());

        registry.setGroupMember(orgId, readerGroupId, reader, true);
        registry.setGroupMember(orgId, writerGroupId, editor, true);
        registry.setGroupMember(orgId, adminGroupId, owner, true);
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

        bytes memory callData = abi.encodeWithSignature(
            "setGroupMember(bytes32,bytes32,address,bool)", orgId, writerGroupId, stranger, true
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
        registry.setContractDefaultPolicy(address(target), registry.POLICY_ADMIN());

        vm.prank(editor);
        assertFalse(checker.isPermitted(editor, address(target), ""));

        vm.prank(owner);
        assertTrue(checker.isPermitted(owner, address(target), ""));
    }

    function testRoleRevocationIsEnforced() public {
        // Remove writer role and ensure access is revoked immediately.
        registry.setGroupMember(orgId, writerGroupId, editor, false);
        vm.prank(editor);
        assertFalse(checker.isPermitted(editor, address(target), ""));
    }

    function testGroupRoleBitsFlow() public {
        // Give the group writer permissions and toggle membership on/off.
        registry.setGroupMember(orgId, writerGroupId, stranger, true);
        vm.prank(stranger);
        assertTrue(checker.isPermitted(stranger, address(target), ""));

        registry.setGroupMember(orgId, writerGroupId, stranger, false);
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

    function testMultipleGroupMembershipRequiresAllSourcesRemoved() public {
        // Stranger joins two writer groups.
        registry.setGroupMember(orgId, writerGroupId, stranger, true);
        registry.setGroupMember(orgId, backupWriterGroupId, stranger, true);

        vm.prank(stranger);
        assertTrue(checker.isPermitted(stranger, address(target), ""));

        // Removing one group still leaves access via the other.
        registry.setGroupMember(orgId, writerGroupId, stranger, false);
        vm.prank(stranger);
        assertTrue(checker.isPermitted(stranger, address(target), ""));

        // Removing the final group revokes access.
        registry.setGroupMember(orgId, backupWriterGroupId, stranger, false);
        vm.prank(stranger);
        assertFalse(checker.isPermitted(stranger, address(target), ""));
    }

    function testGroupRoleBitUpdatesPropagate() public {
        registry.setGroupMember(orgId, writerGroupId, stranger, true);
        vm.prank(stranger);
        assertTrue(checker.isPermitted(stranger, address(target), ""));

        // Clearing the group's role bits should immediately block access.
        registry.setGroupRoleBits(orgId, writerGroupId, 0);
        vm.prank(stranger);
        assertFalse(checker.isPermitted(stranger, address(target), ""));

        // Restoring the role bits should re-enable access.
        registry.setGroupRoleBits(orgId, writerGroupId, registry.ROLE_WRITER());
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
        registry.setGroupRoleBits(otherOrg, writerGroupId, registry.ROLE_WRITER());

        // writer role only exists in the original org; no roles in otherOrg.
        vm.prank(editor);
        assertTrue(checker.isPermitted(editor, address(target), ""));

        vm.prank(editor);
        assertFalse(checker.isPermitted(editor, address(otherTarget), ""));

        // After granting writer role in otherOrg, access should succeed.
        registry.setGroupMember(otherOrg, writerGroupId, editor, true);
        vm.prank(editor);
        assertTrue(checker.isPermitted(editor, address(otherTarget), ""));
    }
}

contract TestTarget {}
