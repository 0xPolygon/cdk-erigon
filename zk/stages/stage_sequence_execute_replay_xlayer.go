package stages

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"

	"github.com/ledgerwatch/erigon/eth/stagedsync"
	"github.com/ledgerwatch/erigon/zk/datastream/server"
	"github.com/ledgerwatch/erigon/zk/datastream/types"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/ledgerwatch/log/v3"
	"golang.org/x/sys/unix"
)

var (
	doneChan        = make(chan struct{})
	mu              = sync.Mutex{}
	sequencingBatch bool
)

func replay(
	s *stagedsync.StageState,
	u stagedsync.Unwinder,
	ctx context.Context,
	cfg SequenceBlockCfg,
	historyCfg stagedsync.HistoryCfg,
	lastBatch, highestBatchInDs uint64,
	externalDataStreamServer server.DataStreamServer,
) (err error) {

	haltBatch := uint64(0)
	if cfg.zk.XLayer.SequencerReplayHaltOnBatchNumber > 0 {
		haltBatch = cfg.zk.XLayer.SequencerReplayHaltOnBatchNumber
		if haltBatch <= lastBatch {
			panic(fmt.Sprintf("[%s] The zkevm.sequencer-replay-halt-on-batch-number is set lower than the last batch number.", s.LogPrefix()))
		} else if haltBatch > highestBatchInDs {
			panic(fmt.Sprintf("[%s] The zkevm.sequencer-replay-halt-on-batch-number is set higher than the highest batch in datastream.", s.LogPrefix()))
		}
	} else {
		haltBatch = highestBatchInDs
	}

	log.Info(fmt.Sprintf("[%s] Last batch %d is lower than highest batch in datastream %d, resequencing from batch %d to %d ...", s.LogPrefix(), lastBatch, highestBatchInDs, lastBatch+1, haltBatch))

	var batches [][]*types.FullL2Block
	if cfg.zk.XLayer.SequencerReplayExternalDatastream {
		batches, err = externalDataStreamServer.ReadBatchesWithConcurrency(lastBatch+1, haltBatch)
	} else {
		batches, err = cfg.dataStreamServer.ReadBatchesWithConcurrency(lastBatch+1, haltBatch)
	}
	if err != nil {
		return err
	}

	log.Info(fmt.Sprintf("[%s] Read %d batches from data stream", s.LogPrefix(), len(batches)))

	tx, err := cfg.db.BeginRw(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// delete L1InfoTreeIndexesProgress
	hermezDb := hermez_db.NewHermezDb(tx)

	// get the start block number from the first batch
	if len(batches) == 0 {
		return fmt.Errorf("no batches to process")
	}
	fromBlock := batches[0][0].L2BlockNumber

	latestBlockNumber, _, err := hermezDb.GetLatestBlockL1InfoTreeIndexProgress()
	if err != nil {
		return fmt.Errorf("get latest block l1 info tree index progress error: %v", err)
	}

	// prune l1infotree indexes to unwinding point
	if err = hermezDb.DeleteBlockL1InfoTreeIndexesProgress(fromBlock, latestBlockNumber); err != nil {
		return fmt.Errorf("truncate block l1 info tree index progress error: %v", err)
	}

	// commit
	if err = tx.Commit(); err != nil {
		return err
	}

	log.Info(fmt.Sprintf("[%s] Deleted L1InfoTreeIndexesProgress from block %d to %d", s.LogPrefix(), fromBlock, latestBlockNumber))

	localHighestBatchInDs, err := cfg.dataStreamServer.GetHighestBatchNumber()
	if err != nil {
		return err
	}
	if localHighestBatchInDs > lastBatch {
		if err = cfg.dataStreamServer.UnwindToBatchStart(lastBatch + 1); err != nil {
			return err
		}
	}

	// listen for signals
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, unix.SIGINT, unix.SIGTERM)
	defer signal.Stop(sigc)

	log.Info(fmt.Sprintf("[%s] Resequence from batch %d to %d in data stream", s.LogPrefix(), lastBatch+1, haltBatch))

	updateSequencingBatch(true)
	defer updateSequencingBatch(false)

	for _, batch := range batches {
		batchJob := NewResequenceBatchJob(batch)
		subBatchCount := 0
		for batchJob.HasMoreBlockToProcess() {
			if err = sequencingBatchStep(s, u, ctx, cfg, historyCfg, batchJob); err != nil {
				return err
			}
			subBatchCount += 1
		}

		log.Info(fmt.Sprintf("[%s] Resequenced original batch %d with %d batches", s.LogPrefix(), batchJob.batchToProcess[0].BatchNumber, subBatchCount))
		if cfg.zk.SequencerResequenceStrict && subBatchCount != 1 {
			return fmt.Errorf("strict mode enabled, but resequenced batch %d has %d sub-batches", batchJob.batchToProcess[0].BatchNumber, subBatchCount)
		}

		select {
		case <-sigc:
			log.Info(fmt.Sprintf("[%s] Had received signal, stopping resequencing", s.LogPrefix()))
			doneChan <- struct{}{}
			return nil
		default:
		}
	}
	log.Info(fmt.Sprintf("[%s] Replay completed.", s.LogPrefix()))
	os.Exit(0)
	return nil
}

func WaitResequenceBatchDone() (<-chan struct{}, bool) {
	mu.Lock()
	defer mu.Unlock()

	return doneChan, sequencingBatch
}

func updateSequencingBatch(value bool) {
	mu.Lock()
	sequencingBatch = value
	mu.Unlock()
}
