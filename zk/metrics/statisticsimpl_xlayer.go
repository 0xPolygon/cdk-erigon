package metrics

import (
	"fmt"
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
	mu            sync.RWMutex
	newRoundTime  time.Time
	newBlockTime  time.Time
	statistics    map[LogTag]int64 // value maybe the counter or time.Duration(ms)
	statisticsOld map[LogTag]int64
	tags          map[LogTag]string
}

func (l *statisticsInstance) CumulativeCounting(tag LogTag) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.statistics[tag]++
}

func (l *statisticsInstance) CumulativeValue(tag LogTag, value int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.statistics[tag] += value
}

func (l *statisticsInstance) CumulativeTiming(tag LogTag, duration time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.statistics[tag] += duration.Milliseconds()
}

func (l *statisticsInstance) CumulativeMicroTiming(tag LogTag, duration time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.statistics[tag] += duration.Microseconds()
}

func (l *statisticsInstance) SetTag(tag LogTag, value string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tags[tag] = value
}

func (l *statisticsInstance) GetTag(tag LogTag) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.tags[tag]
}

func (l *statisticsInstance) GetStatistics(tag LogTag) int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.statistics[tag]
}

func (l *statisticsInstance) resetStatistics() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.newRoundTime = time.Now()
	l.newBlockTime = time.Now()
	l.statistics = make(map[LogTag]int64)
	l.statisticsOld = make(map[LogTag]int64)
	l.tags = make(map[LogTag]string)
}

