package stages

import (
	"context"
	"errors"
	"fmt"
	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/log/v3"
	"sync"
	"sync/atomic"
	"time"

	"math/big"

	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/metrics"
	"github.com/erigontech/erigon/core/rawdb"
	ethTypes "github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/eth/ethconfig"
	"github.com/erigontech/erigon/eth/stagedsync"
	"github.com/erigontech/erigon/eth/stagedsync/stages"
	"github.com/erigontech/erigon/zk/contracts"
	"github.com/erigontech/erigon/zk/hermez_db"
	"github.com/erigontech/erigon/zk/sequencer"
	"github.com/erigontech/erigon/zk/types"
)

type IL1Syncer interface {
	// atomic
	IsSyncStarted() bool
	IsDownloading() bool
	GetLastCheckedL1Block() uint64

	// Channels
	//GetLogsChan() chan []ethTypes.Log
	// GetProgressMessageChan() chan string

	L1QueryHeaders(logs []ethTypes.Log) (map[uint64]*ethTypes.Header, error)
	GetBlock(number uint64) (*ethTypes.Block, error)
	GetHeader(number uint64) (*ethTypes.Header, error)
	RunQueryBlocks(logPrefix string, lastCheckedBlock uint64, logsCh chan<- []ethTypes.Log, errCh chan<- error)
	RunQueryBlocksOnce(logPrefix string, lastCheckedBlock uint64, logsCh chan<- []ethTypes.Log, errCh chan<- error)
	StopSyncer()
	CheckL1BlockFinalized(blockNo uint64) (bool, uint64, error)
}

var (
	ErrStateRootMismatch      = errors.New("state root mismatch")
	lastCheckedL1BlockCounter = metrics.GetOrCreateGauge(`last_checked_l1_block`)
)

type L1SyncerCfg struct {
	db     kv.RwDB
	syncer IL1Syncer

	zkCfg *ethconfig.Zk
}

func StageL1SyncerCfg(db kv.RwDB, syncer IL1Syncer, zkCfg *ethconfig.Zk) L1SyncerCfg {
	return L1SyncerCfg{
		db:     db,
		syncer: syncer,
		zkCfg:  zkCfg,
	}
}

type BatchLogType byte

const (
	logUnknown          BatchLogType = 0
	logSequence         BatchLogType = 1
	logSequenceEtrog    BatchLogType = 2
	logVerify           BatchLogType = 3
	logVerifyEtrog      BatchLogType = 4
	logL1InfoTreeUpdate BatchLogType = 5
	logRollbackBatches  BatchLogType = 6

	logIncompatible BatchLogType = 100
)

type logsVerificationResult struct {
	muInfo                      sync.Mutex
	newVerificationsCount       atomic.Uint64
	newSequencesCount           atomic.Uint64
	highestWrittenL1BlockNumber atomic.Uint64
	highestVerification         *types.L1BatchInfo
	errChan                     chan error
}

