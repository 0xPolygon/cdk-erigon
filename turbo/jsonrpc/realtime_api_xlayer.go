package jsonrpc

import (
	"fmt"
	"math/big"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/rpc"
	realtimeCache "github.com/ledgerwatch/erigon/zk/realtime/cache"
	"github.com/ledgerwatch/erigon/zk/realtime/subscription"
)

var (
	mockBlockHash                   = libcommon.BytesToHash([]byte{1})
	ErrRealtimeNotEnabled           = fmt.Errorf("realtime is not enabled")
	ErrRealtimeConfirmBlockNotFound = fmt.Errorf("realtime confirm block not found")
)

// RealtimeAPIImpl is implementation of the RealtimeAPI interface
type RealtimeAPIImpl struct {
	cacheDB    *realtimeCache.RealtimeCache
	subService *subscription.RealtimeSubscription
	ethApi     *APIImpl
	enableFlag bool
}

// NewRealtimeAPI returns RealtimeAPIImpl instance
func NewRealtimeAPI(
	cacheDB *realtimeCache.RealtimeCache,
	subService *subscription.RealtimeSubscription,
	base *APIImpl,
	enableFlag bool,
) *RealtimeAPIImpl {

	return &RealtimeAPIImpl{
		cacheDB:    cacheDB,
		subService: subService,
		ethApi:     base,
		enableFlag: enableFlag,
	}
}

func (api *RealtimeAPIImpl) getBlockNumber(blockNr rpc.BlockNumber) (uint64, bool, error) {
	if !api.enableFlag || api.cacheDB == nil {
		return 0, false, ErrRealtimeNotEnabled
	}

	confirmHeight := api.cacheDB.GetHighestConfirmHeight()
	if confirmHeight == 0 {
		return 0, false, fmt.Errorf("no block number found in stateless cache")
	}

	switch blockNr {
	case rpc.LatestBlockNumber:
		return confirmHeight, true, nil
	case rpc.EarliestBlockNumber:
		// Unsupported
		return 0, false, fmt.Errorf("earliest block number is not supported")
	case rpc.FinalizedBlockNumber:
		return confirmHeight, true, nil
	case rpc.SafeBlockNumber:
		return confirmHeight, true, nil
	case rpc.PendingBlockNumber:
		pendingHeight := api.cacheDB.GetHighestPendingHeight()
		if pendingHeight == 0 {
			return 0, false, fmt.Errorf("no block number found in stateless cache")
		}
		return pendingHeight, true, nil
	case rpc.LatestExecutedBlockNumber:
		return confirmHeight, true, nil
	default:
		blockNumber := uint64(blockNr.Int64())
		if blockNumber > confirmHeight {
			return 0, false, fmt.Errorf("block with number %d not found", blockNumber)
		}
		return blockNumber, blockNumber == confirmHeight, nil
	}
}

// newRPCTransaction_realtime returns a transaction that will serialize to the RPC
// representation, with the given location metadata set (if available).
// Note that realtime API do not support blockHash.
func newRPCTransaction_realtime(tx types.Transaction, blockNumber uint64, index uint64, baseFee *big.Int) *RPCTransaction {
	result := NewRPCTransaction(tx, mockBlockHash, blockNumber, index, baseFee)
	result.BlockHash = &libcommon.Hash{}
	return result
}
