package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

type BatchFinalizeType string

const (
	BatchTimeOut         BatchFinalizeType = "EmptyBatchTimeOut"
	BatchCounterOverflow BatchFinalizeType = "BatchCounterOverflow"
	BatchLimboRecovery   BatchFinalizeType = "LimboRecovery"
)

var (
	// OperationTiming tracks operation timing in seconds
	OperationTiming = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "xlayer_operation_timing_seconds",
			Help:    "Xlayer operation timing in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 3.0, 5.0, 10.0, 15.0, 30.0, 60.0},
		},
		[]string{"component", "metric_type"},
	)

	// OperationGauge tracks current state of operations
	OperationGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "xlayer_operation_current",
			Help: "Current state of xlayer operations (timing in seconds, others in original units)",
		},
		[]string{"component", "metric_type"},
	)

	// OperationCounter counts operations
	OperationCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xlayer_operation_counter",
			Help: "Total count of xlayer operations",
		},
		[]string{"component", "metric_type"},
	)

	// Gas metrics
	SeqBlockGasUsed = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "seq_block_gas_used",
			Help: "Sequencer gas used per block",
		},
	)

	RpcDynamicGasPrice = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "rpc_dynamic_gas_price",
			Help: "Rpc dynamic gas price",
		},
	)

	TxsInBlock = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "txs_in_block",
			Help: "tx count per block",
		},
	)
)

// Init registers all metrics with Prometheus
func Init() {
	prometheus.MustRegister(OperationTiming)
	prometheus.MustRegister(OperationCounter)
	prometheus.MustRegister(OperationGauge)
	prometheus.MustRegister(SeqBlockGasUsed)
	prometheus.MustRegister(RpcDynamicGasPrice)
	prometheus.MustRegister(TxsInBlock)
}

// Block timing functions
func RecordBlockExecuteTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("block", "execute").Observe(seconds)
	OperationGauge.WithLabelValues("block", "execute").Set(seconds)
}

func RecordBlockProcessTxTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("block", "process_tx").Observe(seconds)
	OperationGauge.WithLabelValues("block", "process_tx").Set(seconds)
}

func RecordBlockGetTxTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("block", "get_tx").Observe(seconds)
	OperationGauge.WithLabelValues("block", "get_tx").Set(seconds)
}

func RecordBlockGetTxPauseTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("block", "get_tx_pause").Observe(seconds)
	OperationGauge.WithLabelValues("block", "get_tx_pause").Set(seconds)
}

func RecordBlockSetSmtCacheTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("block", "set_smt_cache").Observe(seconds)
	OperationGauge.WithLabelValues("block", "set_smt_cache").Set(seconds)
}

// Batch timing functions
func RecordBatchExecuteTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("batch", "execute").Observe(seconds)
	OperationGauge.WithLabelValues("batch", "execute").Set(seconds)
}

func RecordBatchSequencingTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("batch", "sequencing").Observe(seconds)
	OperationGauge.WithLabelValues("batch", "sequencing").Set(seconds)
}

func RecordBatchProcessTxTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("batch", "process_tx").Observe(seconds)
	OperationGauge.WithLabelValues("batch", "process_tx").Set(seconds)
}

func RecordBatchGetTxTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("batch", "get_tx").Observe(seconds)
	OperationGauge.WithLabelValues("batch", "get_tx").Set(seconds)
}

func RecordBatchGetTxPauseTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("batch", "get_tx_pause").Observe(seconds)
	OperationGauge.WithLabelValues("batch", "get_tx_pause").Set(seconds)
}

func RecordBatchPbStateTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("batch", "pb_state").Observe(seconds)
	OperationGauge.WithLabelValues("batch", "pb_state").Set(seconds)
}

func RecordBatchZkIncIntermediateHashesTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("batch", "zk_inc_intermediate_hashes").Observe(seconds)
	OperationGauge.WithLabelValues("batch", "zk_inc_intermediate_hashes").Set(seconds)
}

func RecordBatchFinaliseBlockWriteTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("batch", "finalise_block_write").Observe(seconds)
	OperationGauge.WithLabelValues("batch", "finalise_block_write").Set(seconds)
}

func RecordBatchSmtCommitDBTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("batch", "smt_commit_db").Observe(seconds)
	OperationGauge.WithLabelValues("batch", "smt_commit_db").Set(seconds)
}

func RecordBatchCommitDBTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("batch", "commit_db").Observe(seconds)
	OperationGauge.WithLabelValues("batch", "commit_db").Set(seconds)
}

func RecordBatchSetSmtCacheTimingMs(durationMs int64) {
	seconds := float64(durationMs) / 1000.0
	OperationTiming.WithLabelValues("batch", "set_smt_cache").Observe(seconds)
	OperationGauge.WithLabelValues("batch", "set_smt_cache").Set(seconds)
}

// Gauge functions
func SetBlockNumber(gasUsed float64) {
	OperationGauge.WithLabelValues("block", "number").Set(gasUsed)
}

func SetBatchNumber(gasUsed float64) {
	OperationGauge.WithLabelValues("batch", "number").Set(gasUsed)
}

// Counter functions
func IncBlockTxCount(txCount float64) {
	OperationCounter.WithLabelValues("block", "tx_count").Add(txCount)
}

func IncBlockInvalidTxCount(invalidTxCount float64) {
	OperationCounter.WithLabelValues("block", "invalid_tx_count").Add(invalidTxCount)
}

func IncBatchTxCount(txCount float64) {
	OperationCounter.WithLabelValues("batch", "tx_count").Add(txCount)
}

func IncBatchInvalidTxCount(invalidTxCount float64) {
	OperationCounter.WithLabelValues("batch", "invalid_tx_count").Add(invalidTxCount)
}

func IncRpcInnerTxExecuted(innerTxCount float64) {
	OperationCounter.WithLabelValues("rpc", "inner_tx_count").Add(innerTxCount)
}

func CountTxInBlock(txCount float64) {
	TxsInBlock.Set(txCount)
}