func SpawnStageL1Syncer(
	s *stagedsync.StageState,
	u stagedsync.Unwinder,
	ctx context.Context,
	tx kv.RwTx,
	cfg L1SyncerCfg,
	quiet bool,
) error {
	start := time.Now()
	///// DEBUG BISECT /////
	if cfg.zkCfg.DebugLimit > 0 {
		return nil
	}
	///// DEBUG BISECT /////

	logPrefix := s.LogPrefix()
	log.Info(fmt.Sprintf("[%s] Starting L1 sync stage", logPrefix))
	// if sequencer.IsSequencer() {
	// 	log.Info(fmt.Sprintf("[%s] skipping -- sequencer", logPrefix))
	// 	return nil
	// }
	defer log.Info(fmt.Sprintf("[%s] Finished L1 sync stage ", logPrefix))

	var internalTxOpened bool
	if tx == nil {
		internalTxOpened = true
		log.Debug("l1 sync: no tx provided, creating a new one")
		var err error
		tx, err = cfg.db.BeginRw(ctx)
		if err != nil {
			return fmt.Errorf("cfg.db.BeginRw: %w", err)
		}
		defer tx.Rollback()
	}

	// pass tx to the hermezdb
	hermezDb := hermez_db.NewHermezDb(tx)

	// get l1 block progress from this stage's progress
	l1BlockProgress, err := stages.GetStageProgress(tx, stages.L1Syncer)
	if err != nil {
		return fmt.Errorf("GetStageProgress, %w", err)
	}

	// start syncer if not started
	if cfg.syncer.IsSyncStarted() {
		panic("L1 syncer should already started")
	}

	if l1BlockProgress == 0 {
		l1BlockProgress = cfg.zkCfg.L1FirstBlock - 1
	}

	// start the syncer
	// the buffered channel to prevent I/O blocking on the reader.
	logsCh := make(chan []ethTypes.Log, 10000)
	errCh := make(chan error)
	go cfg.syncer.RunQueryBlocksOnce(logPrefix, l1BlockProgress, logsCh, errCh)

	result, err := logsReader(logPrefix, cfg.zkCfg.L1RollupId, hermezDb, logsCh, errCh)
	if err != nil {
		return fmt.Errorf("logsReader: %w", err)
	}

	latestCheckedBlock := cfg.syncer.GetLastCheckedL1Block()

	lastCheckedL1BlockCounter.Set(float64(latestCheckedBlock))

	if result.HighestWrittenL1BlockNumber() > l1BlockProgress {
		log.Info(fmt.Sprintf(
			"[%s] Saving L1 syncer progress. latestCheckedBlock: %d, newVerificationsCount: %d, newSequencesCount: %d, highestWrittenL1BlockNo: %d",
			logPrefix,
			latestCheckedBlock,
			result.VerificationCount(),
			result.SequenceCount(),
			result.HighestWrittenL1BlockNumber(),
		),
		)

		if err = stages.SaveStageProgress(tx, stages.L1Syncer, result.HighestWrittenL1BlockNumber()); err != nil {
			return fmt.Errorf("SaveStageProgress: %w", err)
		}
		if result.HighestVerification().BatchNo > 0 {
			log.Info(fmt.Sprintf("[%s] highestVerificationBatchNo: %d", logPrefix, result.HighestVerification().BatchNo))
			if err = stages.SaveStageProgress(tx, stages.L1VerificationsBatchNo, result.HighestVerification().BatchNo); err != nil {
				return fmt.Errorf("SaveStageProgress: %w", err)
			}
		}

		// State Root Verifications Check
		if err = verifyAgainstLocalBlocks(tx, hermezDb, logPrefix); err != nil {
			if errors.Is(err, ErrStateRootMismatch) {
				panic(err)
			}
			// do nothing in hope the node will recover if it isn't a stateroot mismatch
		}
	} else {
		log.Info(fmt.Sprintf("[%s] No new L1 blocks to sync", logPrefix))
	}

	if internalTxOpened {
		log.Debug("l1 sync: first cycle, committing tx")
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("tx.Commit: %w", err)
		}
	}

	elapsed := time.Since(start)
	log.Info(fmt.Sprintf("[%s] SpawnStageL1Syncer sync finished in %s\n", logPrefix, elapsed))

	return nil
}
func logsReader(logPrefix string, rollupId uint64, hermezDb *hermez_db.HermezDb, logsCh <-chan []ethTypes.Log, errCh <-chan error) (*logsVerificationResult, error) {
	defer log.Info(fmt.Sprintf("[%s] FINISHING logsReader ", logPrefix))

	result := newLogsVerificationResult()
	for {
		select {
		case logs, ok := <-logsCh:
			if !ok {
				log.Info(fmt.Sprintf("[%s] L1 syncer RunQueryBlocksOnce logs channel closed", logPrefix))
				return result, nil
			}
			err := processLogs(logs, rollupId, hermezDb, result)
			if err != nil {
				return nil, fmt.Errorf("processLogs: %w", err)
			}
		case errVal := <-errCh:
			if errVal != nil {
				log.Info(fmt.Sprintf("[%s] L1 syncer RunQueryBlocksOnce error: %s", logPrefix, errVal))
			}
		}
	}
}

