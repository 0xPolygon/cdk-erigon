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

Vm constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

import "acl/AccessControlFirewall.sol";
import "acl/AdminUpgradeableProxy.sol";
import "acl/ProxyAdmin.sol";
import {ATarget, BTarget} from "../contracts/Targets.sol";

contract DeployACL {
    function run() external {
        uint256 pk = vm.envUint("PK");
        address owner = vm.addr(pk);

        vm.startBroadcast(pk);

        // Deploy ProxyAdmin and ACL logic with regular CREATE (works on all chains)
        ProxyAdmin proxyAdmin = new ProxyAdmin(owner);
        AccessControlFirewall logic = new AccessControlFirewall();

        // Encode initializer for ACL proxy
        bytes memory initData = abi.encodeWithSignature("initialize(address)", owner);

        // Deploy upgradeable proxy via CREATE
        AdminUpgradeableProxy proxy = new AdminUpgradeableProxy(
            address(logic),
            address(proxyAdmin),
            initData
        );

        // Deploy sample target contracts
        ATarget a = new ATarget();
        BTarget b = new BTarget();

        vm.stopBroadcast();

        // Persist addresses for the test stage
        string memory obj;
        obj = vm.serializeAddress("acl", "proxy", address(proxy));
        obj = vm.serializeAddress("acl", "logic", address(logic));
        obj = vm.serializeAddress("acl", "admin", address(proxyAdmin));
        obj = vm.serializeAddress("acl", "A", address(a));
        obj = vm.serializeAddress("acl", "B", address(b));
        vm.writeJson(obj, "out/acl.addresses.json");
    }
}
