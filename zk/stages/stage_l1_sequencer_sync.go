package stages

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/log/v3"
	ethTypes "github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/eth/ethconfig"
	"github.com/erigontech/erigon/eth/stagedsync"
	"github.com/erigontech/erigon/eth/stagedsync/stages"
	"github.com/erigontech/erigon/zk/contracts"
	"github.com/erigontech/erigon/zk/hermez_db"
	"github.com/erigontech/erigon/zk/types"
)

const (
	injectedBatchLogTransactionStartByte = 128
	injectedBatchLastGerStartByte        = 32
	injectedBatchLastGerEndByte          = 64
	injectedBatchSequencerStartByte      = 76
	injectedBatchSequencerEndByte        = 96
)

type L1SequencerSyncCfg struct {
	db     kv.RwDB
	zkCfg  *ethconfig.Zk
	syncer IL1Syncer
}

func StageL1SequencerSyncCfg(db kv.RwDB, zkCfg *ethconfig.Zk, sync IL1Syncer) L1SequencerSyncCfg {
	return L1SequencerSyncCfg{
		db:     db,
		zkCfg:  zkCfg,
		syncer: sync,
	}
}

func SpawnL1SequencerSyncStage(
	s *stagedsync.StageState,
	u stagedsync.Unwinder,
	tx kv.RwTx,
	cfg L1SequencerSyncCfg,
	ctx context.Context,
	logger log.Logger,
) (funcErr error) {
	logPrefix := s.LogPrefix()
	log.Info(fmt.Sprintf("[%s] Starting L1 Sequencer sync stage", logPrefix))
	defer log.Info(fmt.Sprintf("[%s] Finished L1 Sequencer sync stage", logPrefix))

	freshTx := tx == nil
	if freshTx {
		var err error
		tx, err = cfg.db.BeginRw(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback()
	}

	progress, err := stages.GetStageProgress(tx, stages.L1SequencerSync)
	if err != nil {
		return err
	}
	if progress == 0 {
		progress = cfg.zkCfg.L1FirstBlock - 1
	}

	// if the flag is set - wait for that block to be finalized on L1 before continuing
	if progress <= cfg.zkCfg.L1FinalizedBlockRequirement && cfg.zkCfg.L1FinalizedBlockRequirement > 0 {
		for {
			finalized, finalizedBn, err := cfg.syncer.CheckL1BlockFinalized(cfg.zkCfg.L1FinalizedBlockRequirement)
			if err != nil {
				// we shouldn't just throw the error, because it could be a timeout, or "too many requests" error and we could jsut retry
				log.Error(fmt.Sprintf("[%s] Error checking if L1 block %v is finalized: %v", logPrefix, cfg.zkCfg.L1FinalizedBlockRequirement, err))
			}

			if finalized {
				break
			}
			log.Info(fmt.Sprintf("[%s] Waiting for L1 block %v to be correctly checked for \"finalized\" before continuing. Current finalized is %d", logPrefix, cfg.zkCfg.L1FinalizedBlockRequirement, finalizedBn))
			time.Sleep(1 * time.Minute) // sleep could be even bigger since finalization takes more than 10 minutes
		}
	}

	hermezDb := hermez_db.NewHermezDb(tx)

	// start the syncer
	// the buffered channel to prevent I/O blocking on the reader.
	logsCh := make(chan []ethTypes.Log, 1000)
	errCh := make(chan error)
	go cfg.syncer.RunQueryBlocksOnce(logPrefix, progress, logsCh, errCh)

	err = sequencerLogsReader(logPrefix, cfg.syncer, cfg.zkCfg.L1RollupId, hermezDb, logsCh, errCh)

	if err != nil {
		return fmt.Errorf("sequencerLogsReader: %w", err)
	}

	progress = cfg.syncer.GetLastCheckedL1Block()
	if progress >= cfg.zkCfg.L1FirstBlock {
		// do not save progress if progress less than L1FirstBlock
		if funcErr = stages.SaveStageProgress(tx, stages.L1SequencerSync, progress); funcErr != nil {
			return funcErr
		}
	}

	log.Info(fmt.Sprintf("[%s] L1 Sequencer sync finished", logPrefix))

	if freshTx {
		if funcErr = tx.Commit(); funcErr != nil {
			return funcErr
		}
	}

	return nil
}
func sequencerLogsReader(
	logPrefix string,
	syncer IL1Syncer,
	rollupId uint64,
	hermezDb *hermez_db.HermezDb,
	logsCh <-chan []ethTypes.Log, errCh <-chan error,
) error {

	for {
		select {
		case logs, ok := <-logsCh:
			if !ok {
				log.Info(fmt.Sprintf("[%s] SpawnL1SequencerSyncStage RunQueryBlocksOnce logs channel closed", logPrefix))
				return nil
			}
			// optimize slow operation
			headersMap, err := syncer.L1QueryHeaders(logs)
			if err != nil {
				return err
			}

			err = processSequencerLogs(logs, rollupId, hermezDb, headersMap)
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

func processSequencerLogs(
	logEntries []ethTypes.Log,
	rollupId uint64,
	hermezDb *hermez_db.HermezDb,
	headersMap map[uint64]*ethTypes.Header,
) error {
	for logEntryIndex := range logEntries {

		switch logEntries[logEntryIndex].Topics[0] {
		case contracts.InitialSequenceBatchesTopic:
			// Called once, optimize
			header := headersMap[logEntries[logEntryIndex].BlockNumber]
			if err := HandleInitialSequenceBatches(hermezDb, logEntries[logEntryIndex], header); err != nil {
				return err
			}
		case contracts.AddNewRollupTypeTopic:
			fallthrough
		case contracts.AddNewRollupTypeTopicBanana:
			rollupType := logEntries[logEntryIndex].Topics[1].Big().Uint64()
			forkIdBytes := logEntries[logEntryIndex].Data[64:96] // 3rd positioned item in the log data
			forkId := new(big.Int).SetBytes(forkIdBytes).Uint64()
			if err := hermezDb.WriteRollupType(rollupType, forkId); err != nil {
				return err
			}
		case contracts.CreateNewRollupTopic:
			logRollupId := logEntries[logEntryIndex].Topics[1].Big().Uint64()
			if logRollupId != rollupId {
				continue
			}
			rollupTypeBytes := logEntries[logEntryIndex].Data[0:32]
			rollupType := new(big.Int).SetBytes(rollupTypeBytes).Uint64()
			fork, err := hermezDb.GetForkFromRollupType(rollupType)
			if err != nil {
				return err
			}
			if fork == 0 {
				log.Error("received CreateNewRollupTopic for unknown rollup type", "rollupType", rollupType)
			}
			if err = hermezDb.WriteNewForkHistory(fork, 0); err != nil {
				return err
			}
		case contracts.UpdateRollupTopic:
			logRollupId := logEntries[logEntryIndex].Topics[1].Big().Uint64()
			if logRollupId != rollupId {
				continue
			}
			newRollupBytes := logEntries[logEntryIndex].Data[0:32]
			newRollup := new(big.Int).SetBytes(newRollupBytes).Uint64()
			fork, err := hermezDb.GetForkFromRollupType(newRollup)
			if err != nil {
				return err
			}
			if fork == 0 {
				err = fmt.Errorf("received UpdateRollupTopic for unknown rollup type: %v", newRollup)
				return err
			}
			latestVerifiedBytes := logEntries[logEntryIndex].Data[32:64]
			latestVerified := new(big.Int).SetBytes(latestVerifiedBytes).Uint64()
			if err = hermezDb.WriteNewForkHistory(fork, latestVerified); err != nil {
				return err
			}
		default:
			log.Warn("received unexpected topic from l1 sequencer sync stage", "topic", logEntries[logEntryIndex].Topics[0])
		}
	}
	return nil
}

func HandleInitialSequenceBatches(
	db *hermez_db.HermezDb,
	l ethTypes.Log,
	header *ethTypes.Header,
) error {

	if header == nil {
		return fmt.Errorf("header is nil")
	}

	// the log appears to have some trailing some bytes of all 0s in it.  Not sure why but we can't handle the
	// TX without trimming these off
	injectedBatchLogTrailingBytes := getTrailingCutoffLen(l.Data)
	trailingCutoff := len(l.Data) - injectedBatchLogTrailingBytes
	log.Debug(fmt.Sprintf("Handle initial sequence batches, trail len:%v, log data: %v", injectedBatchLogTrailingBytes, l.Data))

	txData := l.Data[injectedBatchLogTransactionStartByte:trailingCutoff]

	ib := &types.L1InjectedBatch{
		L1BlockNumber:      l.BlockNumber,
		Timestamp:          header.Time,
		L1BlockHash:        header.Hash(),
		L1ParentHash:       header.ParentHash,
		LastGlobalExitRoot: common.BytesToHash(l.Data[injectedBatchLastGerStartByte:injectedBatchLastGerEndByte]),
		Sequencer:          common.BytesToAddress(l.Data[injectedBatchSequencerStartByte:injectedBatchSequencerEndByte]),
		Transaction:        txData,
	}

	return db.WriteL1InjectedBatch(ib)
}

func UnwindL1SequencerSyncStage(u *stagedsync.UnwindState, tx kv.RwTx, cfg L1SequencerSyncCfg, ctx context.Context) error {
	return nil
}

func PruneL1SequencerSyncStage(s *stagedsync.PruneState, tx kv.RwTx, cfg L1SequencerSyncCfg, ctx context.Context) error {
	return nil
}

func getTrailingCutoffLen(logData []byte) int {
	for i := len(logData) - 1; i >= 0; i-- {
		if logData[i] != 0 {
			return len(logData) - i - 1
		}
	}
	return 0
}
