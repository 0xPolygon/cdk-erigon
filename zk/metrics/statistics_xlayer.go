package metrics

import (
	"time"
)

type LogTag string

const (
	BlockCounter                  LogTag = "BlockCounter"
	TxCounter                     LogTag = "TxCounter"
	GetTxTiming                   LogTag = "GetTxTiming"
	GetTxPauseCounter             LogTag = "GetTxPauseCounter"
	GetTxPauseTiming              LogTag = "GetTxPauseTiming"
	BatchCloseReason              LogTag = "BatchCloseReason"
	ZKOverflowBlockCounter        LogTag = "ZKOverflowBlockCounter"
	FailTxGasOverCounter          LogTag = "FailTxGasOverCounter"
	BatchGas                      LogTag = "BatchGas"
	SequencingBatchTiming         LogTag = "SequencingBatchTiming"
	ProcessingTxTiming            LogTag = "ProcessingTxTiming"
	ProcessingInvalidTxCounter    LogTag = "ProcessingInvalidTxCounter"
	FinalizeBatchNumber           LogTag = "FinalizeBatchNumber"
	BatchCommitDBTiming           LogTag = "BatchCommitDBTiming"
	PbStateTiming                 LogTag = "PbStateTiming"
	ZkIncIntermediateHashesTiming LogTag = "ZkIncIntermediateHashesTiming"
	FinaliseBlockWriteTiming      LogTag = "FinaliseBlockWriteTiming"

	ZKHashAccountCount LogTag = "ZKHashAccountCount"
	ZKHashStoreCount   LogTag = "ZKHashStoreCount"
	ZKHashCodeCount    LogTag = "ZKHashCodeCount"

	ZKHashSMTDeleteByNodeKey LogTag = "ZKHashSMTDeleteByNodeKey"
	ZKHashSMTDeleteHashKey   LogTag = "ZKHashSMTDeleteHashKey"
	ZKHashSMTInsertKey       LogTag = "ZKHashSMTInsertKey"
	ZKHashSMTGetKey          LogTag = "ZKHashSMTGetKey"

	ZKHashSMTDeleteByNodeKeyTiming LogTag = "ZKHashSMTDeleteByNodeKeyTiming"
	ZKHashSMTDeleteHashKeyTiming   LogTag = "ZKHashSMTDeleteHashKeyTiming"
	ZKHashSMTInsertKeyTiming       LogTag = "ZKHashSMTInsertKeyTiming"
	ZKHashSMTGetKeyTiming          LogTag = "ZKHashSMTGetKeyTiming"

	HermezSmtMetadata       LogTag = "HermezSmtMetadata"
	HermezSmtStats          LogTag = "HermezSmtStats"
	HermezSmt               LogTag = "HermezSmt"
	HermezSmtHashKey        LogTag = "HermezSmtHashKey"
	HermezSmtMetadataTiming LogTag = "HermezSmtMetadataTiming"
	HermezSmtStatsTiming    LogTag = "HermezSmtStatsTiming"
	HermezSmtTiming         LogTag = "HermezSmtTiming"
	HermezSmtHashKeyTiming  LogTag = "HermezSmtHashKeyTiming"

	Delete LogTag = "Delete"
	Append LogTag = "Insert"
	Put    LogTag = "Put"

	FinalizeBlockNumber LogTag = "FinalizeBlockNumber"

	FlushSmtCacheWait LogTag = "FlushSmtCacheWait"
	SetSmtCacheTiming LogTag = "SetSmtCacheTiming"
)

type Statistics interface {
	CumulativeCounting(tag LogTag)
	CumulativeValue(tag LogTag, value int64)
	CumulativeTiming(tag LogTag, duration time.Duration)
	CumulativeMicroTiming(tag LogTag, duration time.Duration)
	SetTag(tag LogTag, value string)
	GetTag(tag LogTag) string
	GetStatistics(tag LogTag) int64
	Summary() string
	SummaryCheckpoint() string
}
