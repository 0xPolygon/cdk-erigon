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
import {ProxyAdmin} from "@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol";
import {TransparentUpgradeableProxy} from "@openzeppelin/contracts/proxy/transparent/TransparentUpgradeableProxy.sol";

contract DeployACL {
    function run() external {
        uint256 pk = HEVM.envUint("PK");
        address owner = HEVM.addr(pk);

        HEVM.startBroadcast(pk);

        // Deploy registry/checker and an OpenZeppelin proxy stack
        AccessControlRBACRegistry registry = new AccessControlRBACRegistry();
        registry.initialize(owner);

        AccessControlRBACChecker checker = new AccessControlRBACChecker();

        // Admin for the transparent proxy
        ProxyAdmin proxyAdmin = new ProxyAdmin(owner);

        // Encode initializer to link the checker with the registry
        bytes memory initData = abi.encodeWithSignature("initialize(address)", address(registry));

        TransparentUpgradeableProxy proxy = new TransparentUpgradeableProxy(
            address(checker),
            address(proxyAdmin),
            initData
        );

        HEVM.stopBroadcast();

        // Persist addresses for the test stage
        string memory obj;
        obj = HEVM.serializeAddress("acl", "proxy", address(proxy));
        obj = HEVM.serializeAddress("acl", "logic", address(checker));
        obj = HEVM.serializeAddress("acl", "registry", address(registry));
        obj = HEVM.serializeAddress("acl", "admin", address(proxyAdmin));
        HEVM.writeJson(obj, "out/acl.addresses.json");
    }
}
