package stages

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"encoding/binary"

	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/core/types"
	ethTypes "github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/eth/ethconfig"
	"github.com/erigontech/erigon/eth/stagedsync"
	"github.com/erigontech/erigon/eth/stagedsync/stages"
	"github.com/erigontech/erigon/zk/hermez_db"
	"github.com/erigontech/erigon/zk/l1_data"
	"github.com/erigontech/erigon/zk/syncer"
)

type SequencerL1BlockSyncCfg struct {
	db     kv.RwDB
	zkCfg  *ethconfig.Zk
	syncer *syncer.L1Syncer
}

func StageSequencerL1BlockSyncCfg(db kv.RwDB, zkCfg *ethconfig.Zk, syncer *syncer.L1Syncer) SequencerL1BlockSyncCfg {
	return SequencerL1BlockSyncCfg{
		db:     db,
		zkCfg:  zkCfg,
		syncer: syncer,
	}
}

// SpawnSequencerL1BlockSyncStage is a special mode of operation where a flag is passed to force the sequencer
// to rebuild the batches as they were sent to the L1 in the first place. Typically, this will be used after
// the unwind tool has rolled the DB back so that the network can be recovered if a bug is found
func SpawnSequencerL1BlockSyncStage(
	s *stagedsync.StageState,
	u stagedsync.Unwinder,
	ctx context.Context,
	tx kv.RwTx,
	cfg SequencerL1BlockSyncCfg,
	logger log.Logger,
) error {
	logPrefix := s.LogPrefix()
	log.Info(fmt.Sprintf("[%s] Starting L1 block sync stage", logPrefix))
	defer log.Info(fmt.Sprintf("[%s] Finished L1 block sync stage", logPrefix))

	if !cfg.zkCfg.IsL1Recovery() {
		log.Info(fmt.Sprintf("[%s] Skipping L1 block sync stage", logPrefix))
		return nil
	}

	var err error
	freshTx := false
	if tx == nil {
		freshTx = true
		tx, err = cfg.db.BeginRw(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
	}

	hermezDb := hermez_db.NewHermezDb(tx)

	// perform a quick check to see if we have fully recovered from the l1 and exit the node
	highestBatch, err := stages.GetStageProgress(tx, stages.HighestSeenBatchNumber)
	if err != nil {
		return err
	}
	highestKnownBatch, err := hermezDb.GetLastL1BatchData()
	if err != nil {
		return err
	}

	// check if the highest batch from the L1 is higher than the config value if it is set
	if cfg.zkCfg.L1SyncStopBatch > 0 {
		// stop completely if we have executed past the stop batch
		if highestKnownBatch > cfg.zkCfg.L1SyncStopBatch {
			log.Info("Stopping L1 sync stage based on configured stop batch", "config", cfg.zkCfg.L1SyncStopBatch, "highest-known", highestKnownBatch)
			time.Sleep(1 * time.Second)
			return nil
		}

		// if not we might have already started execution and just need to see if we have all the batches we care about in the db, and we can exit
		// early as well
		hasEverything, err := haveAllBatchesInDb(highestBatch, cfg, hermezDb)
		if err != nil {
			return err
		}
		if hasEverything {
			log.Info("Stopping L1 sync stage based on configured stop batch", "config", cfg.zkCfg.L1SyncStopBatch, "highest-known", highestKnownBatch)
			time.Sleep(1 * time.Second)
			return nil
		}
	}

	// check if execution has caught up to the tip of the chain
	if highestBatch > 0 && highestKnownBatch == highestBatch {
		log.Info("L1 block sync recovery has completed!", "batch", highestBatch)
		time.Sleep(5 * time.Second)
	}

	l1BlockHeight, err := stages.GetStageProgress(tx, stages.L1BlockSync)
	if err != nil {
		return err
	}
	if l1BlockHeight == 0 {
		l1BlockHeight = cfg.zkCfg.L1SyncStartBlock
	}

	// the buffered channel to prevent I/O blocking on the reader.
	logsCh := make(chan []ethTypes.Log, 10000)
	errCh := make(chan error)
	go cfg.syncer.RunQueryBlocksOnce(logPrefix, l1BlockHeight, logsCh, errCh)

	var latestBatch atomic.Uint64

	err = sequencerBlocksSyncLogsReader(
		logPrefix,
		cfg.syncer,
		cfg.zkCfg.DAUrl,
		cfg.zkCfg.L1SyncStopBatch,
		highestBatch,
		hermezDb,
		logsCh,
		errCh,
		&latestBatch,
	)

	if err != nil {
		return fmt.Errorf("sequencerBlocksSyncLogsReader: %w", err)
	}

	log.Info(fmt.Sprintf("[%s] Syncing L1 blocks. Latest batch: %d", logPrefix, latestBatch.Load()))

	lastCheckedBlock := cfg.syncer.GetLastCheckedL1Block()
	if lastCheckedBlock > l1BlockHeight {
		log.Info(fmt.Sprintf("[%s] Saving L1 block sync progress", logPrefix), "lastChecked", lastCheckedBlock)
		if err = stages.SaveStageProgress(tx, stages.L1BlockSync, lastCheckedBlock); err != nil {
			return err
		}
	}

	if freshTx {
		if err = tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

func sequencerBlocksSyncLogsReader(
	logPrefix string,
	syncer *syncer.L1Syncer,
	dAUrl string,
	l1SyncStopBatch uint64,
	highestBatch uint64,
	hermezDb *hermez_db.HermezDb,
	logsCh <-chan []ethTypes.Log, errCh <-chan error,
	latestBatch *atomic.Uint64,
) error {
	// for concurrency processSequencerBlocksSyncLogs calling, mutex required
	stopBlockMap := make(map[uint64]struct{})
	for {
		select {
		case logs, ok := <-logsCh:
			if !ok {
				log.Info(fmt.Sprintf("[%s] SpawnSequencerL1BlockSyncStage RunQueryBlocksOnce logs channel closed", logPrefix))
				return nil
			}
			err := processSequencerBlocksSyncLogs(logPrefix, syncer, dAUrl, l1SyncStopBatch, highestBatch, stopBlockMap, logs, hermezDb, latestBatch)
			if err != nil {
				return fmt.Errorf("processSequencerLogs: %w", err)
			}
		case errVal := <-errCh:
			if errVal != nil {
				log.Info(fmt.Sprintf("[%s] L1 syncer RunQueryBlocksOnce error: %s", logPrefix, errVal))
			}
		}
	}
}

func processSequencerBlocksSyncLogs(
	logPrefix string,
	syncer *syncer.L1Syncer,
	dAUrl string,
	l1SyncStopBatch uint64,
	highestBatch uint64,
	stopBlockMap map[uint64]struct{},
	logEntries []ethTypes.Log,
	hermezDb *hermez_db.HermezDb,
	latestBatch *atomic.Uint64,
) error {
	for logEntryIndex := range logEntries {
		// for some reason some endpoints seem to not have certain transactions available to
		// them even they are perfectly valid and other RPC nodes return them fine. So, leaning
		// on the internals of the syncer which will round-robin through available RPC nodes, we
		// can attempt a few times to get the transaction before giving up and returning an error
		var (
			transaction types.Transaction
			err         error
		)
		// TODO: Optimize with workers
		for attempt := 0; attempt < 50; attempt++ {
			transaction, _, err = syncer.GetTransaction(logEntries[logEntryIndex].TxHash)
			if err == nil {
				break
			} else {
				log.Warn(fmt.Sprintf("[%s] Error getting transaction, attempting again. Error: %s", logPrefix, err), "hash", logEntries[logEntryIndex].TxHash.String())
				time.Sleep(500 * time.Millisecond)
			}
		}
		if err != nil {
			return err
		}

		if transaction == nil {
			return errors.New("transaction is nil")
		}

		lastBatchSequenced := logEntries[logEntryIndex].Topics[1].Big().Uint64()
		latestBatch.Store(lastBatchSequenced)

		l1InfoRoot := logEntries[logEntryIndex].Data
		if len(l1InfoRoot) != 32 {
			log.Error(fmt.Sprintf("[%s] L1 info root is not 32 bytes", logPrefix), "tx-hash", logEntries[logEntryIndex].TxHash.String())
			return errors.New("l1 info root is not 32 bytes")
		}

		batches, coinbase, limitTimestamp, err := l1_data.DecodeL1BatchData(transaction.GetData(), dAUrl)
		if err != nil {
			return err
		}

		limitTimestampBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(limitTimestampBytes, limitTimestamp)

		// here we find the first batch number that was sequenced by working backwards
		// from the latest batch in the original event
		initBatch := lastBatchSequenced - uint64(len(batches)-1)

		log.Debug(fmt.Sprintf("[%s] Processing L1 sequence transaction", logPrefix),
			"hash", transaction.Hash().String(),
			"initBatch", initBatch,
			"batches", len(batches),
		)

		// iterate over the batches in reverse order to ensure that the batches are written in the correct order
		// this is important because the batches are written in reverse order
		for idx, batch := range batches {
			b := initBatch + uint64(idx)
			size := 20 + 32 + 8 + len(batch)
					if size > LIMIT_120_KB {
						log.Error(fmt.Sprintf("[%s] L1 batch data is too large", logPrefix), "size", size)
						err = fmt.Errorf("L1 batch data is too large: %d", size)
						return err
					}

					data := make([]byte, size)
			copy(data, coinbase.Bytes())
			copy(data[20:], l1InfoRoot)
			copy(data[52:], limitTimestampBytes)
			copy(data[60:], batch)

			if err = hermezDb.WriteL1BatchData(b, data); err != nil {
				return err
			}

			// check if we need to stop here based on config
			if l1SyncStopBatch > 0 {
				stopBlockMap[b] = struct{}{}
				if checkStopBlockMap(highestBatch, l1SyncStopBatch, stopBlockMap) {
					log.Info(fmt.Sprintf("[%s] Stopping L1 sync based on stop batch config", logPrefix))
					return nil
				}
			}
		}

	}
	return nil
}

func haveAllBatchesInDb(highestBatch uint64, cfg SequencerL1BlockSyncCfg, hermezDb *hermez_db.HermezDb) (bool, error) {
	hasEverything := true
	for i := highestBatch; i <= cfg.zkCfg.L1SyncStopBatch; i++ {
		data, err := hermezDb.GetL1BatchData(i)
		if err != nil {
			return false, err
		}
		if len(data) == 0 {
			hasEverything = false
			break
		}
	}
	return hasEverything, nil
}

// checks the stop block map for any gaps between the known lowest and target block height
func checkStopBlockMap(earliest, target uint64, stopBlockMap map[uint64]struct{}) bool {
	for i := earliest; i <= target; i++ {
		if _, ok := stopBlockMap[i]; !ok {
			return false
		}
	}
	return true
}

func UnwindSequencerL1BlockSyncStage(u *stagedsync.UnwindState, tx kv.RwTx, cfg SequencerL1BlockSyncCfg, ctx context.Context) (err error) {
	return nil
}

func PruneSequencerL1BlockSyncStage(s *stagedsync.PruneState, tx kv.RwTx, cfg SequencerL1BlockSyncCfg, ctx context.Context, logger log.Logger) error {
	return nil
}
