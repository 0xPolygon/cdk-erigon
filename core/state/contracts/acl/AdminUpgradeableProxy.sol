// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import {TransparentUpgradeableProxy} from "@openzeppelin/contracts/proxy/transparent/TransparentUpgradeableProxy.sol";

contract AdminUpgradeableProxy is TransparentUpgradeableProxy {
    constructor(address logic, address admin_, bytes memory data) payable TransparentUpgradeableProxy(logic, admin_, data) {}
}
