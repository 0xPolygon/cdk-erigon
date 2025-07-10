package jsonrpc

import (
	"context"
	"fmt"

	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	"github.com/ledgerwatch/erigon/rpc"
	ethapi2 "github.com/ledgerwatch/erigon/turbo/adapter/ethapi"
	"github.com/ledgerwatch/erigon/turbo/transactions"
)

// Call implements realtime_call.
// Executes a new message call immediately without creating a transaction on the block chain.
// Note that realtime API only supports execution on the latest block.
func (api *RealtimeAPIImpl) Call(ctx context.Context, args ethapi2.CallArgs, overrides *ethapi2.StateOverrides) (hexutility.Bytes, error) {
	if !api.enableFlag || api.cacheDB == nil {
		return nil, ErrRealtimeNotEnabled
	}

	tx, err := api.ethApi.db.BeginRo(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	chainConfig, err := api.ethApi.chainConfig(ctx, tx)
	if err != nil {
		return nil, err
	}
	engine := api.ethApi.engine()

	if args.Gas == nil || uint64(*args.Gas) == 0 {
		args.Gas = (*hexutil.Uint64)(&api.ethApi.GasCap)
	}

	blockNumber, _, err := api.getBlockNumber(rpc.PendingBlockNumber)
	if err != nil {
		return nil, err
	}

	header, _, ok := api.cacheDB.Stateless.GetHeader(blockNumber)
	if !ok {
		return nil, fmt.Errorf("header not found for block number %d", blockNumber)
	}

	bn := rpc.BlockNumber(blockNumber)
	rpcBlockNr := rpc.BlockNumberOrHash{BlockNumber: &bn}
	result, err := transactions.DoCall(ctx, engine, args, tx, rpcBlockNr, header, overrides, api.ethApi.GasCap, chainConfig, api.cacheDB.State, api.cacheDB.Stateless, api.ethApi.evmCallTimeout)
	if err != nil {
		return nil, err
	}

	if len(result.ReturnData) > api.ethApi.ReturnDataLimit {
		return nil, fmt.Errorf("call returned result on length %d exceeding --rpc.returndata.limit %d", len(result.ReturnData), api.ethApi.ReturnDataLimit)
	}

	// If the result contains a revert reason, try to unpack and return it.
	if len(result.Revert()) > 0 {
		return nil, ethapi2.NewRevertError(result)
	}

	return result.Return(), result.Err
}
