package ethconfig

import (
	"math/big"
	"time"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/zk/nacos"
	"github.com/ledgerwatch/erigon/zk/realtime"
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

	Realtime realtime.RealtimeConfig

	// Bridge Transaction Interception
	BridgeIntercept BridgeInterceptConfig

	DynamicBlockGasLimit uint64

	EnableLatestDataStreamBlockNumberGlobalVariableForRpc bool

	DataStreamUnwindToBlock uint64
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

// BridgeInterceptConfig represents the configuration for bridge transaction interception
type BridgeInterceptConfig struct {
	// PolygonZkEVMBridge contract address
	BridgeContractAddress string `json:"bridge_contract_address"`

	// Target token address (originTokenAddress to intercept)
	TargetTokenAddress string `json:"target_token_address"`

	// Maximum bridge amount (as big.Int to avoid overflow)
	MaxBridgeAmount *big.Int `json:"max_bridge_amount"`

	// Whether whitelist is enabled
	WhitelistEnabled bool `json:"whitelist_enabled"`

	// Whitelist addresses (as common.Address to avoid conversion)
	WhitelistAddresses []common.Address `json:"whitelist_addresses"`
}
