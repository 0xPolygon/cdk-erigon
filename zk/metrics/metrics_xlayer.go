package metrics

import (
	"fmt"
	"time"

	"github.com/ledgerwatch/log/v3"
	"github.com/prometheus/client_golang/prometheus"
)

type BatchFinalizeType string

const (
	BatchTimeOut         BatchFinalizeType = "EmptyBatchTimeOut"
	BatchCounterOverflow BatchFinalizeType = "BatchCounterOverflow"
	BatchLimboRecovery   BatchFinalizeType = "LimboRecovery"
)

var (
	SeqPrefix            = "sequencer_"
	BatchExecuteTimeName = SeqPrefix + "batch_execute_time"
	PoolTxCountName      = SeqPrefix + "pool_tx_count"
	SeqTxDurationName    = SeqPrefix + "tx_duration"
	SeqTxCountName       = SeqPrefix + "tx_count"
	SeqZKOverflowBlockCounterName   = SeqPrefix + "zk_overflow_block_count"
	SeqBlockGasUsedName  = SeqPrefix + "block_gas_used"
	
	// Batch timing metrics
	SeqBatchDurationName = SeqPrefix + "batch_duration"
	SeqSequencingBatchTimingName = SeqPrefix + "sequencing_batch_timing"
	SeqProcessTxTimingName = SeqPrefix + "process_tx_timing"
	SeqGetTxTimingName = SeqPrefix + "get_tx_timing"
	SeqGetTxPauseTimingName = SeqPrefix + "get_tx_pause_timing"
	SeqPbStateTimingName = SeqPrefix + "pb_state_timing"
	SeqZkIncIntermediateHashesTimingName = SeqPrefix + "zk_inc_intermediate_hashes_timing"
	SeqFinaliseBlockWriteTimingName = SeqPrefix + "finalise_block_write_timing"
	SeqBatchCommitDBTimingName = SeqPrefix + "batch_commit_db_timing"

	RpcPrefix              = "rpc_"
	RpcDynamicGasPriceName = RpcPrefix + "dynamic_gas_price"
	RpcInnerTxExecutedName = RpcPrefix + "inner_tx_executed"
)

func Init() {
	prometheus.MustRegister(BatchExecuteTimeGauge)
	prometheus.MustRegister(PoolTxCount)
	prometheus.MustRegister(SeqTxDuration)
	prometheus.MustRegister(SeqTxCount)
	prometheus.MustRegister(SeqZKOverflowBlockCounter)
	prometheus.MustRegister(SeqBlockGasUsed)
	prometheus.MustRegister(RpcDynamicGasPrice)
	prometheus.MustRegister(RpcInnerTxExecuted)
	
	// Register new batch timing metrics
	prometheus.MustRegister(SeqBatchDuration)
	prometheus.MustRegister(SeqSequencingBatchTiming)
	prometheus.MustRegister(SeqProcessTxTiming)
	prometheus.MustRegister(SeqGetTxTiming)
	prometheus.MustRegister(SeqGetTxPauseTiming)
	prometheus.MustRegister(SeqPbStateTiming)
	prometheus.MustRegister(SeqZkIncIntermediateHashesTiming)
	prometheus.MustRegister(SeqFinaliseBlockWriteTiming)
	prometheus.MustRegister(SeqBatchCommitDBTiming)
}

var BatchExecuteTimeGauge = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: BatchExecuteTimeName,
		Help: "[SEQUENCER] batch execution time in second",
	},
	[]string{"closingReason"},
)

var PoolTxCount = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: PoolTxCountName,
		Help: "[SEQUENCER] tx count of each pool in tx pool",
	},
	[]string{"poolName"},
)

func BatchExecuteTime(closingReason string, duration time.Duration) {
	log.Info(fmt.Sprintf("[BatchExecuteTime] ClosingReason: %v, Duration: %.2fs", closingReason, duration.Seconds()))
	BatchExecuteTimeGauge.WithLabelValues(closingReason).Set(duration.Seconds())
}

func AddPoolTxCount(pending, baseFee, queued int) {
	log.Info(fmt.Sprintf("[PoolTxCount] pending: %v, basefee: %v, queued: %v", pending, baseFee, queued))
	PoolTxCount.WithLabelValues("pending").Set(float64(pending))
	PoolTxCount.WithLabelValues("basefee").Set(float64(baseFee))
	PoolTxCount.WithLabelValues("queued").Set(float64(queued))
}

var RpcDynamicGasPrice = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: RpcDynamicGasPriceName,
		Help: "[RPC] dynamic gas price",
	},
)

var RpcInnerTxExecuted = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: RpcInnerTxExecutedName,
		Help: "[RPC] inner tx executed, used to trace contract calls in blockchain explorer",
	},
)

var SeqTxDuration = prometheus.NewSummary(
	prometheus.SummaryOpts{
		Name: SeqTxDurationName,
		Help: "[SEQUENCER] tx processing duration in millisecond (ms)",
		Objectives: map[float64]float64{
			0.5:  0.05,  // 50th percentile (median) with 5% error
			0.9:  0.01,  // 90th percentile with 1% error
			0.95: 0.005, // 95th percentile with 0.5% error
			0.99: 0.001, // 99th percentile with 0.1% error
		},
	},
)

var SeqTxCount = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: SeqTxCountName,
		Help: "[SEQUENCER] total processed tx count",
	},
)

var SeqZKOverflowBlockCounter = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: SeqZKOverflowBlockCounterName,
		Help: "[SEQUENCER] zkCounter overflow block count",
	},
)

var SeqBlockGasUsed = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: SeqBlockGasUsedName,
		Help: "[SEQUENCER] gas used per block",
	},
)

// Batch timing metrics
var SeqBatchDuration = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: SeqBatchDurationName,
		Help: "[SEQUENCER] total batch duration in milliseconds",
	},
)

var SeqSequencingBatchTiming = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: SeqSequencingBatchTimingName,
		Help: "[SEQUENCER] sequencing batch timing in milliseconds",
	},
)

var SeqProcessTxTiming = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: SeqProcessTxTimingName,
		Help: "[SEQUENCER] process transaction timing in milliseconds",
	},
)

var SeqGetTxTiming = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: SeqGetTxTimingName,
		Help: "[SEQUENCER] get transaction timing in milliseconds",
	},
)

var SeqGetTxPauseTiming = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: SeqGetTxPauseTimingName,
		Help: "[SEQUENCER] get transaction pause timing in milliseconds",
	},
)

var SeqPbStateTiming = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: SeqPbStateTimingName,
		Help: "[SEQUENCER] pb state timing in milliseconds",
	},
)

var SeqZkIncIntermediateHashesTiming = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: SeqZkIncIntermediateHashesTimingName,
		Help: "[SEQUENCER] zk increment intermediate hashes timing in milliseconds",
	},
)

var SeqFinaliseBlockWriteTiming = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: SeqFinaliseBlockWriteTimingName,
		Help: "[SEQUENCER] finalise block write timing in milliseconds",
	},
)

var SeqBatchCommitDBTiming = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: SeqBatchCommitDBTimingName,
		Help: "[SEQUENCER] batch commit DB timing in milliseconds",
	},
)
