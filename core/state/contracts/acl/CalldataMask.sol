pragma solidity ^0.8.23;

library CalldataMask {
    /// @notice Checks if calldata `data` matches (data & mask) == value for the prefix of length mask.length.
    /// If `mask` is empty, returns true.
    /// Note: `mask` and `value` are in memory to allow passing from storage without calldata restrictions.
    function matches(bytes calldata data, bytes memory mask, bytes memory value) internal pure returns (bool) {
        uint256 mlen = mask.length;
        if (mlen == 0) return true;
        if (mlen != value.length) return false;
        if (data.length < mlen) return false;
        for (uint256 i = 0; i < mlen; i++) {
            if ((data[i] & mask[i]) != value[i]) return false;
        }
        return true;
    }
}
