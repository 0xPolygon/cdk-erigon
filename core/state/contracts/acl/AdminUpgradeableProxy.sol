pragma solidity ^0.8.23;

/// @title Minimal Transparent Upgradeable Proxy (EIP-1967 compatible)
/// @notice Admin can upgrade and change admin. Non-admin calls are delegated to implementation.
contract AdminUpgradeableProxy {
    // EIP-1967 slots: keccak256("eip1967.proxy.implementation") - 1, keccak256("eip1967.proxy.admin") - 1
    bytes32 private constant _IMPLEMENTATION_SLOT = bytes32(uint256(keccak256("eip1967.proxy.implementation")) - 1);
    bytes32 private constant _ADMIN_SLOT = bytes32(uint256(keccak256("eip1967.proxy.admin")) - 1);

    event Upgraded(address indexed implementation);
    event AdminChanged(address previousAdmin, address newAdmin);

    constructor(address _logic, address admin_, bytes memory _data) payable {
        require(_logic != address(0) && admin_ != address(0), "Proxy: zero");
        _setAdmin(admin_);
        _setImplementation(_logic);
        if (_data.length > 0) {
            (bool ok, ) = _logic.delegatecall(_data);
            require(ok, "Proxy: init failed");
        }
    }

    // --- Admin accessors ---
    function admin() public view returns (address a) {
        bytes32 slot = _ADMIN_SLOT;
        assembly { a := sload(slot) }
    }

    function implementation() public view returns (address impl) {
        bytes32 slot = _IMPLEMENTATION_SLOT;
        assembly { impl := sload(slot) }
    }

    function changeAdmin(address newAdmin) external onlyAdmin {
        require(newAdmin != address(0), "Proxy: zero admin");
        emit AdminChanged(admin(), newAdmin);
        _setAdmin(newAdmin);
    }

    function upgradeTo(address newImplementation) external onlyAdmin {
        _setImplementation(newImplementation);
        emit Upgraded(newImplementation);
    }

    modifier onlyAdmin() {
        require(msg.sender == admin(), "Proxy: not admin");
        _;
    }

    // --- Internal setters ---
    function _setAdmin(address newAdmin) internal {
        bytes32 slot = _ADMIN_SLOT;
        assembly { sstore(slot, newAdmin) }
    }

    function _setImplementation(address newImplementation) internal {
        require(newImplementation.code.length > 0, "Proxy: impl !code");
        bytes32 slot = _IMPLEMENTATION_SLOT;
        assembly { sstore(slot, newImplementation) }
    }

    // --- Fallback ---
    fallback() external payable {
        if (msg.sender == admin()) {
            // Admin calls only hit admin functions; unknown selectors revert to avoid clashing with implementation.
            revert("Proxy: admin cannot fallback");
        }
        _delegate(implementation());
    }

    receive() external payable {
        _delegate(implementation());
    }

    function _delegate(address impl) internal {
        assembly {
            calldatacopy(0, 0, calldatasize())
            let result := delegatecall(gas(), impl, 0, calldatasize(), 0, 0)
            returndatacopy(0, 0, returndatasize())
            switch result
                case 0 { revert(0, returndatasize()) }
                default { return(0, returndatasize()) }
        }
    }
}

