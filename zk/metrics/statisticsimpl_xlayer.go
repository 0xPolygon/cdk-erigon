package metrics

import (
	"fmt"
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
	statistics   map[LogTag]int64 // value maybe the counter or time.Duration(ms)
	tags         map[LogTag]string
}

func (l *statisticsInstance) CumulativeCounting(tag LogTag) {
	l.statistics[tag]++
}

func (l *statisticsInstance) CumulativeValue(tag LogTag, value int64) {
	l.statistics[tag] += value
}

func (l *statisticsInstance) CumulativeTiming(tag LogTag, duration time.Duration) {
	l.statistics[tag] += duration.Milliseconds()
}

func (l *statisticsInstance) CumulativeMicroTiming(tag LogTag, duration time.Duration) {
	l.statistics[tag] += duration.Microseconds()
}

func (l *statisticsInstance) SetTag(tag LogTag, value string) {
	l.tags[tag] = value
}

func (l *statisticsInstance) resetStatistics() {
	l.newRoundTime = time.Now()
	l.statistics = make(map[LogTag]int64)
	l.tags = make(map[LogTag]string)
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
	batchCloseReason := "BatchCloseReason<" + l.tags[BatchCloseReason] + ">,"
	zkHashAccCount := "zkHashAccCount<acc:" + strconv.Itoa(int(l.statistics[ZKHashAccountCount])) + ", store:" + strconv.Itoa(int(l.statistics[ZKHashStoreCount])) + ", code:" + strconv.Itoa(int(l.statistics[ZKHashCodeCount])) + ">, "
	zkHashSMTCount := "zkHashSMTCount<delByNode:" + strconv.Itoa(int(l.statistics[ZKHashSMTDeleteByNodeKey])) + "-" + fmt.Sprintf("%.0f", float64(l.statistics[ZKHashSMTDeleteByNodeKeyTiming])/1000.0) + "ms, delHash:" + strconv.Itoa(int(l.statistics[ZKHashSMTDeleteHashKey])) + "-" + fmt.Sprintf("%.0f", float64(l.statistics[ZKHashSMTDeleteHashKeyTiming])/1000.0) + "ms, ins:" + strconv.Itoa(int(l.statistics[ZKHashSMTInsertKey])) + "-" + fmt.Sprintf("%.0f", float64(l.statistics[ZKHashSMTInsertKeyTiming])/1000.0) + "ms, get:" + strconv.Itoa(int(l.statistics[ZKHashSMTGetKey])) + "-" + fmt.Sprintf("%.0f", float64(l.statistics[ZKHashSMTGetKeyTiming])/1000.0) + "ms>, "
	zkHermezSmtMetadata := "HermezSmtMetadata<" + l.tags[HermezSmtMetadata] + "," + strconv.Itoa(int(l.statistics[HermezSmtMetadata])) + "ms>, "
	zkHermezSmtStats := "HermezSmtStats<" + l.tags[HermezSmtStats] + "," + strconv.Itoa(int(l.statistics[HermezSmtStats])) + "ms>, "
	zkHermezSmt := "HermezSmt<" + l.tags[HermezSmt] + "," + strconv.Itoa(int(l.statistics[HermezSmt])) + "ms>, "
	zkHermezSmtHashKey := "HermezSmtHashKey<" + l.tags[HermezSmtHashKey] + "," + strconv.Itoa(int(l.statistics[HermezSmtHashKey])) + "ms>, "
	deleteLog := "Delete<" + fmt.Sprintf("%.0f", float64(l.statistics[Delete])/1000.0) + "ms>, "
	appendLog := "Append<" + fmt.Sprintf("%.0f", float64(l.statistics[Append])/1000.0) + "ms>, "
	putLog := "Put<" + fmt.Sprintf("%.0f", float64(l.statistics[Put])/1000.0) + "ms>"

	result := batch + totalDuration + gasUsed + blockCount + tx + getTxPause +
		reprocessTx + gasOverTx + zkOverflowBlock + invalidTx + sequencingBatchTiming + getTxTiming + processTxTiming + getTxPauseTiming + pbStateTiming +
		zkIncIntermediateHashesTiming + finaliseBlockWriteTiming + batchCommitDBTiming +
		batchCloseReason + zkHashAccCount + zkHashSMTCount + zkHermezSmtMetadata + zkHermezSmtStats + zkHermezSmt + zkHermezSmtHashKey +
		deleteLog + appendLog + putLog
	log.Info(result)
	l.resetStatistics()
	return result
}

func (l *statisticsInstance) GetTag(tag LogTag) string {
	return l.tags[tag]
}

func (l *statisticsInstance) GetStatistics(tag LogTag) int64 {
	return l.statistics[tag]
}
