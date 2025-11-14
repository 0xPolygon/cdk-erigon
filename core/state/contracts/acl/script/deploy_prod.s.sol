// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

// Minimal cheatcode interface (avoid external dependencies)
interface Vm {
    function startBroadcast(uint256 privateKey) external;
    function stopBroadcast() external;
    function envUint(string calldata key) external returns (uint256);
    function envOr(string calldata key, string calldata defaultValue) external returns (string memory);
    function addr(uint256 privateKey) external returns (address);
    function serializeAddress(string calldata, string calldata, address) external returns (string memory);
    function writeJson(string calldata json, string calldata path) external;
}

Vm constant HEVM = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

import {AccessControlRBACChecker} from "../AccessControlRBACChecker.sol";
import {AccessControlRBACRegistry} from "../AccessControlRBACRegistry.sol";
import {AccessControlClaimRegistry} from "../AccessControlClaimRegistry.sol";
import {Guard} from "../contracts/Guard.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";

contract DeployACL {
    function run() external {
        uint256 pk = HEVM.envUint("PK");
        address owner = HEVM.addr(pk);
        string memory registryKind = HEVM.envOr("ACL_REGISTRY_KIND", "rbac");
        bool useClaimRegistry = _equalsIgnoreCase(registryKind, "claim");

        HEVM.startBroadcast(pk);

        // Deploy registry/checker implementations and wrap them in ERC1967 (UUPS) proxies
        address registryProxyAddr;
        address registryLogicAddr;
        if (useClaimRegistry) {
            AccessControlClaimRegistry claimImpl = new AccessControlClaimRegistry();
            bytes memory registryInit = abi.encodeCall(AccessControlClaimRegistry.initialize, owner);
            ERC1967Proxy registryProxy = new ERC1967Proxy(address(claimImpl), registryInit);
            registryProxyAddr = address(registryProxy);
            registryLogicAddr = address(claimImpl);
        } else {
            AccessControlRBACRegistry registryImpl = new AccessControlRBACRegistry();
            bytes memory registryInit = abi.encodeCall(AccessControlRBACRegistry.initialize, owner);
            ERC1967Proxy registryProxy = new ERC1967Proxy(address(registryImpl), registryInit);
            registryProxyAddr = address(registryProxy);
            registryLogicAddr = address(registryImpl);
        }

        AccessControlRBACChecker checkerImpl = new AccessControlRBACChecker();
        bytes memory checkerInit = abi.encodeCall(AccessControlRBACChecker.initialize, registryProxyAddr);
        ERC1967Proxy checkerProxy = new ERC1967Proxy(address(checkerImpl), checkerInit);
        AccessControlRBACChecker checker = AccessControlRBACChecker(address(checkerProxy));

        Guard guard = new Guard();

        HEVM.stopBroadcast();

        // Persist addresses for the test stage
        string memory obj;
        obj = HEVM.serializeAddress("acl", "proxy", address(checker));
        obj = HEVM.serializeAddress("acl", "logic", address(checkerImpl));
        obj = HEVM.serializeAddress("acl", "registry", registryProxyAddr);
        obj = HEVM.serializeAddress("acl", "registryLogic", registryLogicAddr);
        obj = HEVM.serializeAddress("acl", "guard", address(guard));
        HEVM.writeJson(obj, "out/acl.addresses.json");
    }

    function _equalsIgnoreCase(string memory a, string memory b) internal pure returns (bool) {
        bytes memory ba = bytes(a);
        bytes memory bb = bytes(b);
        if (ba.length != bb.length) return false;
        for (uint256 i = 0; i < ba.length; ++i) {
            bytes1 ca = ba[i];
            bytes1 cb = bb[i];
            if (ca >= 0x41 && ca <= 0x5A) {
                ca = bytes1(uint8(ca) + 32);
            }
            if (cb >= 0x41 && cb <= 0x5A) {
                cb = bytes1(uint8(cb) + 32);
            }
            if (ca != cb) return false;
        }
        return true;
    }
}
