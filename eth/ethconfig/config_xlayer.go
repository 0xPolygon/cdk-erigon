package ethconfig

import (
	"time"

	"github.com/ledgerwatch/erigon-lib/common"
)

// XLayerConfig is the X Layer config used on the eth backend
type XLayerConfig struct {
	Apollo        ApolloClientConfig
	Nacos         NacosConfig
	EnableInnerTx bool
	// Sequencer
	SequencerBatchSleepDuration time.Duration

	// Local Replay
	SequencerReplay                   bool
	SequencerReplayHaltOnBatchNumber  uint64
	SequencerReplayExternalDatastream bool
	SequencerReplayL1SyncOnly         bool

	// PreRun
	PreRunList      map[common.Address]struct{}
	PreRunCacheSize int
	PreRunCacheTTL  time.Duration
	PreRunChanNum   int
	PreRunTaskNum   int
	// Executor
	ExecutorMock        bool
	BlockInfoConcurrent bool

	// Bulk Add Txs
	BulkAddTxs         bool
	BulkAddTxsSize     int
	BulkAddTxsWaitTime time.Duration
	EnableAddTxNotify  bool
}

var DefaultXLayerConfig = XLayerConfig{}

// NacosConfig is the config for nacos
type NacosConfig struct {
	URLs               string
	NamespaceId        string
	ApplicationName    string
	ExternalListenAddr string
}

// ApolloClientConfig is the config for apollo
type ApolloClientConfig struct {
	Enable        bool
	IP            string
	AppID         string
	NamespaceName string
}
