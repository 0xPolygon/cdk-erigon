package realtimeapi

import (
	"context"
	"fmt"

	"github.com/ledgerwatch/erigon/turbo/jsonrpc"
	realtimeCache "github.com/ledgerwatch/erigon/zk/realtime/cache"
	"github.com/ledgerwatch/log/v3"
)

type RealtimeDebugApiImpl struct {
	*jsonrpc.PrivateDebugAPIImpl
	ethApi  *jsonrpc.APIImpl
	cacheDB *realtimeCache.RealtimeCache
}

func NewRealtimeDebugApiImpl(debugApi *jsonrpc.PrivateDebugAPIImpl, ethApi *jsonrpc.APIImpl, cacheDB *realtimeCache.RealtimeCache) *RealtimeDebugApiImpl {
	return &RealtimeDebugApiImpl{
		PrivateDebugAPIImpl: debugApi,
		ethApi:              ethApi,
		cacheDB:             cacheDB,
	}
}

func NewRealtimeDebugApi(debugApi *jsonrpc.PrivateDebugAPIImpl, ethApi *jsonrpc.APIImpl, cacheDB *realtimeCache.RealtimeCache) interface{} {
	return NewRealtimeDebugApiImpl(debugApi, ethApi, cacheDB)
}

func (api *RealtimeDebugApiImpl) RealtimeDumpCache(ctx context.Context) error {
	if api.cacheDB == nil || !api.cacheDB.ReadyFlag.Load() {
		// Custom for realtime
		return ErrRealtimeNotEnabled
	}

	if err := api.cacheDB.DebugDumpToFile(); err != nil {
		log.Error("[Realtime] Failed to dump state cache", "error", err)
		return fmt.Errorf("failed to dump state cache: %v", err)
	}

	return nil
}

func (api *RealtimeDebugApiImpl) RealtimeCompareStateCache(ctx context.Context) (*RealtimeDebugResult, error) {
	if api.cacheDB == nil || !api.cacheDB.ReadyFlag.Load() {
		// Custom for realtime
		return nil, ErrRealtimeNotEnabled
	}

	// Note that there is a chance there will be differences in state, as RT confirmed state might
	// be ahead of the chainstate confirmed state.
	reader, tx, err := api.ethApi.CreateLatestStateReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("compareStateCache cannot create latest state reader: %w", err)
	}
	defer tx.Rollback()

	mismatches, err := api.cacheDB.State.DebugCompare(reader)
	if err != nil {
		return nil, fmt.Errorf("compareStateCache cannot compare state cache: %w", err)
	}

	return &RealtimeDebugResult{
		ConfirmHeight:   api.cacheDB.GetHighestConfirmHeight(),
		ExecutionHeight: api.cacheDB.GetExecutionHeight(),
		Mismatches:      mismatches,
	}, nil
}
