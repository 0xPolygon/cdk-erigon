package metrics

import (
	"strconv"
	"sync"
	"time"

	"github.com/ledgerwatch/log/v3"
)

var instance *statisticsInstance
var once sync.Once

func GetLogStatistics() Statistics {
	once.Do(func() {
		instance = &statisticsInstance{}
		instance.resetStatistics()
	})
	return instance
}

type statisticsInstance struct {
	newRoundTime time.Time
	statistics   map[logTag]int64 // value maybe the counter or time.Duration(ms)
	tags         map[logTag]string
}

func (l *statisticsInstance) CumulativeCounting(tag logTag) {
	l.statistics[tag]++
}

func (l *statisticsInstance) CumulativeValue(tag logTag, value int64) {
	l.statistics[tag] += value
}

func (l *statisticsInstance) CumulativeTiming(tag logTag, duration time.Duration) {
	l.statistics[tag] += duration.Milliseconds()
}

func (l *statisticsInstance) SetTag(tag logTag, value string) {
	l.tags[tag] = value
}

func (l *statisticsInstance) resetStatistics() {
	l.newRoundTime = time.Now()
	l.statistics = make(map[logTag]int64)
	l.tags = make(map[logTag]string)
}

func (l *statisticsInstance) Summary() string {
	batch := "Batch<" + l.tags[FinalizeBatchNumber] + ">, "
	totalDuration := "TotalDuration<" + strconv.Itoa(int(time.Since(l.newRoundTime).Milliseconds())) + "ms>, "
	gasUsed := "GasUsed<" + strconv.Itoa(int(l.statistics[BatchGas])) + ">, "
	blockCount := "Block<" + strconv.Itoa(int(l.statistics[BlockCounter])) + ">, "
	tx := "Tx<" + strconv.Itoa(int(l.statistics[TxCounter])) + ">, "
	getTxPause := "GetTxPause<" + strconv.Itoa(int(l.statistics[GetTxPauseCounter])) + ">, "
	reprocessTx := "ReprocessTx<" + strconv.Itoa(int(l.statistics[ReprocessingTxCounter])) + ">, "
	gasOverTx := "GasOverTx<" + strconv.Itoa(int(l.statistics[FailTxGasOverCounter])) + ">, "
	zkOverflowBlock := "ZKOverflowBlock<" + strconv.Itoa(int(l.statistics[ZKOverflowBlockCounter])) + ">, "
	invalidTx := "InvalidTx<" + strconv.Itoa(int(l.statistics[ProcessingInvalidTxCounter])) + ">, "
	sequencingBatchTiming := "SequencingBatchTiming<" + strconv.Itoa(int(l.statistics[SequencingBatchTiming])) + "ms>, "
	getTxTiming := "GetTxTiming<" + strconv.Itoa(int(l.statistics[GetTxTiming])) + "ms>, "
	getTxPauseTiming := "GetTxPauseTiming<" + strconv.Itoa(int(l.statistics[GetTxPauseTiming])) + "ms>, "
	processTxTiming := "ProcessTx<" + strconv.Itoa(int(l.statistics[ProcessingTxTiming])) + "ms>, "
	batchCommitDBTiming := "BatchCommitDBTiming<" + strconv.Itoa(int(l.statistics[BatchCommitDBTiming])) + "ms>, "
	pbStateTiming := "PbStateTiming<" + strconv.Itoa(int(l.statistics[PbStateTiming])) + "ms>, "
	zkIncIntermediateHashesTiming := "ZkIncIntermediateHashesTiming<" + strconv.Itoa(int(l.statistics[ZkIncIntermediateHashesTiming])) + "ms>, "
	finaliseBlockWriteTiming := "FinaliseBlockWriteTiming<" + strconv.Itoa(int(l.statistics[FinaliseBlockWriteTiming])) + "ms>, "
	batchCloseReason := "BatchCloseReason<" + l.tags[BatchCloseReason] + ">"

	result := batch + totalDuration + gasUsed + blockCount + tx + getTxPause +
		reprocessTx + gasOverTx + zkOverflowBlock + invalidTx + sequencingBatchTiming + getTxTiming + processTxTiming + getTxPauseTiming + pbStateTiming +
		zkIncIntermediateHashesTiming + finaliseBlockWriteTiming + batchCommitDBTiming +
		batchCloseReason
	log.Info(result)
	l.resetStatistics()
	return result
}

func (l *statisticsInstance) GetTag(tag logTag) string {
	return l.tags[tag]
}

func (l *statisticsInstance) GetStatistics(tag logTag) int64 {
	return l.statistics[tag]
}