// processLogs processes EVM log entries (not sequenced with previous, from multiple writers)
func processLogs(
	logEntries []ethTypes.Log,
	rollupId uint64,
	hermezDb *hermez_db.HermezDb,
	logsVerificationResult *logsVerificationResult,
) error {
	for logEntryIndex := range logEntries {
		log.Debug(fmt.Sprintf("L1 syncer processing log entry %d", logEntries[logEntryIndex].BlockNumber))
		// loopvar issue fixed in go1.22, will use direct slice iteration
		info, batchLogType := parseLogType(rollupId, &logEntries[logEntryIndex])
		switch batchLogType {
		case logSequence:
			fallthrough
		case logSequenceEtrog:
			// prevent storing pre-etrog sequences for etrog rollups
			if batchLogType == logSequence && rollupId > 1 {
				continue
			}
			// Does hemezDb supports checking sequence?
			if err := hermezDb.WriteSequence(
				info.L1BlockNo,
				info.BatchNo,
				info.L1TxHash,
				info.StateRoot,
				info.L1InfoRoot,
			); err != nil {
				return fmt.Errorf("WriteSequence: %w", err)
			}

			logsVerificationResult.UpdateHigherBlock(info.L1BlockNo)
			logsVerificationResult.SequenceCountInc()

		case logRollbackBatches:
			if err := hermezDb.RollbackSequences(info.BatchNo); err != nil {
				return fmt.Errorf("RollbackSequences: %w", err)
			}
			logsVerificationResult.UpdateHigherBlock(info.L1BlockNo)

		case logVerify:
			fallthrough
		case logVerifyEtrog:
			// prevent storing pre-etrog verifications for etrog rollups
			if batchLogType == logVerify && rollupId > 1 {
				continue
			}

			logsVerificationResult.UpdateHighestVerification(&info)

			if err := hermezDb.WriteVerification(info.L1BlockNo, info.BatchNo, info.L1TxHash, info.StateRoot); err != nil {
				return fmt.Errorf("WriteVerification for block %d: %w", info.L1BlockNo, err)
			}

			logsVerificationResult.UpdateHigherBlock(info.L1BlockNo)
			logsVerificationResult.VerificationCountInc()
		case logIncompatible:
			continue
		default:
			log.Warn("L1 Syncer unknown topic", "topic", logEntries[logEntryIndex].Topics[0])
		}
	}
	return nil
}

func newLogsVerificationResult() *logsVerificationResult {
	return &logsVerificationResult{
		highestVerification: &types.L1BatchInfo{},
		errChan:             make(chan error)}
}

// SequenceCountInc atomically increments the count of new sequences by 1.
func (l *logsVerificationResult) SequenceCountInc() {
	l.newSequencesCount.Add(1)
}

func (l *logsVerificationResult) SequenceCount() uint64 {
	return l.newSequencesCount.Load()
}

// VerificationCountInc atomically increments the count of new verifications by 1.
func (l *logsVerificationResult) VerificationCountInc() {
	l.newVerificationsCount.Add(1)
}

func (l *logsVerificationResult) VerificationCount() uint64 {
	return l.newVerificationsCount.Load()
}

// UpdateHigherBlock updates the highest written block number if blockNumber is higher than the stored one
// with atomic CAS operation strategy
func (l *logsVerificationResult) UpdateHigherBlock(blockNumber uint64) {
	for {
		currentHighestBlockNumber := l.highestWrittenL1BlockNumber.Load()

		if blockNumber <= currentHighestBlockNumber {
			break
		}
		if l.highestWrittenL1BlockNumber.CompareAndSwap(currentHighestBlockNumber, blockNumber) {
			break
		}
	}
}

