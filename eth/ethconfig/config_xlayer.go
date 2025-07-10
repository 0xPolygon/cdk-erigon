package ethconfig

import (
	"time"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/zk/nacos"
)

// AnalysisGroupVerificationConfig contains configuration for analysis group verification
type AnalysisGroupVerificationConfig struct {
	BatchDelay  uint64                   // Number of batches to delay before verifying the last block of a batch
	NacosClient *nacos.XlayerNacosClient // Nacos client for analysis group service discovery
	APIPath     string                   // API path for analysis group verification
	SkipAPI     bool                     // If true, skip calling analysis group API and directly set block number to verified status
}

// XLayerConfig is the X Layer config used on the eth backend
type XLayerConfig struct {
	Apollo        ApolloClientConfig
	Nacos         NacosConfig
	EnableInnerTx bool
	ApolloChanged []string
	// Sequencer
	SequencerBatchSleepDuration time.Duration
	StandaloneSMTDatabase       bool

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
	BlockInfoConcurrent bool

	EnableAsyncCommit bool
	// Bulk Add Txs
	BulkAddTxs         bool
	BulkAddTxsSize     int
	BulkAddTxsWaitTime time.Duration
	EnableAddTxNotify  bool

	SequencerSkipEmptyBlocks  bool
	SequencerMaxBlockSealTime time.Duration

	GetLogsTimeout time.Duration
	GetLogsRetries int

	TraceLogPath   string
	EnableTraceLog bool

	SequencerBatchCounterPercentage int

	// Analysis Group Verification
	AnalysisGroupVerification AnalysisGroupVerificationConfig
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