func (l *statisticsInstance) SummaryCheckpoint() string {
	l.mu.RLock()
	block := l.tags[FinalizeBlockNumber]
	blockDuration := time.Since(l.newBlockTime).Milliseconds()
	blockGasUsed := l.statistics[BatchGas] - l.statisticsOld[BatchGas]
	blockTx := l.statistics[TxCounter] - l.statisticsOld[TxCounter]
	blockGetTxPause := l.statistics[GetTxPauseCounter] - l.statisticsOld[GetTxPauseCounter]
	blockGasOverTx := l.statistics[FailTxGasOverCounter] - l.statisticsOld[FailTxGasOverCounter]
	blockZkOverflowBlock := (l.statistics[ZKOverflowBlockCounter] - l.statisticsOld[ZKOverflowBlockCounter]) == 1
	blockInvalidTx := l.statistics[ProcessingInvalidTxCounter] - l.statisticsOld[ProcessingInvalidTxCounter]
	blockGetTxTiming := l.statistics[GetTxTiming] - l.statisticsOld[GetTxTiming]
	blockGetTxPauseTiming := l.statistics[GetTxPauseTiming] - l.statisticsOld[GetTxPauseTiming]
	blockProcessTxTiming := l.statistics[ProcessingTxTiming] - l.statisticsOld[ProcessingTxTiming]
	blockBatchCommitDBTiming := l.statistics[BatchCommitDBTiming] - l.statisticsOld[BatchCommitDBTiming]
	blockPbStateTiming := l.statistics[PbStateTiming] - l.statisticsOld[PbStateTiming]
	blockZkIncIntermediateHashesTiming := l.statistics[ZkIncIntermediateHashesTiming] - l.statisticsOld[ZkIncIntermediateHashesTiming]
	blockFinaliseBlockWriteTiming := l.statistics[FinaliseBlockWriteTiming] - l.statisticsOld[FinaliseBlockWriteTiming]

	blockZKHashAccountCount := l.statistics[ZKHashAccountCount] - l.statisticsOld[ZKHashAccountCount]
	blockZKHashStoreCount := l.statistics[ZKHashStoreCount] - l.statisticsOld[ZKHashStoreCount]
	blockZKHashCodeCount := l.statistics[ZKHashCodeCount] - l.statisticsOld[ZKHashCodeCount]

	blockZKHashSMTDeleteByNodeKey := l.statistics[ZKHashSMTDeleteByNodeKey] - l.statisticsOld[ZKHashSMTDeleteByNodeKey]
	blockZKHashSMTDeleteByNodeKeyTiming := l.statistics[ZKHashSMTDeleteByNodeKeyTiming] - l.statisticsOld[ZKHashSMTDeleteByNodeKeyTiming]
	blockZKHashSMTDeleteHashKey := l.statistics[ZKHashSMTDeleteHashKey] - l.statisticsOld[ZKHashSMTDeleteHashKey]
	blockZKHashSMTDeleteHashKeyTiming := l.statistics[ZKHashSMTDeleteHashKeyTiming] - l.statisticsOld[ZKHashSMTDeleteHashKeyTiming]
	blockZKHashSMTInsertKey := l.statistics[ZKHashSMTInsertKey] - l.statisticsOld[ZKHashSMTInsertKey]
	blockZKHashSMTInsertKeyTiming := l.statistics[ZKHashSMTInsertKeyTiming] - l.statisticsOld[ZKHashSMTInsertKeyTiming]
	blockZKHashSMTGetKey := l.statistics[ZKHashSMTGetKey] - l.statisticsOld[ZKHashSMTGetKey]
	blockZKHashSMTGetKeyTiming := l.statistics[ZKHashSMTGetKeyTiming] - l.statisticsOld[ZKHashSMTGetKeyTiming]

	blockHermezSmtMetadata := l.statistics[HermezSmtMetadata] - l.statisticsOld[HermezSmtMetadata]
	blockHermezSmtMetadataTiming := l.statistics[HermezSmtMetadataTiming] - l.statisticsOld[HermezSmtMetadataTiming]
	blockHermezSmtStats := l.statistics[HermezSmtStats] - l.statisticsOld[HermezSmtStats]
	blockHermezSmtStatsTiming := l.statistics[HermezSmtStatsTiming] - l.statisticsOld[HermezSmtStatsTiming]
	blockHermezSmt := l.statistics[HermezSmt] - l.statisticsOld[HermezSmt]
	blockHermezSmtTiming := l.statistics[HermezSmtTiming] - l.statisticsOld[HermezSmtTiming]
	blockHermezSmtHashKey := l.statistics[HermezSmtHashKey] - l.statisticsOld[HermezSmtHashKey]
	blockHermezSmtHashKeyTiming := l.statistics[HermezSmtHashKeyTiming] - l.statisticsOld[HermezSmtHashKeyTiming]

	blockDelete := l.statistics[Delete] - l.statisticsOld[Delete]
	blockAppend := l.statistics[Append] - l.statisticsOld[Append]
	blockPut := l.statistics[Put] - l.statisticsOld[Put]

	setSmtCacheTiming := l.statistics[SetSmtCacheTiming] - l.statisticsOld[SetSmtCacheTiming]
	l.mu.RUnlock()

	txProcessDetails := fmt.Sprintf("{ getTx[%dms], getTxPause[%dms] }",
		blockGetTxTiming, blockGetTxPauseTiming)

	zkHashSMTTimings := fmt.Sprintf("{ zkHashSMTDeleteByNodeKey[%d-%.3fms], zkHashSMTDeleteHashKey[%d-%.3fms], "+
		"zkHashSMTInsertKey[%d-%.3fms], zkHashSMTGetKey[%d-%.3fms] }",
		blockZKHashSMTDeleteByNodeKey, float64(blockZKHashSMTDeleteByNodeKeyTiming)/1000.0,
		blockZKHashSMTDeleteHashKey, float64(blockZKHashSMTDeleteHashKeyTiming)/1000.0,
		blockZKHashSMTInsertKey, float64(blockZKHashSMTInsertKeyTiming)/1000.0,
		blockZKHashSMTGetKey, float64(blockZKHashSMTGetKeyTiming)/1000.0)

	hermezTimings := fmt.Sprintf("{ hermezSmtMetadata[%d-%dms], hermezSmtStats[%d-%dms], "+
		"hermezSmt[%d-%dms], hermezSmtHashKey[%d-%dms], [delete:%.3fms, append:%.3fms, put:%.3fms] }",
		blockHermezSmtMetadata, blockHermezSmtMetadataTiming,
		blockHermezSmtStats, blockHermezSmtStatsTiming,
		blockHermezSmt, blockHermezSmtTiming,
		blockHermezSmtHashKey, blockHermezSmtHashKeyTiming,
		float64(blockDelete)/1000.0, float64(blockAppend)/1000.0, float64(blockPut)/1000.0)

	zkHashingDetails := fmt.Sprintf("{ zkHashSMT %s, hermezSMT %s }", zkHashSMTTimings, hermezTimings)

	result := fmt.Sprintf("Block<%s>, Txs<%d>, TotalDuration-block<%dms> { "+
		"ProcessTxTiming<%dms> %s, "+
		"PbStateTiming<%dms>, "+
		"ZkIncIntermediateHashesTiming<%dms> %s, "+
		"FinaliseBlockWriteTiming<%dms>, "+
		"BatchCommitDBTiming<%dms> "+
		"}, "+
		"GasUsed<%d>, GetTxPause<%d>, "+
		"GasOverTx<%d>, ZKOverflowBlock<%t>, InvalidTx<%d>, "+
		"zkHashAccCount<account:%d, storage:%d, code:%d> "+
		"SetSmtCacheTiming<%dms>",
		block, blockTx, blockDuration,
		blockProcessTxTiming, txProcessDetails,
		blockPbStateTiming,
		blockZkIncIntermediateHashesTiming, zkHashingDetails,
		blockFinaliseBlockWriteTiming,
		blockBatchCommitDBTiming,
		blockGasUsed, blockGetTxPause,
		blockGasOverTx, blockZkOverflowBlock, blockInvalidTx,
		blockZKHashAccountCount, blockZKHashStoreCount, blockZKHashCodeCount, setSmtCacheTiming)

	log.Info(result)
	l.mu.Lock()
	for k, v := range l.statistics {
		l.statisticsOld[k] = v
	}
	l.newBlockTime = time.Now()
	l.mu.Unlock()

	return result
}

