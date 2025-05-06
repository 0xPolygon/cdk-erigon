package stages

import (
	"context"
	"errors"
	"fmt"
	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/l1infotree"
	"sync"
	"sync/atomic"
	"time"

	"math/big"

	libcommon "github.com/erigontech/erigon-lib/common"
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
	zkTypes "github.com/erigontech/erigon/zk/types"
)

const (
	logTypeUnknown      = 0
	logTypeVerification = 1
	logTypeSequence     = 2
	logTypeL1InfoTree   = 3

	logUnknown          BatchLogType = 0
	logSequence         BatchLogType = 1
	logSequenceEtrog    BatchLogType = 2
	logVerify           BatchLogType = 3
	logVerifyEtrog      BatchLogType = 4
	logL1InfoTreeUpdate BatchLogType = 5
	logRollbackBatches  BatchLogType = 6

	logIncompatible BatchLogType = 100

	injectedBatchLogTransactionStartByte = 128
	injectedBatchLastGerStartByte        = 32
	injectedBatchLastGerEndByte          = 64
	injectedBatchSequencerStartByte      = 76
	injectedBatchSequencerEndByte        = 96
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
	VerifyAddress(logEntry *ethTypes.Log) bool
	IsSequencer() bool
}

var (
	ErrStateRootMismatch      = errors.New("state root mismatch")
	lastCheckedL1BlockCounter = metrics.GetOrCreateGauge(`last_checked_l1_block`)
)

type L1CombinedSyncerCfg struct {
	db      kv.RwDB
	syncer  IL1Syncer
	updater *l1infotree.Updater

	zkCfg       *ethconfig.Zk
	isSequencer bool
}

func StageL1CombinedSyncerCfg(db kv.RwDB, syncer IL1Syncer, zkCfg *ethconfig.Zk, updater *l1infotree.Updater) L1CombinedSyncerCfg {
	return L1CombinedSyncerCfg{
		db:          db,
		syncer:      syncer,
		zkCfg:       zkCfg,
		updater:     updater,
		isSequencer: sequencer.IsSequencer(),
	}
}

func (l *L1CombinedSyncerCfg) IsSequencer() bool {
	return l.isSequencer
}

type BatchLogType byte

type logsVerificationResult struct {
	muInfo                      sync.Mutex
	newVerificationsCount       atomic.Uint64
	newSequencesCount           atomic.Uint64
	highestWrittenL1BlockNumber atomic.Uint64
	highestVerification         *types.L1BatchInfo
	l1InfoTreeLogs              []ethTypes.Log
	errChan                     chan error
}

