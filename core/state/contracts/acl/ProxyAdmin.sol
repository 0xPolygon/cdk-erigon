// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import {ProxyAdmin as OProxyAdmin} from "@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol";

contract ProxyAdmin is OProxyAdmin {
    constructor(address owner) OProxyAdmin(owner) {}
}
