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
	SeqPrefix                     = "sequencer_"
	BatchExecuteTimeName          = SeqPrefix + "batch_execute_time"
	PoolTxCountName               = SeqPrefix + "pool_tx_count"
	SeqTxDurationName             = SeqPrefix + "tx_duration"
	SeqTxCountName                = SeqPrefix + "tx_count"
	SeqZKOverflowBlockCounterName = SeqPrefix + "zk_overflow_block_count"
	SeqBlockGasUsedName           = SeqPrefix + "block_gas_used"

	RpcPrefix = "rpc_"
)

func Init() {
	prometheus.MustRegister(BatchExecuteTimeGauge)
	prometheus.MustRegister(PoolTxCount)
	prometheus.MustRegister(SeqTxDuration)
	prometheus.MustRegister(SeqTxCount)
	prometheus.MustRegister(SeqZKOverflowBlockCounter)
	prometheus.MustRegister(SeqBlockGasUsed)
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