func SpawnStageL1CombinedSyncer(
	s *stagedsync.StageState,
	u stagedsync.Unwinder,
	ctx context.Context,
	tx kv.RwTx,
	cfg L1CombinedSyncerCfg,
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

	freshTx := tx == nil
	if freshTx {
		var err error
		tx, err = cfg.db.BeginRw(ctx)
		if err != nil {
			return fmt.Errorf("cfg.db.BeginRw: %w", err)
		}
		defer tx.Rollback()
	}

	l1InfoTreeProgress, err := stages.GetStageProgress(tx, stages.L1InfoTree)
	if err != nil {
		return err
	}
	// L2InfoTreeUpdatesEnabled must be enabled, this method uses an updated rpc method that uses to and from.
	if l1InfoTreeProgress == 0 && !sequencer.IsSequencer() && cfg.zkCfg.L2InfoTreeUpdatesEnabled {
		select {
		default:
			// If we are a rpc node, and we are starting from the beginning, we need to check for updates from the L2
			infoTrees, err := cfg.updater.CheckL2RpcForInfoTreeUpdates(logPrefix, tx)
			if err != nil {
				log.Warn(fmt.Sprintf("[%s] L2 Info Tree sync failed, getting Info Tree from L1", logPrefix), "err", err)
				break
			}

			var latestIndex uint64
			latestUpdate := cfg.updater.GetLatestUpdate()
			if latestUpdate != nil {
				latestIndex = latestUpdate.Index
			}

			log.Info(fmt.Sprintf("[%s] Synced Info Tree updates from L2 Sequencer RPC", logPrefix), "count", len(infoTrees), "latestIndex", latestIndex)

			if freshTx {
				if err = tx.Commit(); err != nil {
					return fmt.Errorf("tx.Commit: %w", err)
				}
			}

			return nil
		}
	}

	// get l1 block progress from this stage's progress
	l1BlockProgress, err := stages.GetStageProgress(tx, stages.L1Syncer)
	if err != nil {
		return fmt.Errorf("GetStageProgress, %w", err)
	}

	if l1BlockProgress <= cfg.zkCfg.L1FinalizedBlockRequirement && cfg.zkCfg.L1FinalizedBlockRequirement > 0 {
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

	// pass tx to the hermezdb
	hermezDb := hermez_db.NewHermezDb(tx)

	// start syncer if not started
	if cfg.syncer.IsSyncStarted() {
		panic("L1 syncer should already started")
	}

	/*if err = cfg.updater.WarmUp(tx); err != nil {
		return fmt.Errorf("cfg.updater.WarmUp: %w", err)
	}*/

	if l1BlockProgress == 0 {
		l1BlockProgress = cfg.zkCfg.L1FirstBlock - 1
	}

	// start the syncer
	// the buffered channel to prevent I/O blocking on the reader.
	logsCh := make(chan []ethTypes.Log, 10000)
	errCh := make(chan error)
	go cfg.syncer.RunQueryBlocksOnce(logPrefix, l1BlockProgress, logsCh, errCh)

	result, err := logsReader(logPrefix, cfg.zkCfg.L1RollupId, hermezDb, cfg.syncer, logsCh, errCh)
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

		// For syncer only
		// TODO: Combine
		if cfg.IsSequencer() {
			if l1BlockProgress >= cfg.zkCfg.L1FirstBlock {
				// do not save progress if progress less than L1FirstBlock
				if err = stages.SaveStageProgress(tx, stages.L1SequencerSync, l1BlockProgress); err != nil {
					return err
				}
			}
		}

	} else {
		log.Info(fmt.Sprintf("[%s] No new L1 blocks to sync", logPrefix))
	}

	logsCount, err := cfg.updater.ProcessInfoTreeUpdates(logPrefix, tx, result.l1InfoTreeLogs)
	if err != nil {
		return fmt.Errorf("CheckForInfoTreeUpdates: %w", err)
	}

	var latestIndex uint64
	latestUpdate := cfg.updater.GetLatestUpdate()
	if latestUpdate != nil {
		latestIndex = latestUpdate.Index
	}

	log.Info(fmt.Sprintf("[%s] Info tree updates", logPrefix), "count", logsCount, "latestIndex", latestIndex)

	if freshTx {
		log.Debug("l1 sync: first cycle, committing tx")
		if err = tx.Commit(); err != nil {
			return fmt.Errorf("tx.Commit: %w", err)
		}
	}

	elapsed := time.Since(start)
	log.Info(fmt.Sprintf("[%s] SpawnStageL1Syncer sync finished in %s\n", logPrefix, elapsed))

	return nil
}
func logsReader(
	logPrefix string,
	rollupId uint64,
	hermezDb *hermez_db.HermezDb,
	syncer IL1Syncer,
	logsCh <-chan []ethTypes.Log,
	errCh <-chan error) (*logsVerificationResult, error) {
	result := newLogsVerificationResult()
	for {
		select {
		case logs, ok := <-logsCh:
			if !ok {
				log.Info(fmt.Sprintf("[%s] L1 syncer RunQueryBlocksOnce logs channel closed", logPrefix))
				return result, nil
			}
			err := processLogs(logPrefix, logs, rollupId, hermezDb, result, syncer)
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
	logPrefix string,
	logEntries []ethTypes.Log,
	rollupId uint64,
	hermezDb *hermez_db.HermezDb,
	logsVerificationResult *logsVerificationResult,
	syncer IL1Syncer,
) error {
	for logEntryIndex := range logEntries {
		if !syncer.VerifyAddress(&logEntries[logEntryIndex]) {
			log.Info(fmt.Sprintf("[%s][Security] Log address mismatch with defined addresses. L1 syncer skipping log entry %s.", logPrefix, logEntries[logEntryIndex].Address))
			continue
		}

		switch checkLogType(&logEntries[logEntryIndex]) {
		case logTypeVerification:
			if err := processVerificationLog(&logEntries[logEntryIndex], rollupId, hermezDb, logsVerificationResult); err != nil {
				return fmt.Errorf("processVerificationLog: %w", err)
			}
		case logTypeSequence:
			// optimize slow operation
			headersMap, err := syncer.L1QueryHeaders([]ethTypes.Log{logEntries[logEntryIndex]})
			if err != nil {
				return err
			}

			if err := processSequencerLog(&logEntries[logEntryIndex], rollupId, hermezDb, headersMap); err != nil {
				return fmt.Errorf("processSequencerLog: %w", err)
			}
		case logTypeL1InfoTree:
			logsVerificationResult.l1InfoTreeLogs = append(logsVerificationResult.l1InfoTreeLogs, logEntries[logEntryIndex])
		default:
			log.Warn("L1 Syncer unknown topic", "topic", logEntries[logEntryIndex].Topics[0])
		}
	}
	return nil
}

func processVerificationLog(
	logEntry *ethTypes.Log,
	rollupId uint64,
	hermezDb *hermez_db.HermezDb,
	logsVerificationResult *logsVerificationResult,
) error {
	log.Debug(fmt.Sprintf("L1 syncer processing log entry %d", logEntry.BlockNumber))

	info, batchLogType := parseLogType(rollupId, logEntry)
	switch batchLogType {
	case logSequence:
		fallthrough
	case logSequenceEtrog:
		// prevent storing pre-etrog sequences for etrog rollups
		if batchLogType == logSequence && rollupId > 1 {
			return nil
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
			return nil
		}

		logsVerificationResult.UpdateHighestVerification(&info)

		if err := hermezDb.WriteVerification(info.L1BlockNo, info.BatchNo, info.L1TxHash, info.StateRoot); err != nil {
			return fmt.Errorf("WriteVerification for block %d: %w", info.L1BlockNo, err)
		}

		logsVerificationResult.UpdateHigherBlock(info.L1BlockNo)
		logsVerificationResult.VerificationCountInc()
	case logIncompatible:
		log.Warn("L1 Syncer logIncompatible", "topic", logEntry.BlockNumber)
		return nil
	default:
		log.Warn("L1 Syncer unknown topic", "topic", logEntry.Topics[0])
	}
	return nil
}

func processSequencerLog(
	logEntry *ethTypes.Log,
	rollupId uint64,
	hermezDb *hermez_db.HermezDb,
	headersMap map[uint64]*ethTypes.Header,
) error {
	switch logEntry.Topics[0] {
	case contracts.InitialSequenceBatchesTopic:
		// Called once, optimize
		fmt.Println("InitialSequenceBatchesTopic")
		header := headersMap[logEntry.BlockNumber]
		if err := HandleInitialSequenceBatches(hermezDb, logEntry, header); err != nil {
			return err
		}
	case contracts.AddNewRollupTypeTopic:
		fallthrough
	case contracts.AddNewRollupTypeTopicBanana:
		rollupType := logEntry.Topics[1].Big().Uint64()
		forkIdBytes := logEntry.Data[64:96] // 3rd positioned item in the log data
		forkId := new(big.Int).SetBytes(forkIdBytes).Uint64()
		if err := hermezDb.WriteRollupType(rollupType, forkId); err != nil {
			return err
		}
	case contracts.CreateNewRollupTopic:
		logRollupId := logEntry.Topics[1].Big().Uint64()
		if logRollupId != rollupId {
			return nil
		}
		rollupTypeBytes := logEntry.Data[0:32]
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
		logRollupId := logEntry.Topics[1].Big().Uint64()
		if logRollupId != rollupId {
			return nil
		}
		newRollupBytes := logEntry.Data[0:32]
		newRollup := new(big.Int).SetBytes(newRollupBytes).Uint64()
		fork, err := hermezDb.GetForkFromRollupType(newRollup)
		if err != nil {
			return err
		}
		if fork == 0 {
			err = fmt.Errorf("received UpdateRollupTopic for unknown rollup type: %v", newRollup)
			return err
		}
		latestVerifiedBytes := logEntry.Data[32:64]
		latestVerified := new(big.Int).SetBytes(latestVerifiedBytes).Uint64()
		if err = hermezDb.WriteNewForkHistory(fork, latestVerified); err != nil {
			return err
		}
	default:
		log.Warn("received unexpected topic from l1 sequencer sync stage", "topic", logEntry.Topics[0])
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

func checkLogType(log *ethTypes.Log) uint8 {
	switch log.Topics[0] {
	case contracts.SequenceBatchesTopicPreEtrog,
		contracts.SequenceBatchesTopicEtrog,
		contracts.VerificationTopicPreEtrog,
		contracts.VerificationValidiumTopicEtrog,
		contracts.VerificationTopicEtrog,
		contracts.RollbackBatchesTopic:
		return logTypeVerification
	case contracts.InitialSequenceBatchesTopic,
		contracts.AddNewRollupTypeTopic,
		contracts.AddNewRollupTypeTopicBanana,
		contracts.CreateNewRollupTopic,
		contracts.UpdateRollupTopic:
		return logTypeSequence
	case contracts.UpdateL1InfoTreeTopic:
		return logTypeL1InfoTree
	default:
		return logTypeUnknown
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
		stateRoot, l1InfoRoot libcommon.Hash
	)

	switch log.Topics[0] {
	case contracts.SequenceBatchesTopicPreEtrog:
		batchLogType = logSequence
		batchNum = new(big.Int).SetBytes(log.Topics[1].Bytes()).Uint64()
	case contracts.SequenceBatchesTopicEtrog:
		batchLogType = logSequenceEtrog
		batchNum = new(big.Int).SetBytes(log.Topics[1].Bytes()).Uint64()
		l1InfoRoot = libcommon.BytesToHash(log.Data[:32])
	case contracts.VerificationTopicPreEtrog:
		batchLogType = logVerify
		batchNum = new(big.Int).SetBytes(log.Topics[1].Bytes()).Uint64()
		stateRoot = libcommon.BytesToHash(log.Data[:32])
	case contracts.VerificationValidiumTopicEtrog:
		batchLogType = logVerifyEtrog
		batchNum = new(big.Int).SetBytes(log.Topics[1].Bytes()).Uint64()
		stateRoot = libcommon.BytesToHash(log.Data[:32])
	case contracts.VerificationTopicEtrog:
		bigRollupId := new(big.Int).SetUint64(l1RollupId)
		isRollupIdMatching := log.Topics[1] == libcommon.BigToHash(bigRollupId)
		if isRollupIdMatching {
			batchLogType = logVerifyEtrog
			batchNum = libcommon.BytesToHash(log.Data[:32]).Big().Uint64()
			stateRoot = libcommon.BytesToHash(log.Data[32:64])
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
		L1TxHash:   libcommon.BytesToHash(log.TxHash.Bytes()),
		StateRoot:  stateRoot,
		L1InfoRoot: l1InfoRoot,
	}, batchLogType
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

// Sequencer functions

func HandleInitialSequenceBatches(
	db *hermez_db.HermezDb,
	l *ethTypes.Log,
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
		LastGlobalExitRoot: libcommon.BytesToHash(l.Data[injectedBatchLastGerStartByte:injectedBatchLastGerEndByte]),
		Sequencer:          libcommon.BytesToAddress(l.Data[injectedBatchSequencerStartByte:injectedBatchSequencerEndByte]),
		Transaction:        txData,
	}

	return db.WriteL1InjectedBatch(ib)
}

func getTrailingCutoffLen(logData []byte) int {
	for i := len(logData) - 1; i >= 0; i-- {
		if logData[i] != 0 {
			return len(logData) - i - 1
		}
	}
	return 0
}

// L1Tree

type Syncer interface {
	IsSyncStarted() bool
	RunQueryBlocks(logPrefix string, lastCheckedBlock uint64, logsCh chan<- []ethTypes.Log, errCh chan<- error)
	//GetLogsChan() chan []types.Log
	// GetProgressMessageChan() chan string
	IsDownloading() bool
	GetHeader(blockNumber uint64) (*ethTypes.Header, error)
	L1QueryHeaders(logs []ethTypes.Log) (map[uint64]*ethTypes.Header, error)
	StopSyncer()
	QueryForRootLog(to uint64) (*ethTypes.Log, error)
}

type L2InfoReaderRpc interface {
	GetExitRootTable(endpoint string) ([]zkTypes.L1InfoTreeUpdate, error)
}

type L2Syncer interface {
	IsSyncStarted() bool
	IsSyncFinished() bool
	GetInfoTreeChan() chan []zkTypes.L1InfoTreeUpdate
	RunSyncInfoTree()
	ConsumeInfoTree()
}

type Updater struct {
	cfg          *ethconfig.Zk
	syncer       Syncer
	logsL1Ch     chan []ethTypes.Log
	errL1Ch      chan error
	progress     uint64
	latestUpdate *zkTypes.L1InfoTreeUpdate
	l2Syncer     L2Syncer
}

func UnwindL1CombinedSyncerStage(u *stagedsync.UnwindState, tx kv.RwTx, cfg L1CombinedSyncerCfg, ctx context.Context) (err error) {
	// we want to keep L1 data during an unwind, as we only sync finalized data there should be
	// no need to unwind here
	return nil
}

func PruneL1CombinedSyncerStage(s *stagedsync.PruneState, tx kv.RwTx, cfg L1CombinedSyncerCfg, ctx context.Context) (err error) {
	// no need to prune this data
	return nil
}