// HighestWrittenL1BlockNumber returns the current highest L1 block number that has been written
func (l *logsVerificationResult) HighestWrittenL1BlockNumber() uint64 {
	return l.highestWrittenL1BlockNumber.Load()
}

// UpdateHighestVerification updates the highestVerification field if the provided batch info has a higher BatchNo.
func (l *logsVerificationResult) UpdateHighestVerification(info *types.L1BatchInfo) {
	l.muInfo.Lock()
	defer l.muInfo.Unlock()

	if info.BatchNo > l.highestVerification.BatchNo {
		l.highestVerification = info
	}
}

func (l *logsVerificationResult) HighestVerification() *types.L1BatchInfo {
	return l.highestVerification
}

func (l *logsVerificationResult) ErrChan() chan error {
	return l.errChan
}

func parseLogType(l1RollupId uint64, log *ethTypes.Log) (l1BatchInfo types.L1BatchInfo, batchLogType BatchLogType) {
	var (
		batchNum              uint64
		stateRoot, l1InfoRoot common.Hash
	)

	switch log.Topics[0] {
	case contracts.SequenceBatchesTopicPreEtrog:
		batchLogType = logSequence
		batchNum = new(big.Int).SetBytes(log.Topics[1].Bytes()).Uint64()
	case contracts.SequenceBatchesTopicEtrog:
		batchLogType = logSequenceEtrog
		batchNum = new(big.Int).SetBytes(log.Topics[1].Bytes()).Uint64()
		l1InfoRoot = common.BytesToHash(log.Data[:32])
	case contracts.VerificationTopicPreEtrog:
		batchLogType = logVerify
		batchNum = new(big.Int).SetBytes(log.Topics[1].Bytes()).Uint64()
		stateRoot = common.BytesToHash(log.Data[:32])
	case contracts.VerificationValidiumTopicEtrog:
		batchLogType = logVerifyEtrog
		batchNum = new(big.Int).SetBytes(log.Topics[1].Bytes()).Uint64()
		stateRoot = common.BytesToHash(log.Data[:32])
	case contracts.VerificationTopicEtrog:
		bigRollupId := new(big.Int).SetUint64(l1RollupId)
		isRollupIdMatching := log.Topics[1] == common.BigToHash(bigRollupId)
		if isRollupIdMatching {
			batchLogType = logVerifyEtrog
			batchNum = common.BytesToHash(log.Data[:32]).Big().Uint64()
			stateRoot = common.BytesToHash(log.Data[32:64])
		} else {
			batchLogType = logIncompatible
		}
	case contracts.UpdateL1InfoTreeTopic:
		batchLogType = logL1InfoTreeUpdate
	case contracts.RollbackBatchesTopic:
		batchLogType = logRollbackBatches
		batchNum = new(big.Int).SetBytes(log.Topics[1].Bytes()).Uint64()
	default:
		batchLogType = logUnknown
		batchNum = 0
	}

	return types.L1BatchInfo{
		BatchNo:    batchNum,
		L1BlockNo:  log.BlockNumber,
		L1TxHash:   common.BytesToHash(log.TxHash.Bytes()),
		StateRoot:  stateRoot,
		L1InfoRoot: l1InfoRoot,
	}, batchLogType
}

func UnwindL1SyncerStage(u *stagedsync.UnwindState, tx kv.RwTx, cfg L1SyncerCfg, ctx context.Context) (err error) {
	// we want to keep L1 data during an unwind, as we only sync finalised data there should be
	// no need to unwind here
	return nil
}

func PruneL1SyncerStage(s *stagedsync.PruneState, tx kv.RwTx, cfg L1SyncerCfg, ctx context.Context) (err error) {
	// no need to prune this data
	return nil
}

