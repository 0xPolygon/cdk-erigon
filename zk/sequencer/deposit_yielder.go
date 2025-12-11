package sequencer

import (
	"fmt"

	libcommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/zk/deposits"
	"github.com/holiman/uint256"
)

// DepositTransactionYielder exposes parsed deposit logs grouped per L1 block.
type DepositTransactionYielder struct {
	cache *deposits.Cache
}

func NewDepositTransactionYielder(cache *deposits.Cache) *DepositTransactionYielder {
	return &DepositTransactionYielder{cache: cache}
}

// NextBlock returns the next queued deposit block whose L1 block number is greater than after.
func (d *DepositTransactionYielder) NextBlock(after uint64) *deposits.BlockDeposits {
	if d == nil || d.cache == nil {
		return nil
	}
	return d.cache.PopNext(after)
}

// BuildTransactions converts deposit payloads into typed deposit transactions.
func (d *DepositTransactionYielder) BuildTransactions(block *deposits.BlockDeposits) ([]types.Transaction, error) {
	if block == nil {
		return nil, nil
	}
	txs := make([]types.Transaction, 0, len(block.Deposits))
	for _, dep := range block.Deposits {
		tx, err := buildDepositTransaction(dep)
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func buildDepositTransaction(dep *deposits.Deposit) (*types.DepositTx, error) {
	if dep == nil {
		return nil, fmt.Errorf("nil deposit")
	}
	mint, overflow := uint256.FromBig(dep.Mint)
	if overflow {
		return nil, fmt.Errorf("deposit mint exceeds uint256 for source %s", dep.SourceHash.Hex())
	}
	value, overflow := uint256.FromBig(dep.Value)
	if overflow {
		return nil, fmt.Errorf("deposit value exceeds uint256 for source %s", dep.SourceHash.Hex())
	}

	var to *libcommon.Address
	if dep.To != nil {
		addr := *dep.To
		to = &addr
	}

	return types.NewDepositTx(
		dep.SourceHash,
		dep.From,
		to,
		mint,
		value,
		dep.Gas,
		dep.IsCreation,
		dep.Data,
	)
}
