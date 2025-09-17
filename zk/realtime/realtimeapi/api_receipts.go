package realtimeapi

import (
	"context"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/turbo/jsonrpc"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

// GetTransactionReceipt implements the realtime eth_getTransactionReceipt.
// Returns the receipt of a transaction given the transaction's hash.
func (api *RealtimeAPIImpl) GetTransactionReceipt(ctx context.Context, hash common.Hash) (map[string]interface{}, error) {
	if api.cacheDB == nil || !api.cacheDB.ReadyFlag.Load() {
		return api.APIImpl.GetTransactionReceipt(ctx, hash)
	}

	txn, receipt, _, _, ok := api.cacheDB.Stateless.GetTxInfo(hash)
	if !ok {
		return api.APIImpl.GetTransactionReceipt(ctx, hash)
	}
	header, _, blockhash, ok := api.cacheDB.Stateless.GetBlockInfo(receipt.BlockNumber.Uint64())
	if !ok {
		return api.APIImpl.GetTransactionReceipt(ctx, hash)
	}
	if blockhash != EmptyBlockHash {
		receipt.BlockHash = blockhash
		for _, log := range receipt.Logs {
			log.BlockHash = blockhash
		}
	}

	tx, err := api.APIImpl.GetDB().BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	cc, err := api.APIImpl.GetChainConfig(ctx, tx)
	if err != nil {
		return nil, err
	}

	return jsonrpc.MarshalReceipt(receipt, txn, cc, header, txn.Hash(), true), nil
}

// GetInternalTransactions implements the realtime eth_getInternalTransactions.
// Returns the internal transactions of a transaction given the transaction's hash.
func (api *RealtimeAPIImpl) GetInternalTransactions(ctx context.Context, hash common.Hash) ([]*zktypes.InnerTx, error) {
	if api.cacheDB == nil || !api.cacheDB.ReadyFlag.Load() {
		return api.APIImpl.GetInternalTransactions(ctx, hash)
	}

	_, _, _, innerTxs, ok := api.cacheDB.Stateless.GetTxInfo(hash)
	if !ok {
		return api.APIImpl.GetInternalTransactions(ctx, hash)
	}

	return innerTxs, nil
}

func (api *RealtimeAPIImpl) GetBlockReceipts(ctx context.Context, number rpc.BlockNumberOrHash) ([]map[string]interface{}, error) {
	if api.cacheDB == nil || !api.cacheDB.ReadyFlag.Load() {
		return api.APIImpl.GetBlockReceipts(ctx, number)
	}

	blockNum, _, _, err := api.getBlockNumberOrHash(number)
	if err != nil {
		return api.APIImpl.GetBlockReceipts(ctx, number)
	}

	header, _, blockhash, ok := api.cacheDB.Stateless.GetBlockInfo(blockNum)
	if !ok {
		return api.APIImpl.GetBlockReceipts(ctx, number)
	}

	txHashes, ok := api.cacheDB.Stateless.GetBlockTxs(blockNum)
	if !ok {
		return api.APIImpl.GetBlockReceipts(ctx, number)
	}

	tx, err := api.APIImpl.GetDB().BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	cc, err := api.APIImpl.GetChainConfig(ctx, tx)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(txHashes))
	for _, txHash := range txHashes {
		txn, receipt, _, _, exists := api.cacheDB.Stateless.GetTxInfo(txHash)
		if !exists {
			return api.APIImpl.GetBlockReceipts(ctx, number)
		}
		if blockhash != EmptyBlockHash {
			receipt.BlockHash = blockhash
			for _, log := range receipt.Logs {
				log.BlockHash = blockhash
			}
		}
		result = append(result, jsonrpc.MarshalReceipt(receipt, txn, cc, header, txn.Hash(), true))
	}

	return result, nil
}
