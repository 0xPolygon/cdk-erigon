package jsonrpc

import (
	"context"

	"github.com/ledgerwatch/erigon-lib/common"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

// GetTransactionReceipt implements realtime_getTransactionReceipt.
// Returns the receipt of a transaction given the transaction's hash.
func (api *RealtimeAPIImpl) GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]interface{}, error) {
	if !api.enableFlag || api.cacheDB == nil {
		return nil, ErrRealtimeNotEnabled
	}

	txn, receipt, _, _, ok := api.cacheDB.Stateless.GetTxInfo(hash)
	if !ok {
		return api.ethApi.GetTransactionReceipt(ctx, hash)
	}
	header, _, ok := api.cacheDB.Stateless.GetHeader(receipt.BlockNumber.Uint64())
	if !ok {
		return api.ethApi.GetTransactionReceipt(ctx, hash)
	}

	tx, err := api.ethApi.db.BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	cc, err := api.ethApi.chainConfig(ctx, tx)
	if err != nil {
		return nil, err
	}

	return marshalReceipt(receipt, txn, cc, header, txn.Hash(), true), nil
}

// GetInternalTransactions implements realtime_getInternalTransactions.
// Returns the internal transactions of a transaction given the transaction's hash.
func (api *RealtimeAPIImpl) GetInternalTransactions(ctx context.Context, hash common.Hash) ([]*zktypes.InnerTx, error) {
	if !api.enableFlag || api.cacheDB == nil {
		return nil, ErrRealtimeNotEnabled
	}

	_, _, _, innerTxs, ok := api.cacheDB.Stateless.GetTxInfo(hash)
	if !ok {
		return api.ethApi.GetInternalTransactions(ctx, hash)
	}

	return innerTxs, nil
}
