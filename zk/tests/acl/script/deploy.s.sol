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
import "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";
import {ATarget, BTarget} from "../contracts/Targets.sol";

contract DeployACL {
    function run() external {
        uint256 pk = vm.envUint("PK");
        address owner = vm.addr(pk);

        vm.startBroadcast(pk);

        AccessControlRBACRegistry registryImpl = new AccessControlRBACRegistry();
        bytes memory registryInit = abi.encodeCall(AccessControlRBACRegistry.initialize, owner);
        ERC1967Proxy registryProxy = new ERC1967Proxy(address(registryImpl), registryInit);
        AccessControlRBACRegistry registry = AccessControlRBACRegistry(address(registryProxy));

        AccessControlRBACChecker checkerImpl = new AccessControlRBACChecker();
        bytes memory checkerInit = abi.encodeCall(AccessControlRBACChecker.initialize, address(registry));
        ERC1967Proxy proxy = new ERC1967Proxy(address(checkerImpl), checkerInit);
        AccessControlRBACChecker checker = AccessControlRBACChecker(address(proxy));

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
        obj = vm.serializeAddress("acl", "proxy", address(checker));
        obj = vm.serializeAddress("acl", "logic", address(checkerImpl));
        obj = vm.serializeAddress("acl", "registry", address(registry));
        obj = vm.serializeAddress("acl", "registryLogic", address(registryImpl));
        obj = vm.serializeAddress("acl", "A", address(a));
        obj = vm.serializeAddress("acl", "B", address(b));
        vm.writeJson(obj, "out/acl.addresses.json");
    }
}
