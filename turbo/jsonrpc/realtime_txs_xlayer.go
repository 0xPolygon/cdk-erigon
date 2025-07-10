package jsonrpc

import (
	"bytes"
	"context"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
)

// GetTransactionByHash implements realtime_getTransactionByHash.
// Returns information about a transaction given the transaction's hash.
func (api *RealtimeAPIImpl) GetTransactionByHash(ctx context.Context, txnHash common.Hash, includeExtraInfo *bool) (interface{}, error) {
	if !api.enableFlag || api.cacheDB == nil {
		return nil, ErrRealtimeNotEnabled
	}

	txn, _, blockNum, _, ok := api.cacheDB.Stateless.GetTxInfo(txnHash)
	if !ok {
		return api.ethApi.GetTransactionByHash(ctx, txnHash, includeExtraInfo)
	}
	txHashes, ok := api.cacheDB.Stateless.GetBlockTxs(blockNum)
	if !ok {
		return api.ethApi.GetTransactionByHash(ctx, txnHash, includeExtraInfo)
	}
	header, _, ok := api.cacheDB.Stateless.GetHeader(blockNum)
	if !ok {
		return api.ethApi.GetTransactionByHash(ctx, txnHash, includeExtraInfo)
	}

	found := false
	var txnIndex uint64
	for i, hash := range txHashes {
		if hash == txnHash {
			found = true
			txnIndex = uint64(i)
			break
		}
	}
	if !found || txn == nil {
		return api.ethApi.GetTransactionByHash(ctx, txnHash, includeExtraInfo)
	}

	return newRPCTransaction_realtime(txn, blockNum, txnIndex, header.BaseFee), nil
}

// GetRawTransactionByHash implements realtime_getRawTransactionByHash.
// Returns the bytes of the transaction for the given hash.
func (api *RealtimeAPIImpl) GetRawTransactionByHash(ctx context.Context, hash common.Hash) (hexutility.Bytes, error) {
	if !api.enableFlag || api.cacheDB == nil {
		return nil, ErrRealtimeNotEnabled
	}

	txn, _, _, _, ok := api.cacheDB.Stateless.GetTxInfo(hash)
	if !ok || txn == nil {
		return api.ethApi.GetRawTransactionByHash(ctx, hash)
	}

	var buf bytes.Buffer
	err := txn.MarshalBinary(&buf)

	return buf.Bytes(), err
}
