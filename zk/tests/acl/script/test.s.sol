// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

// Minimal cheatcode interface
interface Vm {
    function startBroadcast(uint256 privateKey) external;
    function stopBroadcast() external;
    function envUint(string calldata key) external returns (uint256);
    function addr(uint256 privateKey) external returns (address);
    function readFile(string calldata path) external returns (string memory);
    function parseJsonAddress(string calldata json, string calldata key) external returns (address);
}

Vm constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

import "acl/IAccessControlFirewall.sol";
import {ATarget, BTarget} from "../contracts/Targets.sol";

contract TestACL {
    function run() external {
        uint256 pk = vm.envUint("PK");
        address subject = vm.addr(pk);

        // Load addresses from deployment output
        string memory json = vm.readFile("out/acl.addresses.json");
        address aclProxy = vm.parseJsonAddress(json, ".proxy");
        address aAddr = vm.parseJsonAddress(json, ".A");
        address bAddr = vm.parseJsonAddress(json, ".B");

        IAccessControlFirewall acl = IAccessControlFirewall(aclProxy);
        ATarget a = ATarget(aAddr);
        BTarget b = BTarget(bAddr);

        // Begin broadcasting from the same EOA used for deployment (ACL owner)
        vm.startBroadcast(pk);

        // 1) Allowed call: grant selector for A.setX(uint256) and execute
        acl.grantSelector(subject, aAddr, ATarget.setX.selector);
        (bool okSetX, ) = aAddr.call(abi.encodeWithSelector(ATarget.setX.selector, uint256(777)));
        require(okSetX, "expected allowed A.setX");

        // 2) Denied call: selector not granted (A.clearX())
        (bool okClearX, ) = aAddr.call(abi.encodeWithSelector(ATarget.clearX.selector));
        require(!okClearX, "expected deny for ungranted selector A.clearX");

        // 3) Param mask constraint: allow A.setY only if arg == 42
        acl.grantSelector(subject, aAddr, ATarget.setY.selector);
        // The ACL checks (calldata & mask) == value for the first mask.length bytes.
        // Build 36-byte mask/value: 4 bytes selector (ignored) + 32-byte arg (match last 4 bytes to 0x2a)
        // Mask: match only last 4 bytes of the 32-byte arg (sufficient for small values like 42)
        bytes memory m = hex"00000000"; // selector ignored
        m = bytes.concat(m, hex"00000000000000000000000000000000000000000000000000000000ffffffff");
        bytes memory v = hex"00000000"; // selector ignored
        v = bytes.concat(v, hex"000000000000000000000000000000000000000000000000000000000000002a");
        acl.setParamConstraint(subject, aAddr, ATarget.setY.selector, m, v);

        // Call with disallowed value -> expect deny
        (bool okSetYBad, ) = aAddr.call(abi.encodeWithSelector(ATarget.setY.selector, uint256(43)));
        require(!okSetYBad, "expected deny for A.setY(43)");
        // Call with allowed value -> expect success
        (bool okSetYGood, ) = aAddr.call(abi.encodeWithSelector(ATarget.setY.selector, uint256(42)));
        require(okSetYGood, "expected allow for A.setY(42)");

        // 4) Nested CALL denied: allow top-level B.nestedCall(address), deny nested A.setZ
        acl.grantSelector(subject, bAddr, BTarget.nestedCall.selector);
        (bool okNested, ) = bAddr.call(abi.encodeWithSelector(BTarget.nestedCall.selector, aAddr));
        require(!okNested, "expected deny for nested call to A.setZ via B.nestedCall");

        // 5) CREATE denied: allow top-level B.doCreate(), but deny contract creation permission
        acl.grantSelector(subject, bAddr, BTarget.doCreate.selector);
        (bool okCreateDenied, ) = bAddr.call(abi.encodeWithSelector(BTarget.doCreate.selector));
        require(!okCreateDenied, "expected deny for CREATE without grantContractCreation");

        // Grant contract creation permission, expect CREATE to succeed now
        acl.grantContractCreation(subject);
        (bool okCreateAllowed, ) = bAddr.call(abi.encodeWithSelector(BTarget.doCreate.selector));
        require(okCreateAllowed, "expected allow for CREATE after grantContractCreation");

        vm.stopBroadcast();
    }
}
