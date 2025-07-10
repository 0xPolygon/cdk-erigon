package jsonrpc

import (
	"context"

	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon/rpc"
)

// BlockNumber implements realtime_blockNumber.
// Returns the block number of the most recent confirmed block.
func (api *RealtimeAPIImpl) BlockNumber(ctx context.Context) (hexutil.Uint64, error) {
	if !api.enableFlag || api.cacheDB == nil {
		return hexutil.Uint64(0), ErrRealtimeNotEnabled
	}

	blockNumber, _, err := api.getBlockNumber(rpc.LatestBlockNumber)
	if err != nil {
		return api.ethApi.BlockNumber(ctx)
	}
	return hexutil.Uint64(blockNumber), nil
}

// PendingBlockNumber implements realtime_pendingBlockNumber.
// Returns the block number of the most recent pre-confirmed block.
func (api *RealtimeAPIImpl) PendingBlockNumber(ctx context.Context) (hexutil.Uint64, error) {
	if !api.enableFlag || api.cacheDB == nil {
		return hexutil.Uint64(0), ErrRealtimeNotEnabled
	}

	blockNumber, _, err := api.getBlockNumber(rpc.PendingBlockNumber)
	if err != nil {
		return api.ethApi.BlockNumber(ctx)
	}
	return hexutil.Uint64(blockNumber), nil
}

// GetBlockTransactionCountByNumber implements realtime_getBlockTransactionCountByNumber.
// Returns the number of transactions in a block given the block's block number.
func (api *RealtimeAPIImpl) GetBlockTransactionCountByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*hexutil.Uint, error) {
	if !api.enableFlag || api.cacheDB == nil {
		return nil, ErrRealtimeNotEnabled
	}

	blockNum, _, err := api.getBlockNumber(blockNr)
	if err != nil {
		return api.ethApi.GetBlockTransactionCountByNumber(ctx, blockNr)
	}

	_, _, ok := api.cacheDB.Stateless.GetHeader(blockNum)
	if !ok {
		return nil, ErrRealtimeConfirmBlockNotFound
	}

	txs, _ := api.cacheDB.Stateless.GetBlockTxs(blockNum)
	numOfTx := hexutil.Uint(0)
	if ok {
		numOfTx = hexutil.Uint(len(txs))
	}

	return &numOfTx, nil
}
