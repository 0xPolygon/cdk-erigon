package ethermanpool

import (
	"context"
	"math/big"

	ethereum "github.com/erigontech/erigon"
	"github.com/erigontech/erigon-lib/common"
	ethTypes "github.com/erigontech/erigon/core/types"
)

// IEtherman defines the interface for Ethereum client operations
// Used by syncer, gas tracker, and other components that need to interact with L1
type IEtherman interface {
	HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethTypes.Header, error)
	BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethTypes.Block, error)
	FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]ethTypes.Log, error)
	CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	TransactionByHash(ctx context.Context, hash common.Hash) (ethTypes.Transaction, bool, error)
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*ethTypes.Receipt, error)
	StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error)
	SuggestedGasPrice(ctx context.Context) (*big.Int, error)
}

// IMultiEtherman is an optional interface for pools with multiple clients
// Implementations can be type-asserted to this interface to get client count
// for worker scaling purposes
type IMultiEtherman interface {
	IEtherman
	ClientCount() int
}

// HeaderBatcher is an optional interface for batch header retrieval
// Implemented by some clients for efficiency
type HeaderBatcher interface {
	HeadersByNumbers(ctx context.Context, numbers []*big.Int) ([]*ethTypes.Header, error)
}