func verifyAgainstLocalBlocks(tx kv.RwTx, hermezDb *hermez_db.HermezDb, logPrefix string) (err error) {
	// get the highest hashed block
	hashedBlockNo, err := stages.GetStageProgress(tx, stages.IntermediateHashes)
	if err != nil {
		return fmt.Errorf("GetStageProgress: %w", err)
	}

	// no need to check - interhashes has not yet run
	if hashedBlockNo == 0 {
		return nil
	}

	// get the highest verified block
	verifiedBlockNo, err := hermezDb.GetHighestVerifiedBlockNo()
	if err != nil {
		return fmt.Errorf("GetHighestVerifiedBlockNo: %w", err)
	}

	// no verifications on l1
	if verifiedBlockNo == 0 {
		return nil
	}

	// 3 scenarios:
	//     1. verified and node both equal
	//     2. node behind l1 - verification block is higher than hashed block - use hashed block to find verification block
	//     3. l1 behind node - verification block is lower than hashed block - use verification block to find hashed block
	var blockToCheck uint64
	if verifiedBlockNo <= hashedBlockNo {
		blockToCheck = verifiedBlockNo
	} else {
		// in this case we need to find the blocknumber that is highest for the last batch
		// get the batch of the last hashed block
		hashedBatch, err := hermezDb.GetBatchNoByL2Block(hashedBlockNo)
		if err != nil && !errors.Is(err, hermez_db.ErrorNotStored) {
			return fmt.Errorf("GetBatchNoByL2Block: %w", err)
		}

		if hashedBatch == 0 {
			log.Warn(fmt.Sprintf("[%s] No batch number found for block %d", logPrefix, hashedBlockNo))
			return nil
		}

		// we don't know if this is the latest block in this batch, so check for the previous one
		// find the higher blocknum for previous batch
		blockNumbers, err := hermezDb.GetL2BlockNosByBatch(hashedBatch)
		if err != nil {
			return fmt.Errorf("GetL2BlockNosByBatch: %w", err)
		}

		if len(blockNumbers) == 0 {
			log.Warn(fmt.Sprintf("[%s] No block numbers found for batch %d", logPrefix, hashedBatch))
			return nil
		}

		for _, num := range blockNumbers {
			if num > blockToCheck {
				blockToCheck = num
			}
		}
	}

	// already checked
	highestChecked, err := stages.GetStageProgress(tx, stages.VerificationsStateRootCheck)
	if err != nil {
		return fmt.Errorf("GetStageProgress: %w", err)
	}
	if highestChecked >= blockToCheck {
		return nil
	}

	if !sequencer.IsSequencer() {
		if err = blockComparison(tx, hermezDb, blockToCheck, logPrefix); err == nil {
			log.Info(fmt.Sprintf("[%s] State root verified in block %d", logPrefix, blockToCheck))
			if err := stages.SaveStageProgress(tx, stages.VerificationsStateRootCheck, verifiedBlockNo); err != nil {
				return fmt.Errorf("SaveStageProgress: %w", err)
			}
		}
	}

	return err
}

func blockComparison(tx kv.RwTx, hermezDb *hermez_db.HermezDb, blockNo uint64, logPrefix string) error {
	v, err := hermezDb.GetVerificationByL2BlockNo(blockNo)
	if err != nil {
		return fmt.Errorf("GetVerificationByL2BlockNo: %w", err)
	}

	block, err := rawdb.ReadBlockByNumber(tx, blockNo)
	if err != nil {
		return fmt.Errorf("ReadBlockByNumber: %w", err)
	}

	if v == nil || block == nil {
		log.Info("block or verification is nil", "block", block, "verification", v)
		return nil
	}

	if v.StateRoot != block.Root() {
		log.Error(fmt.Sprintf("[%s] State root mismatch in block %d. Local=0x%x, L1 verification=0x%x", logPrefix, blockNo, block.Root(), v.StateRoot))
		return ErrStateRootMismatch
	}

	return nil
}