func (l *statisticsInstance) Summary() string {
	l.mu.RLock()
	batch := l.tags[FinalizeBatchNumber]
	batchDuration := time.Since(l.newRoundTime).Milliseconds()
	gasUsed := l.statistics[BatchGas]
	blockCount := l.statistics[BlockCounter]
	tx := l.statistics[TxCounter]
	getTxPause := l.statistics[GetTxPauseCounter]
	gasOverTx := l.statistics[FailTxGasOverCounter]
	zkOverflowBlock := l.statistics[ZKOverflowBlockCounter]
	invalidTx := l.statistics[ProcessingInvalidTxCounter]
	sequencingBatchTiming := l.statistics[SequencingBatchTiming]
	getTxTiming := l.statistics[GetTxTiming]
	getTxPauseTiming := l.statistics[GetTxPauseTiming]
	processTxTiming := l.statistics[ProcessingTxTiming]
	batchCommitDBTiming := l.statistics[BatchCommitDBTiming]
	pbStateTiming := l.statistics[PbStateTiming]
	zkIncIntermediateHashesTiming := l.statistics[ZkIncIntermediateHashesTiming]
	finaliseBlockWriteTiming := l.statistics[FinaliseBlockWriteTiming]

	zkHashAccountCount := l.statistics[ZKHashAccountCount]
	zkHashStoreCount := l.statistics[ZKHashStoreCount]
	zkHashCodeCount := l.statistics[ZKHashCodeCount]

	zkHashSMTDeleteByNodeKey := l.statistics[ZKHashSMTDeleteByNodeKey]
	zkHashSMTDeleteByNodeKeyTiming := l.statistics[ZKHashSMTDeleteByNodeKeyTiming]
	zkHashSMTDeleteHashKey := l.statistics[ZKHashSMTDeleteHashKey]
	zkHashSMTDeleteHashKeyTiming := l.statistics[ZKHashSMTDeleteHashKeyTiming]
	zkHashSMTInsertKey := l.statistics[ZKHashSMTInsertKey]
	zkHashSMTInsertKeyTiming := l.statistics[ZKHashSMTInsertKeyTiming]
	zkHashSMTGetKey := l.statistics[ZKHashSMTGetKey]
	zkHashSMTGetKeyTiming := l.statistics[ZKHashSMTGetKeyTiming]

	hermezSmtMetadata := l.statistics[HermezSmtMetadata]
	hermezSmtMetadataTiming := l.statistics[HermezSmtMetadataTiming]
	hermezSmtStats := l.statistics[HermezSmtStats]
	hermezSmtStatsTiming := l.statistics[HermezSmtStatsTiming]
	hermezSmt := l.statistics[HermezSmt]
	hermezSmtTiming := l.statistics[HermezSmtTiming]
	hermezSmtHashKey := l.statistics[HermezSmtHashKey]
	hermezSmtHashKeyTiming := l.statistics[HermezSmtHashKeyTiming]

	deleteTime := l.statistics[Delete]
	appendTime := l.statistics[Append]
	putTime := l.statistics[Put]

	flushSmtCacheWait := l.statistics[FlushSmtCacheWait]
	setSmtCacheTiming := l.statistics[SetSmtCacheTiming]
	l.mu.RUnlock()

	txProcessDetails := fmt.Sprintf("{ getTx[%dms], getTxPause[%dms] }", getTxTiming, getTxPauseTiming)

	zkHashSMTTimings := fmt.Sprintf("{ zkHashSMTDeleteByNodeKey[%d-%.3fms], zkHashSMTDeleteHashKey[%d-%.3fms], zkHashSMTInsertKey[%d-%.3fms], zkHashSMTGetKey[%d-%.3fms] }",
		zkHashSMTDeleteByNodeKey, float64(zkHashSMTDeleteByNodeKeyTiming)/1000.0,
		zkHashSMTDeleteHashKey, float64(zkHashSMTDeleteHashKeyTiming)/1000.0,
		zkHashSMTInsertKey, float64(zkHashSMTInsertKeyTiming)/1000.0,
		zkHashSMTGetKey, float64(zkHashSMTGetKeyTiming)/1000.0)

	hermezTimings := fmt.Sprintf("{ hermezSmtMetadata[%d-%dms], hermezSmtStats[%d-%dms], "+
		"hermezSmt[%d-%dms], hermezSmtHashKey[%d-%dms], [delete:%.3fms, append:%.3fms, put:%.3fms] }",
		hermezSmtMetadata, hermezSmtMetadataTiming,
		hermezSmtStats, hermezSmtStatsTiming,
		hermezSmt, hermezSmtTiming,
		hermezSmtHashKey, hermezSmtHashKeyTiming, float64(deleteTime)/1000.0, float64(appendTime)/1000.0, float64(putTime)/1000.0)

	zkHashingDetails := fmt.Sprintf("{ zkHashSMT %s, hermezSMT %s }", zkHashSMTTimings, hermezTimings)

	result := fmt.Sprintf("Batch<%s>, Blocks<%d>, Txs<%d>, TotalDuration-batch<%dms> { SequencingBatchTiming<%dms> { ProcessTxTiming<%dms> %s, PbStateTiming<%dms>, ZkIncIntermediateHashesTiming<%dms> %s, FinaliseBlockWriteTiming<%dms>, BatchCommitDBTiming<%dms> } }"+
		", GasUsed<%d>, GetTxPause<%d>, "+
		"GasOverTx<%d>, ZKOverflowBlock<%d>, InvalidTx<%d>, "+
		"zkHashAccCount<account:%d, storage:%d, code:%d>, "+
		"FlushSmtCacheWait<%dms>, SetSmtCacheTiming<%dms>",
		batch, blockCount, tx, batchDuration,
		sequencingBatchTiming,
		processTxTiming, txProcessDetails,
		pbStateTiming,
		zkIncIntermediateHashesTiming, zkHashingDetails,
		finaliseBlockWriteTiming,
		batchCommitDBTiming,
		gasUsed, getTxPause,
		gasOverTx, zkOverflowBlock, invalidTx,
		zkHashAccountCount, zkHashStoreCount, zkHashCodeCount, flushSmtCacheWait, setSmtCacheTiming)

	log.Info(result)

	// Report metrics to Prometheus
	// Batch level metrics
	SeqBatchDuration.Set(float64(batchDuration))
	SeqSequencingBatchTiming.Set(float64(sequencingBatchTiming))

	// Process transaction metrics
	SeqProcessTxTiming.Set(float64(processTxTiming))
	SeqGetTxTiming.Set(float64(getTxTiming))
	SeqGetTxPauseTiming.Set(float64(getTxPauseTiming))

	// State and finalization metrics
	SeqPbStateTiming.Set(float64(pbStateTiming))
	SeqZkIncIntermediateHashesTiming.Set(float64(zkIncIntermediateHashesTiming))
	SeqFinaliseBlockWriteTiming.Set(float64(finaliseBlockWriteTiming))
	SeqBatchCommitDBTiming.Set(float64(batchCommitDBTiming))

	l.resetStatistics()
	return result
}
