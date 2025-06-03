package stages

import (
	"fmt"
	"github.com/ledgerwatch/erigon/core/rawdb"
	"github.com/ledgerwatch/erigon/eth/stagedsync/stages"
	"github.com/ledgerwatch/erigon/zk/l1_data"
	"github.com/ledgerwatch/log/v3"
)

func prepareBatchNumber(sdb *stageDb, forkId, lastBatch uint64, isL1Recovery bool) (uint64, error) {
	if isL1Recovery {
		recoveredBatchData, err := l1_data.BreakDownL1DataByBatch(lastBatch, forkId, sdb.hermezDb.HermezDbReader)
		if err != nil {
			return 0, err
		}

		blockNumbersInBatchSoFar, err := sdb.hermezDb.GetL2BlockNosByBatch(lastBatch)
		if err != nil {
			return 0, err
		}

		if len(blockNumbersInBatchSoFar) < len(recoveredBatchData.DecodedData) { // check if there are more blocks to process
			isLastBatchBad, err := sdb.hermezDb.GetInvalidBatch(lastBatch)
			if err != nil {
				return 0, err
			}

			// if last batch is not bad then continue buildingin it, otherwise return lastBatch+1 (at the end of the function)
			if !isLastBatchBad {
				return lastBatch, nil
			}
		}
	}

	return lastBatch + 1, nil
}

func doCheckForBadBatch(batchContext *BatchContext, batchState *BatchState, thisBlock uint64) (bool, error) {
	infoTreeIndex, err := batchState.batchL1RecoveryData.getInfoTreeIndex(batchContext.sdb)
	if err != nil {
		return false, err
	}

	// now let's detect a bad batch and skip it if we have to
	currentBlock, err := rawdb.ReadBlockByNumber(batchContext.sdb.tx, thisBlock)
	if err != nil {
		return false, err
	}

	badBatch, err := checkForBadBatch(batchState.batchNumber, batchContext.sdb.hermezDb, currentBlock.Time(), infoTreeIndex, batchState.batchL1RecoveryData.recoveredBatchData.LimitTimestamp, batchState.batchL1RecoveryData.recoveredBatchData.DecodedData)
	if err != nil {
		return false, err
	}

	return badBatch, nil
}

func writeBadBatchDetails(batchContext *BatchContext, batchState *BatchState, blockNumber uint64) error {
	log.Info(fmt.Sprintf("[%s] Skipping bad batch %d...", batchContext.s.LogPrefix(), batchState.batchNumber))
	// store the fact that this batch was invalid during recovery - will be used for the stream later
	if err := batchContext.sdb.hermezDb.WriteInvalidBatch(batchState.batchNumber); err != nil {
		return err
	}
	if err := batchContext.sdb.hermezDb.WriteBatchCounters(blockNumber, []int{}); err != nil {
		return err
	}
	if err := stages.SaveStageProgress(batchContext.sdb.tx, stages.HighestSeenBatchNumber, batchState.batchNumber); err != nil {
		return err
	}
	if err := batchContext.sdb.hermezDb.WriteForkId(batchState.batchNumber, batchState.forkId); err != nil {
		return err
	}
	if err := batchContext.sdb.tx.Commit(); err != nil {
		return err
	}
	return nil
}
