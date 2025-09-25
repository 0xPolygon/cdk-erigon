pragma solidity ^0.8.23;

import {AdminUpgradeableProxy} from "./AdminUpgradeableProxy.sol";

/// @title ProxyAdmin - manages AdminUpgradeableProxy instances
/// @notice Standard pattern: keep admin keys here (ideally multisig) and point proxies to this contract as their admin.
contract ProxyAdmin {
    address public owner;

    event OwnershipTransferred(address indexed oldOwner, address indexed newOwner);

    modifier onlyOwner() {
        require(msg.sender == owner, "ProxyAdmin: not owner");
        _;
    }

    constructor(address initialOwner) {
        require(initialOwner != address(0), "ProxyAdmin: zero owner");
        owner = initialOwner;
    }

    function transferOwnership(address newOwner) external onlyOwner {
        require(newOwner != address(0), "ProxyAdmin: zero owner");
        emit OwnershipTransferred(owner, newOwner);
        owner = newOwner;
    }

    function getProxyAdmin(AdminUpgradeableProxy proxy) public view returns (address) {
        (bool ok, bytes memory data) = address(proxy).staticcall(abi.encodeWithSignature("admin()"));
        require(ok && data.length == 32, "ProxyAdmin: admin() failed");
        return abi.decode(data, (address));
    }

    function getProxyImplementation(AdminUpgradeableProxy proxy) public view returns (address) {
        (bool ok, bytes memory data) = address(proxy).staticcall(abi.encodeWithSignature("implementation()"));
        require(ok && data.length == 32, "ProxyAdmin: implementation() failed");
        return abi.decode(data, (address));
    }

    function changeProxyAdmin(AdminUpgradeableProxy proxy, address newAdmin) external onlyOwner {
        (bool ok, ) = address(proxy).call(abi.encodeWithSignature("changeAdmin(address)", newAdmin));
        require(ok, "ProxyAdmin: change admin failed");
    }

    function upgrade(AdminUpgradeableProxy proxy, address newImplementation) external onlyOwner {
        (bool ok, ) = address(proxy).call(abi.encodeWithSignature("upgradeTo(address)", newImplementation));
        require(ok, "ProxyAdmin: upgrade failed");
    }
}

