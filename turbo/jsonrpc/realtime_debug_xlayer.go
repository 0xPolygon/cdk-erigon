package jsonrpc

import (
	"context"
	"fmt"

	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/turbo/rpchelper"
	"github.com/ledgerwatch/erigon/zkevm/log"
)

func (api *RealtimeAPIImpl) DebugDumpCache(ctx context.Context) error {
	if !api.enableFlag || api.cacheDB == nil {
		return ErrRealtimeNotEnabled
	}

	if err := api.cacheDB.DebugDumpToFile(); err != nil {
		log.Error("[Realtime] Failed to dump state cache", "error", err)
		return fmt.Errorf("failed to dump state cache: %v", err)
	}

	return nil
}

func (api *RealtimeAPIImpl) DebugCompareStateCache(ctx context.Context) ([]string, error) {
	if !api.enableFlag || api.cacheDB == nil || api.cacheDB.State == nil {
		return nil, ErrRealtimeNotEnabled
	}

	tx, err := api.ethApi.db.BeginRo(ctx)
	if err != nil {
		return nil, fmt.Errorf("compareStateCache cannot open tx: %w", err)
	}
	defer tx.Rollback()
	blockNumber := rpc.LatestBlockNumber
	reader, err := rpchelper.CreateStateReader(ctx, tx, rpc.BlockNumberOrHash{BlockNumber: &blockNumber}, 0, api.ethApi.filters, api.ethApi.stateCache, api.ethApi.historyV3(tx), "")
	if err != nil {
		return nil, err
	}

	mismatches := api.cacheDB.State.DebugCompare(reader)
	return mismatches, nil
}
