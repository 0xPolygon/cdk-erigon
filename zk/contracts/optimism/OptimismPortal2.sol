// SPDX-License-Identifier: MIT
pragma solidity ^0.8.15;

// Minimal Optimism Portal to emit TransactionDeposited events.
import {AddressAliasHelper} from "./AddressAliasHelper.sol";

contract OptimismPortal2 {
    event TransactionDeposited(address indexed from, address indexed to, uint256 indexed version, bytes opaqueData);

    uint256 internal constant DEPOSIT_VERSION = 0;
    uint64 internal constant RECEIVE_DEFAULT_GAS_LIMIT = 100_000;

    function depositTransaction(
        address _to,
        uint256 _value,
        uint64 _gasLimit,
        bool _isCreation,
        bytes calldata _data
    ) external payable {
        if (_isCreation && _to != address(0)) revert BadTarget();
        if (_gasLimit < minimumGasLimit(uint64(_data.length))) revert GasLimitTooLow();
        if (_data.length > 120_000) revert CalldataTooLarge();

        address from = msg.sender;
        if (msg.sender != tx.origin) {
            from = AddressAliasHelper.applyL1ToL2Alias(msg.sender);
        }

        bytes memory opaqueData = abi.encodePacked(msg.value, _value, _gasLimit, _isCreation, _data);
        emit TransactionDeposited(from, _to, DEPOSIT_VERSION, opaqueData);
    }

    receive() external payable {
        bytes memory opaqueData = abi.encodePacked(msg.value, uint256(0), RECEIVE_DEFAULT_GAS_LIMIT, false, hex"");
        emit TransactionDeposited(msg.sender, msg.sender, DEPOSIT_VERSION, opaqueData);
    }

    function minimumGasLimit(uint64 _byteCount) public pure returns (uint64) {
        unchecked {
            return uint64(200_000 + 16 * _byteCount); // TODO Is this number good for us?
        }
    }

    error BadTarget();
    error GasLimitTooLow();
    error CalldataTooLarge();
}
