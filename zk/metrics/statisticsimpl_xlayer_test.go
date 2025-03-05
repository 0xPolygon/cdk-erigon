package metrics

import (
	"testing"
	"time"
)

func TestStatisticsInstanceSummary(t *testing.T) {
	type fields struct {
		timestamp     time.Time
		statistics    map[LogTag]int64
		statisticsOld map[LogTag]int64
		tags          map[LogTag]string
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{"1", fields{
			timestamp: time.Now().Add(-time.Second),
			statistics: map[LogTag]int64{
				BatchGas:                      111111,
				TxCounter:                     10,
				GetTxTiming:                   time.Second.Milliseconds(),
				GetTxPauseCounter:             2,
				GetTxPauseTiming:              time.Second.Milliseconds() * 30,
				FailTxGasOverCounter:          1,
				ZKOverflowBlockCounter:        1,
				ProcessingInvalidTxCounter:    2,
				SequencingBatchTiming:         time.Second.Milliseconds() * 20,
				ProcessingTxTiming:            time.Second.Milliseconds() * 30,
				BatchCommitDBTiming:           time.Second.Milliseconds() * 10,
				PbStateTiming:                 time.Second.Milliseconds() * 20,
				ZkIncIntermediateHashesTiming: time.Second.Milliseconds() * 15,
				FinaliseBlockWriteTiming:      time.Second.Milliseconds() * 25,
				ZKHashAccountCount:            1,
				ZKHashStoreCount:              2,
				ZKHashCodeCount:               3,

				ZKHashSMTDeleteByNodeKey: 4,
				ZKHashSMTDeleteHashKey:   5,
				ZKHashSMTInsertKey:       6,
				ZKHashSMTGetKey:          7,

				ZKHashSMTDeleteByNodeKeyTiming: 4100,
				ZKHashSMTDeleteHashKeyTiming:   5100,
				ZKHashSMTInsertKeyTiming:       6100,
				ZKHashSMTGetKeyTiming:          7100,

				HermezSmtStats:          1,
				HermezSmtMetadata:       1,
				HermezSmt:               60,
				HermezSmtHashKey:        20,
				HermezSmtStatsTiming:    1100,
				HermezSmtMetadataTiming: 2100,
				HermezSmtTiming:         3200,
				HermezSmtHashKeyTiming:  4200,

				Delete: 5000,
				Append: 6000,
				Put:    7000,
			},
			statisticsOld: map[LogTag]int64{},
			tags:          map[LogTag]string{BatchCloseReason: "deadline", FinalizeBatchNumber: "123", FinalizeBlockNumber: "5"},
		}, "test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &statisticsInstance{
				newRoundTime:  tt.fields.timestamp,
				statistics:    tt.fields.statistics,
				statisticsOld: tt.fields.statisticsOld,
				tags:          tt.fields.tags,
			}
			t.Log(l.SummaryCheckpoint())
			t.Log(l.Summary())
		})
	}
}
