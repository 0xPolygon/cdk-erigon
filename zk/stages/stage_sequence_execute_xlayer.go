package stages

import (
	"context"
	"fmt"
	"time"

	"github.com/0xPolygonHermez/zkevm-data-streamer/datastreamer"
	dslog "github.com/0xPolygonHermez/zkevm-data-streamer/log"
	"github.com/ledgerwatch/erigon/core/rawdb"
	"github.com/ledgerwatch/erigon/eth/stagedsync"
	"github.com/ledgerwatch/erigon/zk/apollo"
	"github.com/ledgerwatch/erigon/zk/datastream/server"
	"github.com/ledgerwatch/log/v3"
)

type SMTAlignmentCheckState int

// For X Layer, for auto recovery
var shouldCheckForExecutionAndSMTAlignment = SMTAlignmentInit

// For X Layer, for local replay feature
var externalDataStreamServerCreated = false

const (
	// Initial state
	SMTAlignmentInit SMTAlignmentCheckState = iota
	// Pending resequence state
	SMTAlignmentPendingResequence
	// Terminated state
	SMTAlignmentTerminated
)

func tryToSleepSequencer(localDuration time.Duration, logPrefix string) {
	fullBatchSleepDuration := apollo.GetFullBatchSleepDuration(localDuration)
	if fullBatchSleepDuration > 0 {
		log.Info(fmt.Sprintf("[%s] Slow down sequencer: %v", logPrefix, fullBatchSleepDuration))
		time.Sleep(fullBatchSleepDuration)
	}
}

func createExternalDataStreamServer(cfg SequenceBlockCfg) (server.DataStreamServer, error) {
	// Use hardcoded timeout values & port & datastream file
	writeTimeout := 20 * time.Second
	inactivityTimeout := 10 * time.Minute
	inactivityCheckInterval := 5 * time.Minute
	port := uint16(16900)
	datastreamFile := "/home/data-stream"

	logConfig := &dslog.Config{
		Environment: "production",
		Level:       "warn",
		Outputs:     nil,
	}

	factory := server.NewZkEVMDataStreamServerFactory()

	streamServer, err := factory.CreateStreamServer(
		port,
		uint8(cfg.zk.DatastreamVersion),
		1,
		datastreamer.StreamType(1),
		datastreamFile,
		writeTimeout,
		inactivityTimeout,
		inactivityCheckInterval,
		logConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream server: %v", err)
	}

	fmt.Printf("Successfully created external data stream server with file: %s\n", datastreamFile)

	dataStreamServer := factory.CreateDataStreamServer(streamServer, cfg.zk.L2ChainId)

	return dataStreamServer, nil
}

func unwindExecutionToSMT(batchContext *BatchContext, lastExecutedBlock, targetBlock uint64, u stagedsync.Unwinder) (bool, error) {
	if lastExecutedBlock > targetBlock {
		block, err := rawdb.ReadBlockByNumber(batchContext.sdb.tx, targetBlock)
		if err != nil {
			return false, err
		}

		log.Warn(fmt.Sprintf("[%s] Unwinding due to SMT gap", batchContext.s.LogPrefix()), "smtHeight", targetBlock, "sequencerHeight", lastExecutedBlock)
		u.UnwindTo(targetBlock, stagedsync.BadBlock(block.Hash(), fmt.Errorf("received bad block")))
		return true, nil
	}

	return false, nil
}

func resequenceFromSMTAlignment(
	s *stagedsync.StageState,
	u stagedsync.Unwinder,
	ctx context.Context,
	cfg SequenceBlockCfg,
	historyCfg stagedsync.HistoryCfg,
	lastBatch, highestBatchInDs uint64,
) (err error) {
	log.Info(fmt.Sprintf("[%s] ResequenceFromSMTAlignment, last batch %d is lower than highest batch in datastream %d, resequencing...", s.LogPrefix(), lastBatch, highestBatchInDs))
	batches, err := cfg.dataStreamServer.ReadBatches(lastBatch+1, highestBatchInDs)
	if err != nil {
		return err
	}
	if err = cfg.dataStreamServer.UnwindToBatchStart(lastBatch + 1); err != nil {
		return err
	}

	log.Info(fmt.Sprintf("[%s] ResequenceFromSMTAlignment, from batch %d to %d in data stream", s.LogPrefix(), lastBatch+1, highestBatchInDs))
	for _, batch := range batches {
		batchJob := NewResequenceBatchJob(batch)
		subBatchCount := 0
		for batchJob.HasMoreBlockToProcess() {
			if err = sequencingBatchStep(s, u, ctx, cfg, historyCfg, batchJob); err != nil {
				return err
			}
			subBatchCount += 1
		}

		log.Info(fmt.Sprintf("[%s] ResequenceFromSMTAlignment, original batch %d with %d batches", s.LogPrefix(), batchJob.batchToProcess[0].BatchNumber, subBatchCount))
		if cfg.zk.SequencerResequenceStrict && subBatchCount != 1 {
			return fmt.Errorf("strict mode enabled, but resequenced batch %d has %d sub-batches", batchJob.batchToProcess[0].BatchNumber, subBatchCount)
		}
	}
	shouldCheckForExecutionAndSMTAlignment = SMTAlignmentTerminated
	return nil
}

func getTargetBlockForSMTAlignment(sdb *stageDb, logPrefix string, executionAt uint64, smtMaxBlockNumber uint64) (targetBlock uint64, err error) {
	smtBatchNo, err := sdb.hermezDb.GetBatchNoByL2Block(smtMaxBlockNumber)
	if err != nil || smtBatchNo == 0 {
		log.Error(fmt.Sprintf("[%s] Failed to get smt max block number, or batchNo is 0", logPrefix), "error", err, "smtMaxBlockNumber", smtMaxBlockNumber, "batchNo", smtBatchNo)
		return 0, err
	}
	smtBatchNo = smtBatchNo - 1
	targetBlock, found, err := sdb.hermezDb.GetHighestBlockInBatch(smtBatchNo)
	if err != nil {
		log.Error(fmt.Sprintf("[%s] Failed to get highest block in batch", logPrefix), "error", err, "batchNo", smtBatchNo, "targetBlock", targetBlock)
		return 0, err
	}
	if !found {
		log.Warn(fmt.Sprintf("[%s] No blocks in SMT target batch, using execution position", logPrefix), "batchNo", smtBatchNo, "executionAt", executionAt)
	}
	log.Warn(fmt.Sprintf("[%s] Target block for SMT alignment", logPrefix), "targetBlock", targetBlock, "executionAt", executionAt, "smtMaxBlockNumber", smtMaxBlockNumber, "smtBatchNo", smtBatchNo)
	return targetBlock, nil
}
