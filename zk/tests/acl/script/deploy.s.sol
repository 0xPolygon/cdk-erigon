// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.23;

interface Vm {
    function startBroadcast(uint256 privateKey) external;
    function stopBroadcast() external;
    function envUint(string calldata key) external returns (uint256);
    function addr(uint256 privateKey) external returns (address);
    function serializeAddress(string calldata, string calldata, address) external returns (string memory);
    function writeJson(string calldata json, string calldata path) external;
}

Vm constant vm = Vm(address(uint160(uint256(keccak256("hevm cheat code")))));

import "acl/AccessControlRBACChecker.sol";
import "acl/AccessControlRBACRegistry.sol";
import "@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol";
import "@openzeppelin/contracts/proxy/transparent/TransparentUpgradeableProxy.sol";
import {ATarget, BTarget} from "../contracts/Targets.sol";

contract DeployACL {
    function run() external {
        uint256 pk = vm.envUint("PK");
        address owner = vm.addr(pk);

        vm.startBroadcast(pk);

        AccessControlRBACRegistry registry = new AccessControlRBACRegistry();
        registry.initialize(owner);
        AccessControlRBACChecker checker = new AccessControlRBACChecker();
        ProxyAdmin proxyAdmin = new ProxyAdmin(owner);
        bytes memory initData = abi.encodeWithSignature("initialize(address)", address(registry));
        TransparentUpgradeableProxy proxy = new TransparentUpgradeableProxy(address(checker), address(proxyAdmin), initData);

        ATarget a;
        BTarget b;
        bool deployTargets;
        try vm.envUint("DEPLOY_SAMPLE_TARGETS") returns (uint256 flag) {
            deployTargets = flag != 0;
        } catch {
            deployTargets = true;
        }
        if (deployTargets) {
            a = new ATarget();
            b = new BTarget();
        }

        vm.stopBroadcast();

        string memory obj;
        obj = vm.serializeAddress("acl", "proxy", address(proxy));
        obj = vm.serializeAddress("acl", "logic", address(checker));
        obj = vm.serializeAddress("acl", "registry", address(registry));
        obj = vm.serializeAddress("acl", "admin", address(proxyAdmin));
        obj = vm.serializeAddress("acl", "A", address(a));
        obj = vm.serializeAddress("acl", "B", address(b));
        vm.writeJson(obj, "out/acl.addresses.json");
    }
}
