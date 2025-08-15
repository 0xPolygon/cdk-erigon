// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/proxy/transparent/TransparentUpgradeableProxy.sol";

/**
 * @title TokenManagerProxy
 * @dev Transparent upgradeable proxy for TokenManager using OpenZeppelin standard
 * This provides a secure, battle-tested proxy implementation with minimal custom code
 */
contract TokenManagerProxy is TransparentUpgradeableProxy {
    /**
     * @dev Constructor for the proxy
     * @param logic Address of the implementation contract
     * @param admin Address of the proxy admin
     * @param data Initialization data for the implementation contract
     */
    constructor(
        address logic,
        address admin,
        bytes memory data
    ) TransparentUpgradeableProxy(logic, admin, data) {
        // OpenZeppelin TransparentUpgradeableProxy handles all the logic
        // No additional custom logic needed - keeps it simple and secure
    }
} 