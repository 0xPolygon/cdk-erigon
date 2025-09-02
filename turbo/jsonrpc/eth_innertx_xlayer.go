package jsonrpc

import (
	"context"
	"errors"
	"fmt"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/eth/stagedsync/stages"
	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/turbo/rpchelper"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/ledgerwatch/erigon/zk/metrics"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
	"github.com/ledgerwatch/log/v3"
)

// GetInternalTransactions ...
func (api *APIImpl) GetInternalTransactions(ctx context.Context, txnHash libcommon.Hash) ([]*zktypes.InnerTx, error) {
	if !api.EnableInnerTx {
		return nil, errors.New("unsupported internal transaction method")
	}

	tx, err := api.db.BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	blockNum, ok, err := api.txnLookup(ctx, tx, txnHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("can't get the matching block")
	}
	block, err := api.blockByNumberWithSenders(ctx, tx, blockNum)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, fmt.Errorf("can't get the matching block")
	}

	var txnIndex uint64
	for idx, transaction := range block.Transactions() {
		if transaction.Hash() == txnHash {
			txnIndex = uint64(idx)
			break
		}
	}

	hermezReader := hermez_db.NewHermezDbReader(tx)
	blockInnerTxs := hermezReader.GetInnerTxs(blockNum)
	if len(blockInnerTxs) < len(block.Transactions()) {
		return nil, fmt.Errorf("block inner tx count %d is less than block tx count %d", len(blockInnerTxs), len(block.Transactions()))
	} else if len(blockInnerTxs) > len(block.Transactions()) {
		log.Warn(fmt.Sprintf("block inner tx count %d is greater than block tx count %d", len(blockInnerTxs), len(block.Transactions())))
	}

	return blockInnerTxs[txnIndex], nil
}

// GetBlockInternalTransactions ...
func (api *APIImpl) GetBlockInternalTransactions(ctx context.Context, number rpc.BlockNumber) (map[libcommon.Hash][]*zktypes.InnerTx, error) {
	if !api.EnableInnerTx {
		return nil, errors.New("unsupported internal transaction method")
	}

	tx, err := api.db.BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if number == rpc.PendingBlockNumber {
		return nil, fmt.Errorf("not supported pending block number")
	}

	// get latest executed block
	executedBlock, err := stages.GetStageProgress(tx, stages.Execution)
	if err != nil {
		return nil, err
	}

	// return null if requested block  is higher than executed
	// made for consistency with zkevm
	if number > 0 && executedBlock < uint64(number.Int64()) {
		return nil, fmt.Errorf("can't get the matching block")
	}

	n, _, _, err := rpchelper.GetBlockNumber(rpc.BlockNumberOrHashWithNumber(number), tx, api.filters)
	if err != nil {
		return nil, err
	}

	block, err := api.blockByNumberWithSenders(ctx, tx, n)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, fmt.Errorf("can't get the matching block")
	}

	hermezReader := hermez_db.NewHermezDbReader(tx)
	blockInnerTxs := hermezReader.GetInnerTxs(n)
	if len(blockInnerTxs) < len(block.Transactions()) {
		return nil, fmt.Errorf("block inner tx count %d is less than block tx count %d", len(blockInnerTxs), len(block.Transactions()))
	} else if len(blockInnerTxs) > len(block.Transactions()) {
		log.Warn(fmt.Sprintf("block inner tx count %d is greater than block tx count %d", len(blockInnerTxs), len(block.Transactions())))
	}
	metrics.IncRpcInnerTxExecuted(float64(len(blockInnerTxs)))

	res := make(map[libcommon.Hash][]*zktypes.InnerTx)
	for index, innerTxs := range blockInnerTxs[:len(block.Transactions())] {
		res[block.Transactions()[index].Hash()] = innerTxs
	}

	return res, nil
}
