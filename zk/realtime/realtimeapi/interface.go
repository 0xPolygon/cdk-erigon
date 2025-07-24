package realtimeapi

import (
	"context"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	"github.com/ledgerwatch/erigon/eth/filters"
	"github.com/ledgerwatch/erigon/rpc"
	ethapi2 "github.com/ledgerwatch/erigon/turbo/adapter/ethapi"
	realtimeSub "github.com/ledgerwatch/erigon/zk/realtime/subscription"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

type RealtimeAPI interface {
	// Block related (see ./realtime_blocks_xlayer.go)
	BlockNumber(ctx context.Context) (hexutil.Uint64, error)
	GetBlockTransactionCountByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*hexutil.Uint, error)

	// Transaction related (see ./realtime_txs_xlayer.go)
	GetTransactionByHash(ctx context.Context, hash libcommon.Hash, includeExtraInfo *bool) (interface{}, error)
	GetRawTransactionByHash(ctx context.Context, hash libcommon.Hash) (hexutility.Bytes, error)

	// Receipt related (see ./realtime_receipts_xlayer.go)
	GetTransactionReceipt(ctx context.Context, hash libcommon.Hash) (map[string]interface{}, error)
	GetInternalTransactions(ctx context.Context, hash libcommon.Hash) ([]*zktypes.InnerTx, error)

	// Account related (see ./realtime_accounts_xlayer.go)
	GetBalance(ctx context.Context, address libcommon.Address) (*hexutil.Big, error)
	GetTransactionCount(ctx context.Context, address libcommon.Address) (*hexutil.Uint64, error)
	GetCode(ctx context.Context, address libcommon.Address) (hexutility.Bytes, error)
	GetStorageAt(ctx context.Context, address libcommon.Address, index string) (string, error)

	// Sending related (see ./realtime_call_xlayer.go)
	Call(ctx context.Context, args ethapi2.CallArgs, overrides *ethapi2.StateOverrides) (hexutility.Bytes, error)

	// Debug related (see ./realtime_debug.go)
	DebugDumpCache(ctx context.Context) error
	DebugCompareStateCache(ctx context.Context) (*DebugResult, error)
}

type RealtimeSubscriptionAPI interface {
	// Ws subscription related (see ./realtime_filters_xlayer.go)
	RealtimeTransactions(ctx context.Context, criteria realtimeSub.StreamCriteria) (*rpc.Subscription, error)
	Logs(ctx context.Context, crit filters.FilterCriteria) (*rpc.Subscription, error)
}
