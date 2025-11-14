// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

import "./Test.sol";
import "../AccessControlClaimRegistry.sol";
import "../AccessControlRBACChecker.sol";

contract ClaimRegistryFlowTest is Test {
    AccessControlClaimRegistry public registry;
    AccessControlRBACChecker public checker;
    TestTarget public target;

    bytes32 public orgId;
    address public owner;
    address public writer;
    address public reader;
    address public stranger;

    function setUp() public {
        owner = address(this);
        writer = makeAddr("writer");
        reader = makeAddr("reader");
        stranger = makeAddr("stranger");

        registry = new AccessControlClaimRegistry();
        registry.initialize(owner);

        checker = new AccessControlRBACChecker();
        checker.initialize(address(registry));

        target = new TestTarget();
        orgId = keccak256("claims.example");

        registry.addOrg(orgId);
        registry.setOrgAdmin(orgId, owner, true);
        registry.bindContractToOrg(address(target), orgId);
        registry.setContractDefaultPolicy(address(target), registry.POLICY_WRITER());

        registry.setUserOrg(orgId, writer);
        registry.setUserOrg(orgId, reader);
        registry.setUserOrg(orgId, owner);
        registry.setClaim(orgId, owner, registry.CLAIM_ADMIN(), true);
    }

    function testWriterClaimAllowsAccess() public {
        registry.setClaim(orgId, writer, registry.CLAIM_WRITER(), true);

        vm.prank(writer);
        assertTrue(checker.isPermitted(writer, address(target), ""));
    }

    function testRemovingClaimRevokesAccess() public {
        registry.setClaim(orgId, writer, registry.CLAIM_WRITER(), true);
        vm.prank(writer);
        assertTrue(checker.isPermitted(writer, address(target), ""));

        registry.setClaim(orgId, writer, registry.CLAIM_WRITER(), false);
        vm.prank(writer);
        assertFalse(checker.isPermitted(writer, address(target), ""));
    }

    function testAdminPolicyRequiresAdminClaim() public {
        registry.setContractDefaultPolicy(address(target), registry.POLICY_ADMIN());

        vm.prank(stranger);
        assertFalse(checker.isPermitted(stranger, address(target), ""));

        vm.prank(owner);
        assertTrue(checker.isPermitted(owner, address(target), ""));
    }

    function testOrgMembersEnumeration() public {
        registry.setClaim(orgId, reader, registry.CLAIM_READER(), true);
        registry.setClaim(orgId, writer, registry.CLAIM_WRITER(), true);

        address[] memory members = registry.getOrgMembers(orgId);
        assertTrue(members.length >= 3);

        registry.setUserOrg(orgId, stranger);
        registry.setClaim(orgId, stranger, registry.CLAIM_WRITER(), true);
        vm.prank(stranger);
        assertTrue(checker.isPermitted(stranger, address(target), ""));
    }
}

contract TestTarget {}
