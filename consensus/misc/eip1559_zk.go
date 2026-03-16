package misc

import (
	"math/big"

	"github.com/erigontech/erigon-lib/chain"
	"github.com/erigontech/erigon/core/types"
)

func CalcBaseFeeZk(config *chain.Config, parent *types.Header) *big.Int {
	if config.AllowFreeTransactions {
		return big.NewInt(0)
	}

	if parent.Number.Cmp(big.NewInt(0)) == 0 && !config.IsFep(parent.Time) {
		// If the parent is the genesis block, the next block will include the initial batch transaction, which is a legacy transaction, so the basefee will be set to 0
		return big.NewInt(0)
	}

	// If the parent block is injected block from L1 at block 1 (while block 0 is the genesis), it will have base fee of 0 so we will set the basefee of current block to ZK default gas price.
	if parent.Number.Cmp(big.NewInt(1)) == 0 && !config.IsFep(parent.Time) {
		return new(big.Int).SetUint64(config.ZkDefaultGasPrice)
	}

	// For  pre-London hard fork, base fee is not applicable.
	if !config.IsLondon(parent.Number.Uint64() + 1) {
		return big.NewInt(0)
	}

	// If we are switching from gasless to gas, we will set the basefee to ZK default gas price.
	if (parent.BaseFee == nil || parent.BaseFee.Cmp(big.NewInt(0)) == 0) && config.IsLondon(parent.Number.Uint64()) {
		return new(big.Int).SetUint64(config.ZkDefaultGasPrice)
	}

	return CalcBaseFee(config, parent)
}

// RecomputeBaseFeeSentinel is a special value used to mark headers whose basefee
// must be recomputed at execution time (e.g. when datastream did not provide it).
// We use uint256 max as a sentinel; realistic basefees will never reach it.
var RecomputeBaseFeeSentinel = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
