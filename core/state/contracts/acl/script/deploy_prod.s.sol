// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

// Minimal cheatcode interface (avoid external dependencies)
interface Vm {
    function startBroadcast(uint256 privateKey) external;
    function stopBroadcast() external;
    function envUint(string calldata key) external returns (uint256);
    function addr(uint256 privateKey) external returns (address);
    function serializeAddress(string calldata, string calldata, address) external returns (string memory);
    function writeJson(string calldata json, string calldata path) external;
}

Vm constant HEVM = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

import {AccessControlRBACChecker} from "../AccessControlRBACChecker.sol";
import {AccessControlRBACRegistry} from "../AccessControlRBACRegistry.sol";
import {Guard} from "../contracts/Guard.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";

contract DeployACL {
    function run() external {
        uint256 pk = HEVM.envUint("PK");
        address owner = HEVM.addr(pk);

        HEVM.startBroadcast(pk);

        // Deploy registry/checker implementations and wrap them in ERC1967 (UUPS) proxies
        AccessControlRBACRegistry registryImpl = new AccessControlRBACRegistry();
        bytes memory registryInit = abi.encodeCall(AccessControlRBACRegistry.initialize, owner);
        ERC1967Proxy registryProxy = new ERC1967Proxy(address(registryImpl), registryInit);
        AccessControlRBACRegistry registry = AccessControlRBACRegistry(address(registryProxy));

        AccessControlRBACChecker checkerImpl = new AccessControlRBACChecker();
        bytes memory checkerInit = abi.encodeCall(AccessControlRBACChecker.initialize, address(registry));
        ERC1967Proxy checkerProxy = new ERC1967Proxy(address(checkerImpl), checkerInit);
        AccessControlRBACChecker checker = AccessControlRBACChecker(address(checkerProxy));

        Guard guard = new Guard();

        HEVM.stopBroadcast();

        // Persist addresses for the test stage
        string memory obj;
        obj = HEVM.serializeAddress("acl", "proxy", address(checker));
        obj = HEVM.serializeAddress("acl", "logic", address(checkerImpl));
        obj = HEVM.serializeAddress("acl", "registry", address(registry));
        obj = HEVM.serializeAddress("acl", "registryLogic", address(registryImpl));
        obj = HEVM.serializeAddress("acl", "guard", address(guard));
        HEVM.writeJson(obj, "out/acl.addresses.json");
    }
}
