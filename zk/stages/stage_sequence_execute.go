package stages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core"
	"github.com/ledgerwatch/erigon/core/rawdb"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/eth/stagedsync"
	"github.com/ledgerwatch/erigon/eth/stagedsync/stages"
	"github.com/ledgerwatch/erigon/zk"
	"github.com/ledgerwatch/erigon/zk/apollo"
	"github.com/ledgerwatch/erigon/zk/datastream/server"
	"github.com/ledgerwatch/erigon/zk/hermez_db"
	"github.com/ledgerwatch/erigon/zk/metrics"
	zktx "github.com/ledgerwatch/erigon/zk/tx"
	"github.com/ledgerwatch/erigon/zk/txpool"
	"github.com/ledgerwatch/erigon/zk/utils"
	"github.com/ledgerwatch/log/v3"
)

var shouldCheckForExecutionAndDataStreamAlignment = true

func SpawnSequencingStage(
	s *stagedsync.StageState,
	u stagedsync.Unwinder,
	ctx context.Context,
	cfg SequenceBlockCfg,
	historyCfg stagedsync.HistoryCfg,
	quiet bool,
) (err error) {
	roTx, err := cfg.db.BeginRo(ctx)
	if err != nil {
		return err
	}
	defer roTx.Rollback()

	lastBatch, err := stages.GetStageProgress(roTx, stages.HighestSeenBatchNumber)
	if err != nil {
		return err
	}

	highestBatchInDs, err := cfg.dataStreamServer.GetHighestBatchNumber()
	if err != nil {
		return err
	}

	// For X Layer, local replay feature
	if cfg.zk.XLayer.SequencerReplay {
		if cfg.zk.XLayer.SequencerReplayL1SyncOnly {
			log.Info(fmt.Sprintf("[%s] Stop here because the zkevm.sequencer-replay-l1-sync-only flag is set to true.", s.LogPrefix()))
			os.Exit(0)
		}
		var externalDataStreamServer server.DataStreamServer
		if cfg.zk.XLayer.SequencerReplayExternalDatastream && !externalDataStreamServerCreated {
			externalDataStreamServer, err = createExternalDataStreamServer(cfg)
			if err != nil {
				return err
			}
			externalDataStreamServerCreated = true
			highestBatchInDs, err = externalDataStreamServer.GetHighestBatchNumber()
		} else {
			highestBatchInDs, err = cfg.dataStreamServer.GetHighestBatchNumber()
		}
		if err != nil {
			return err
		}
		if lastBatch < highestBatchInDs {
			return replay(s, u, ctx, cfg, historyCfg, lastBatch, highestBatchInDs, externalDataStreamServer)
		}
	}

	// For X Layer, for auto recovery
	if lastBatch < highestBatchInDs && shouldCheckForExecutionAndSMTAlignment == SMTAlignmentPendingResequence {
		log.Warn(fmt.Sprintf("[%s] Start to resequence for SMT alignment, lastBatch:%v, highestBatchInDs:%v", s.LogPrefix(), lastBatch, highestBatchInDs))
		return resequenceFromSMTAlignment(s, u, ctx, cfg, historyCfg, lastBatch, highestBatchInDs)
	}

	if lastBatch < highestBatchInDs {
		return resequence(s, u, ctx, cfg, historyCfg, lastBatch, highestBatchInDs)
	}

	if cfg.zk.SequencerResequence {
		log.Info(fmt.Sprintf("[%s] Resequencing completed. Please restart sequencer without resequence flag.", s.LogPrefix()))
		time.Sleep(10 * time.Minute)
		return nil
	}

	// For X Layer, split db and ac
	startWaitTime := time.Now()
	if cfg.zk.XLayer.EnableAsyncCommit {
		s.FlushSmtCacheWait()
	}
	metrics.GetLogStatistics().CumulativeTiming(metrics.FlushSmtCacheWait, time.Since(startWaitTime))

	if err = sequencingBatchStep(s, u, ctx, cfg, historyCfg, nil); err == nil {
		// For X Layer, split db and ac
		if !cfg.zk.XLayer.EnableAsyncCommit {
			return err
		}

		s.FlushSmtCacheSignalInc()
		go func() {
			defer s.FlushSmtCacheDone()
			// enable split smt db
			_ = s.FlushSmtCache(cfg.zk.XLayer.StandaloneSMTDatabase, false)
		}()
	}

	return err
}

func sequencingBatchStep(
	s *stagedsync.StageState,
	u stagedsync.Unwinder,
	ctx context.Context,
	cfg SequenceBlockCfg,
	historyCfg stagedsync.HistoryCfg,
	resequenceBatchJob *ResequenceBatchJob,
) (err error) {
	startSequenceTime := time.Now()
	logPrefix := s.LogPrefix()
	log.Info(fmt.Sprintf("[%s] Starting sequencing stage", logPrefix))
	defer func() {
		metrics.GetLogStatistics().CumulativeTiming(metrics.SequencingBatchTiming, time.Since(startSequenceTime))
		log.Info(fmt.Sprintf("[%s] Finished sequencing stage", logPrefix))
		metrics.GetLogStatistics().Summary()
	}()

	// at this point of time the datastream could not be ahead of the executor
	if err = validateIfDatastreamIsAheadOfExecution(s, ctx, cfg); err != nil {
		return err
	}

	// For X Layer, split db and ac
	sdb, err := newStageDb(ctx, cfg.db, cfg.dbsmt, cfg.zk.XLayer.EnableAsyncCommit)
	if err != nil {
		return err
	}
	defer func() {
		sdb.Rollback()

		if err != nil {
			if !cfg.zk.XLayer.EnableAsyncCommit {
				return
			}

			executionAt, _ := s.ExecutionAt(sdb.tx)
			if err != nil {
				return
			}

			s.FlushSmtCacheSignalInc()
			go func() {
				defer s.FlushSmtCacheDone()
				s.ResetCurrentBatchCache(executionAt + 1)
			}()
		}
	}()

	if sdb.supportAC {
		// For X Layer, split db and ac
		sdb.eridb.SetCache(s.GetSmtCache())
	}

	if err = cfg.infoTreeUpdater.WarmUp(sdb.tx); err != nil {
		return err
	}

	executionAt, err := s.ExecutionAt(sdb.tx)
	if err != nil {
		return err
	}

	lastBatch, err := stages.GetStageProgress(sdb.tx, stages.HighestSeenBatchNumber)
	if err != nil {
		return err
	}

	forkId, err := prepareForkId(lastBatch, executionAt, sdb.hermezDb, cfg)
	if err != nil {
		return err
	}

	// stage loop should continue until we get the forkid from the L1 in a finalised block
	if forkId == 0 {
		log.Warn(fmt.Sprintf("[%s] ForkId is 0. Waiting for L1 to finalise a block...", logPrefix))
		time.Sleep(10 * time.Second)
		return nil
	}

	batchNumberForStateInitialization, err := prepareBatchNumber(sdb, forkId, lastBatch, cfg.zk.L1SyncStartBlock > 0)
	if err != nil {
		return err
	}

	var block *types.Block
	runLoopBlocks := true
	batchContext := newBatchContext(ctx, &cfg, &historyCfg, s, sdb)
	batchState := newBatchState(forkId, batchNumberForStateInitialization, executionAt+1, cfg.zk.L1SyncStartBlock > 0, cfg.txPool, resequenceBatchJob)
	streamWriter := newSequencerBatchStreamWriter(batchContext, batchState)

	// injected batch
	if executionAt == 0 {
		if err = processInjectedInitialBatch(batchContext, batchState); err != nil {
			return err
		}

		if err = cfg.dataStreamServer.WriteWholeBatchToStream(logPrefix, sdb.tx, sdb.hermezDb.HermezDbReader, lastBatch, injectedBatchBatchNumber); err != nil {
			return err
		}
		if err = stages.SaveStageProgress(sdb.tx, stages.DataStream, 1); err != nil {
			return err
		}

		// For X Layer, split db and ac
		return sdb.Commit(s, executionAt+1, true)
	}

	// For X Layer, for auto recovery
	if cfg.zk.XLayer.StandaloneSMTDatabase && shouldCheckForExecutionAndSMTAlignment == SMTAlignmentInit {
		if !batchState.isAnyRecovery() {
			smtMaxBlockNumber, err := sdb.eridb.GetLastHeight()
			if err != nil {
				log.Error(fmt.Sprintf("[%s] Failed to get smt max block number", logPrefix), "error", err, "smtMaxBlockNumber", smtMaxBlockNumber)
				return err
			}
			if smtMaxBlockNumber+1 < executionAt {
				targetBlock := uint64(0)
				if smtMaxBlockNumber != 0 {
					targetBlock, err = getTargetBlockForSMTAlignment(sdb, logPrefix, executionAt, smtMaxBlockNumber)
					if err != nil {
						return err
					}
				}

				isUnwinding, err := unwindExecutionToSMT(batchContext, executionAt, targetBlock, u)
				if err != nil {
					return err
				}
				if isUnwinding {
					err = sdb.tx.Commit()
					if err != nil {
						return err
					}
					// set to pending resequence state
					shouldCheckForExecutionAndSMTAlignment = SMTAlignmentPendingResequence
					log.Warn(fmt.Sprintf("[%s] SMT alignment check triggered resequence", logPrefix))
					return nil
				}
			}
		}

		// set to terminated state, indicating verification is completed
		shouldCheckForExecutionAndSMTAlignment = SMTAlignmentTerminated
		log.Info(fmt.Sprintf("[%s] SMT alignment check completed", logPrefix))
	}

	if shouldCheckForExecutionAndDataStreamAlignment {
		// handle cases where the last batch wasn't committed to the data stream.
		// this could occur because we're migrating from an RPC node to a sequencer
		// or because the sequencer was restarted and not all processes completed (like waiting from remote executor)
		// we consider the data stream as verified by the executor so treat it as "safe" and unwind blocks beyond there
		// if we identify any.  During normal operation this function will simply check and move on without performing
		// any action.
		if !batchState.isAnyRecovery() {
			isUnwinding, err := alignExecutionToDatastream(batchContext, executionAt, u)
			if err != nil {
				// do not set shouldCheckForExecutionAndDataStreamAlighment=false because of the error
				return err
			}
			if isUnwinding {
				// For X Layer, split db and ac
				err := sdb.Commit(s, executionAt+1, true)
				if err != nil {
					// do not set shouldCheckForExecutionAndDataStreamAlighment=false because of the error
					return err
				}
				shouldCheckForExecutionAndDataStreamAlignment = false
				return nil
			}
		}
		shouldCheckForExecutionAndDataStreamAlignment = false
	}

	// For X Layer, split db and ac
	needsUnwind, exitStage, err := tryHaltSequencer(batchContext, batchState, streamWriter, u, executionAt, s)
	if needsUnwind || err != nil {
		return err
	}
	if exitStage {
		log.Info(fmt.Sprintf("[%s] Exiting stage during halted sequencer", logPrefix))
		// For X Layer, split db and ac
		// commit the tx so any updates to the stream etc are persisted
		return sdb.Commit(s, executionAt+1, true)
	}

	if err := utils.UpdateZkEVMBlockCfg(cfg.chainConfig, sdb.hermezDb, logPrefix); err != nil {
		return err
	}

	// Batch counters removed

	if batchState.isL1Recovery() {
		if cfg.zk.L1SyncStopBatch > 0 && batchState.batchNumber > cfg.zk.L1SyncStopBatch {
			log.Info(fmt.Sprintf("[%s] L1 recovery has completed!", logPrefix), "batch", batchState.batchNumber)
			time.Sleep(1 * time.Second)
			return nil
		}

		log.Info(fmt.Sprintf("[%s] L1 recovery beginning for batch", logPrefix), "batch", batchState.batchNumber)

		// let's check if we have any L1 data to recover
		if err = batchState.batchL1RecoveryData.loadBatchData(sdb); err != nil {
			return err
		}

		if !batchState.batchL1RecoveryData.hasAnyDecodedBlocks() {
			log.Info(fmt.Sprintf("[%s] L1 recovery has completed!", logPrefix), "batch", batchState.batchNumber)
			time.Sleep(1 * time.Second)
			return nil
		}

		bad := false
		for _, batch := range cfg.zk.BadBatches {
			if batch == batchState.batchNumber {
				bad = true
				break
			}
		}

		// if we aren't forcing a bad batch then check it
		if !bad {
			bad, err = doCheckForBadBatch(batchContext, batchState, executionAt)
			if err != nil {
				return err
			}
		}

		if bad {
			return writeBadBatchDetails(batchContext, batchState, executionAt)
		}
	}

	logTicker := time.NewTicker(10 * time.Second)
	infoTreeTicker := time.NewTicker(cfg.zk.InfoTreeUpdateInterval)
	defer logTicker.Stop()
	defer infoTreeTicker.Stop()

	batchTimer := time.NewTimer(cfg.zk.SequencerBatchSealTime)

	log.Info(fmt.Sprintf("[%s] Starting batch %d...", logPrefix, batchState.batchNumber))

	// For X Layer
	var batchCloseReason metrics.BatchFinalizeType
	cfg.yieldSize = apollo.GetYieldSize(cfg.yieldSize)

	// once the batch ticker has ticked we need a signal to close the batch after the next block is done
	batchTimedOut := false

	// to avoid nonce problems when a transaction causes the batch to overflow we need to temporarily skip handling transactions from the same sender
	// until the next batch starts
	sendersToSkip := make(map[common.Address]struct{})

	// For X Layer, split db and ac
	blockNumber := uint64(0)
	breakBatchLoop := false
BatchLoop:
	for blockNumber = executionAt + 1; runLoopBlocks; blockNumber++ {
		if batchTimedOut {
			log.Debug(fmt.Sprintf("[%s] Closing batch due to timeout", logPrefix))
			break
		}
		startTime := time.Now()
		log.Info(fmt.Sprintf("[%s] Starting block %d (forkid %v)...", logPrefix, blockNumber, batchState.forkId))
		utils.LogTrace(
			"",                          // txhash
			utils.ServiceNameSequencer,  // serviceName
			utils.StepSeqBeginBlock.ID,  // processId
			utils.StepSeqBeginBlock.Key, // processWord
			blockNumber,                 // blockHeight
			"",                          // blockHash
			0,                           // blockTime
			-1,                          // transactionType
		)

		logTicker.Reset(10 * time.Second)
		// For X Layer block timer
		blockTimer := time.NewTimer(cfg.zk.XLayer.SequencerMaxBlockSealTime)
		ethBlockGasPool := new(core.GasPool).AddGas(cfg.zk.XLayer.DynamicBlockGasLimit) // used only in normalcy mode per block

		if batchState.isL1Recovery() {
			blockNumbersInBatchSoFar, err := batchContext.sdb.hermezDb.GetL2BlockNosByBatch(batchState.batchNumber)
			if err != nil {
				return err
			}

			didLoadedAnyDataForRecovery := batchState.loadBlockL1RecoveryData(uint64(len(blockNumbersInBatchSoFar)))
			if !didLoadedAnyDataForRecovery {
				log.Info(fmt.Sprintf("[%s] Block %d is not part of batch %d. Stopping blocks loop", logPrefix, blockNumber, batchState.batchNumber))
				break
			}
		}

		if batchState.isResequence() {
			if !batchState.resequenceBatchJob.HasMoreBlockToProcess() {
				// Legacy verifier pending check removed
				runLoopBlocks = false
				break
			}
		}

		header, parentBlock, err := prepareHeader(sdb.tx, blockNumber-1, batchState.blockState.getDeltaTimestamp(), batchState.getBlockHeaderForcedTimestamp(), batchState.forkId, batchState.getCoinbase(&cfg), cfg.chainConfig, cfg.miningConfig)
		if err != nil {
			return err
		}

		// timer: evm + smt
		t := utils.StartTimer("stage_sequence_execute", "evm", "smt")

		infoTreeIndexProgress, l1TreeUpdate, l1TreeUpdateIndex, l1BlockHash, ger, shouldWriteGerToContract, err := prepareL1AndInfoTreeRelatedStuff(logPrefix, sdb, batchState, header.Time, cfg.zk.SequencerResequenceReuseL1InfoIndex, cfg.zk.SequencerResequenceInfoTreeOffset)
		if err != nil {
			return err
		}

		// Counter overflow check removed

		ibs := state.New(sdb.stateReader)
		getHashFn := core.GetHashFn(header, func(hash common.Hash, number uint64) *types.Header { return rawdb.ReadHeader(sdb.tx, hash, number) })
		coinbase := batchState.getCoinbase(&cfg)
		blockContext := core.NewEVMBlockContext(header, getHashFn, cfg.engine, &coinbase)
		batchState.blockState.builtBlockElements.resetBlockBuildingArrays()

		parentRoot := parentBlock.Root()
		if err = handleStateForNewBlockStarting(batchContext, ibs, blockNumber, batchState.batchNumber, header.Time, &parentRoot, l1TreeUpdate, shouldWriteGerToContract); err != nil {
			return err
		}

		// start waiting for a new transaction to arrive
		if !batchState.isAnyRecovery() {
			log.Info(fmt.Sprintf("[%s] Waiting for txs from the pool...", logPrefix))
		}

		innerBreak := false
		emptyBlockOverflow := false

		// For X Layer, local replay's feature of stateroot mismatch detection
		stateRootBeforeReplay := common.Hash{}

		sendersToTriggerStatechanges := make(map[common.Address]struct{})
		processingTxTime := time.Now()

		// For X Layer, realtime. Send kafka block header
		if cfg.zk.XLayer.Realtime.Enable && cfg.kafkaNewBlockInfoChan != nil {
			cfg.kafkaNewBlockInfoChan <- header
		}

	OuterLoopTransactions:
		for {
			if innerBreak {
				break
			}
			// For X Layer, block timer
			if len(batchState.blockState.builtBlockElements.transactions) > 0 && time.Since(startTime) >= cfg.zk.SequencerBlockSealTime {
				blockTimer.Reset(0)
			}

			select {
			case <-logTicker.C:
				if !batchState.isAnyRecovery() {
					log.Info(fmt.Sprintf("[%s] Waiting some more for txs from the pool...", logPrefix))
				}
			default:
			}

			select {
			case <-batchTimer.C:
				if !batchState.isAnyRecovery() {
					log.Debug(fmt.Sprintf("[%s] Batch timeout reached", logPrefix))
					batchTimedOut = true
				}
			default:
			}

			select {
			case <-blockTimer.C:
				if !batchState.isAnyRecovery() {
					break OuterLoopTransactions
				}
			default:
			}

			select {
			case <-infoTreeTicker.C:
				processedLogs, err := cfg.infoTreeUpdater.CheckForInfoTreeUpdates(logPrefix, sdb.tx)
				if err != nil {
					return err
				}
				var latestIndex uint64
				latest := cfg.infoTreeUpdater.GetLatestUpdate()
				if latest != nil {
					latestIndex = latest.Index
				}
				log.Info(fmt.Sprintf("[%s] Info tree updates", logPrefix), "count", processedLogs, "latestIndex", latestIndex)
			default:
			}

			getTxTime := time.Now()
			if batchState.isLimboRecovery() {
				batchState.blockState.transactionsForInclusion, err = getLimboTransaction(ctx, cfg, batchState.limboRecoveryData.limboTxHash, executionAt)
				if err != nil {
					return err
				}
			} else if batchState.isResequence() {
				batchState.blockState.transactionsForInclusion, err = batchState.resequenceBatchJob.YieldNextBlockTransactions(zktx.DecodeTx)
				if err != nil {
					return err
				}

				// For X Layer, local replay and smt alignment's feature of stateroot mismatch detection
				if cfg.zk.XLayer.SequencerReplay || shouldCheckForExecutionAndSMTAlignment == SMTAlignmentPendingResequence {
					stateRootBeforeReplay = batchState.resequenceBatchJob.CurrentBlock().StateRoot
					log.Info(fmt.Sprintf("[%s] State root before replay", logPrefix), "stateRoot", stateRootBeforeReplay)
				}
			} else if !batchState.isL1Recovery() {

				var allConditionsOK bool
				var newTransactions []types.Transaction
				var newIds []common.Hash
				newTransactions, newIds, allConditionsOK, err = getNextPoolTransactions(ctx, cfg, executionAt, batchState.forkId, batchState.yieldedTransactions)
				if err != nil {
					return err
				}

				metrics.GetLogStatistics().CumulativeTiming(metrics.GetTxTiming, time.Since(getTxTime))

				batchState.blockState.transactionsForInclusion = append(batchState.blockState.transactionsForInclusion, newTransactions...)
				for idx, tx := range newTransactions {
					batchState.blockState.transactionHashesToSlots[tx.Hash()] = newIds[idx]
				}

				if len(batchState.blockState.transactionsForInclusion) == 0 {
					pauseTime := time.Now()
					if allConditionsOK {
						time.Sleep(batchContext.cfg.zk.SequencerTimeoutOnEmptyTxPool)
					} else {
						time.Sleep(batchContext.cfg.zk.SequencerTimeoutOnEmptyTxPool / 5) // we do not need to sleep too long for txpool not ready
					}
					metrics.GetLogStatistics().CumulativeCounting(metrics.GetTxPauseCounter)
					metrics.GetLogStatistics().CumulativeTiming(metrics.GetTxPauseTiming, time.Since(pauseTime))
				} else {
					log.Trace(fmt.Sprintf("[%s] Yielded transactions from the pool", logPrefix), "txCount", len(batchState.blockState.transactionsForInclusion))
				}
			}

			// For X Layer
			txpool.ArquireTxPoolLock(false)

			if len(batchState.blockState.transactionsForInclusion) == 0 {
				if !batchState.isAnyRecovery() {
					pauseTime := time.Now()
					time.Sleep(batchContext.cfg.zk.SequencerTimeoutOnEmptyTxPool)
					metrics.GetLogStatistics().CumulativeCounting(metrics.GetTxPauseCounter)
					metrics.GetLogStatistics().CumulativeTiming(metrics.GetTxPauseTiming, time.Since(pauseTime))
				}
			} else {
				log.Trace(fmt.Sprintf("[%s] Yielded transactions from the pool", logPrefix), "txCount", len(batchState.blockState.transactionsForInclusion))
			}

			badTxHashes := make([]common.Hash, 0)
			minedTxHashes := make([]common.Hash, 0)

		InnerLoopTransactions:
			for i, transaction := range batchState.blockState.transactionsForInclusion {
				// quick check if we should stop handling transactions
				select {
				case <-blockTimer.C:
					if !batchState.isAnyRecovery() {
						innerBreak = true
						break InnerLoopTransactions
					}
				default:
				}

				txHash := transaction.Hash()

				txSender, ok := transaction.GetSender()
				if !ok {
					signer := types.MakeSigner(cfg.chainConfig, executionAt, 0)
					sender, err := signer.Sender(transaction)
					if err != nil {
						log.Warn("[extractTransaction] Failed to recover sender from transaction, skipping and removing from pool",
							"error", err,
							"hash", transaction.Hash())
						badTxHashes = append(badTxHashes, txHash)
						batchState.blockState.transactionsToDiscard = append(batchState.blockState.transactionsToDiscard, batchState.blockState.transactionHashesToSlots[txHash])
						continue
					}

					transaction.SetSender(sender)
					txSender = sender
				}

				if _, found := sendersToSkip[txSender]; found {
					continue
				}

				effectiveGas := batchState.blockState.getL1EffectiveGases(cfg, i)

				receipt, execResult, _, anyOverflow, err := attemptAddTransaction(cfg, sdb, ibs, &blockContext, header, transaction, effectiveGas, batchState.isL1Recovery(), batchState.forkId, l1TreeUpdateIndex, ethBlockGasPool, len(batchState.blockState.builtBlockElements.transactions))
				if err != nil {
					metrics.GetLogStatistics().CumulativeCounting(metrics.ProcessingInvalidTxCounter)
					if batchState.isLimboRecovery() {
						panic("limbo transaction has already been executed once so they must not fail while re-executing")
					}

					if batchState.isResequence() {
						if cfg.zk.SequencerResequenceStrict {
							return fmt.Errorf("strict mode enabled, but resequenced batch %d failed to add transaction %s: %v", batchState.batchNumber, txHash, err)
						} else {
							log.Warn(fmt.Sprintf("[%s] error adding transaction to batch during resequence: %v", logPrefix, err),
								"hash", txHash,
								"to", transaction.GetTo(),
							)
							continue
						}
					}

					// if we are in recovery just log the error as a warning.  If the data is on the L1 then we should consider it as confirmed.
					// The executor/prover would simply skip a TX with an invalid nonce for example so we don't need to worry about that here.
					if batchState.isL1Recovery() {
						log.Warn(fmt.Sprintf("[%s] error adding transaction to batch during recovery: %v", logPrefix, err),
							"hash", txHash,
							"to", transaction.GetTo(),
						)
						continue
					}

					if errors.Is(err, core.ErrNonceTooHigh) || errors.Is(err, core.ErrNonceTooLow) {
						// here we have a case where some situation has caused a nonce issue to find its way into the pending pool
						// we want to skip transactions for this sender in this batch for now and ask the pool to trigger a sender
						// state change for this sender.  This will cause the pool to skip any transactions from this sender until
						// the sender's nonce is corrected in the pending pool
						log.Info(fmt.Sprintf("[%s] nonce issue detected for sender, skipping transactions for now", logPrefix), "sender", txSender.Hex(), "nonceIssue", err)
						sendersToSkip[txSender] = struct{}{}
						sendersToTriggerStatechanges[txSender] = struct{}{}
						continue
					}

					// if we have an error at this point something has gone wrong, either in the pool or otherwise
					// to stop the pool growing and hampering further processing of good transactions here
					// we mark it for being discarded
					log.Warn(fmt.Sprintf("[%s] error adding transaction to batch, discarding from pool", logPrefix), "hash", txHash, "err", err)
					badTxHashes = append(badTxHashes, txHash)
					batchState.blockState.transactionsToDiscard = append(batchState.blockState.transactionsToDiscard, batchState.blockState.transactionHashesToSlots[txHash])
				}

				switch anyOverflow {
				case overflowCounters:
					panic("unreachable")
				case overflowGas:
					metrics.GetLogStatistics().CumulativeCounting(metrics.FailTxGasOverCounter)
					if batchState.isAnyRecovery() {
						panic(fmt.Sprintf("block gas limit overflow in recovery block: %d", blockNumber))
					}
					log.Info(fmt.Sprintf("[%s] gas overflowed adding transaction to block", logPrefix), "block", blockNumber, "tx-hash", txHash)
					runLoopBlocks = false
					break OuterLoopTransactions
				case overflowNone:
				}

				if err == nil {
					metrics.GetLogStatistics().CumulativeValue(metrics.BatchGas, int64(execResult.UsedGas))
					batchState.onAddedTransaction(transaction, receipt, execResult, effectiveGas)
					minedTxHashes = append(minedTxHashes, txHash)
				}

				// We will only update the processed index in resequence job if there isn't overflow
				if batchState.isResequence() {
					batchState.resequenceBatchJob.UpdateLastProcessedTx(txHash)
				}
			}

			if batchState.isResequence() {
				if len(batchState.blockState.transactionsForInclusion) == 0 {
					// We need to jump to the next block here if there are no transactions in current block
					batchState.resequenceBatchJob.UpdateLastProcessedTx(batchState.resequenceBatchJob.CurrentBlock().L2Blockhash)
					break OuterLoopTransactions
				}

				if batchState.resequenceBatchJob.AtNewBlockBoundary() {
					// We need to jump to the next block here if we are at the end of the current block
					break OuterLoopTransactions
				} else {
					if cfg.zk.SequencerResequenceStrict {
						return fmt.Errorf("strict mode enabled, but resequenced batch %d has transactions that overflowed counters or failed transactions", batchState.batchNumber)
					}
				}
			}

			// remove bad and mined transactions from the list for inclusion
			for i := len(batchState.blockState.transactionsForInclusion) - 1; i >= 0; i-- {
				tx := batchState.blockState.transactionsForInclusion[i]
				hash := tx.Hash()
				for _, badHash := range badTxHashes {
					if badHash == hash {
						batchState.blockState.transactionsForInclusion = removeInclusionTransaction(batchState.blockState.transactionsForInclusion, i)
						break
					}
				}

				for _, minedHash := range minedTxHashes {
					if minedHash == hash {
						batchState.blockState.transactionsForInclusion = removeInclusionTransaction(batchState.blockState.transactionsForInclusion, i)
						break
					}
				}
			}

			if batchState.isL1Recovery() {
				// just go into the normal loop waiting for new transactions to signal that the recovery
				// has finished as far as it can go
				if !batchState.isThereAnyTransactionsToRecover() {
					log.Info(fmt.Sprintf("[%s] L1 recovery no more transactions to recover", logPrefix))
				}

				break OuterLoopTransactions
			}

			if batchState.isLimboRecovery() {
				runLoopBlocks = false
				break OuterLoopTransactions
			}
		}

		// For X Layer
		metrics.GetLogStatistics().CumulativeTiming(metrics.ProcessingTxTiming, time.Since(processingTxTime))

		// we do not want to commit this block if it has no transactions and we detected an overflow - essentially the batch is too
		// full to get any more transactions in it and we don't want to commit an empty block
		if emptyBlockOverflow {
			log.Info(fmt.Sprintf("[%s] Block %d overflow detected with no transactions added, skipping block for next batch", logPrefix, blockNumber))
			break
		}

		// 0 TX block handling:
		// if we had some transactions yielded but didn't mine any in this block then we shouldn't commit it and move on.
		// this could happen if there were lots of nonce issues from transaction in the pool due to a failed tx processing or similar and
		// there wasn't much time left in the batch to mine any transactions
		if len(batchState.blockState.transactionsForInclusion) > 0 && len(batchState.blockState.builtBlockElements.transactions) == 0 {
			if cfg.zk.XLayer.SequencerSkipEmptyBlocks {
				log.Warn(fmt.Sprintf("[%s] Skipping block: no transactions mined in block %d, skipping block for now", logPrefix, blockNumber))
				break
			}
			log.Warn(fmt.Sprintf("[%s] Closing batch to keep liveness when encountering empty block %d", logPrefix, blockNumber))
			breakBatchLoop = true
		}

		// For X Layer, split db and ac
		if batchContext.sdb.supportAC {
			quit := batchContext.ctx.Done()
			batchContext.sdb.eridb.OpenBatch(quit)           // do nothing...
			batchContext.sdb.eridb.SetCache(s.GetSmtCache()) // will deep copy in internal function
			if block, err = doFinishBlockAndUpdateState(batchContext, ibs, header, parentBlock, batchState, ger, l1BlockHash, l1TreeUpdateIndex, infoTreeIndexProgress); err != nil {
				batchContext.sdb.eridb.RollbackBatch()
				return err
			}
			commitSmtTime := time.Now()
			blockCache := batchContext.sdb.eridb.RetriveAndCleanCache()
			if err := batchContext.sdb.eridb.CommitBatch(); err != nil {
				return err
			}
			metrics.GetLogStatistics().CumulativeTiming(metrics.SmtBatchCommitDBTiming, time.Since(commitSmtTime))
			setTime := time.Now()
			s.SetSmtCache(blockNumber, blockCache)
			metrics.GetLogStatistics().CumulativeTiming(metrics.SetSmtCacheTiming, time.Since(setTime))
		} else {
			quit := batchContext.ctx.Done()
			batchContext.sdb.eridb.OpenBatch(quit)
			if block, err = doFinishBlockAndUpdateState(batchContext, ibs, header, parentBlock, batchState, ger, l1BlockHash, l1TreeUpdateIndex, infoTreeIndexProgress); err != nil {
				batchContext.sdb.eridb.RollbackBatch()
				return err
			}
			commitSmtTime := time.Now()
			if err := batchContext.sdb.eridb.CommitBatch(); err != nil {
				return err
			}
			metrics.GetLogStatistics().CumulativeTiming(metrics.SmtBatchCommitDBTiming, time.Since(commitSmtTime))
		}

		// For X Layer
		metrics.GetLogStatistics().CumulativeCounting(metrics.BlockCounter)
		// Count successful transactions
		metrics.GetLogStatistics().CumulativeValue(metrics.TxCounter, int64(len(batchState.blockState.builtBlockElements.transactions)))

		// add a check to the verifier and also check for responses
		batchState.onBuiltBlock(blockNumber)

		// check if we are in limbo recovery and update the pool with the new state root for the latest transaction
		// being checked then return before committing anything about the block to the DB
		if batchState.isLimboRecovery() {
			stateRoot := block.Root()
			cfg.txPool.UpdateLimboRootByTxHash(batchState.limboRecoveryData.limboTxHash, &stateRoot)
			return fmt.Errorf("[%s] %w: %s = %s", s.LogPrefix(), zk.ErrLimboState, batchState.limboRecoveryData.limboTxHash.Hex(), stateRoot.Hex())
		}

		// For X Layer
		txpool.ArquireTxPoolLock(true)

		if !batchState.isL1Recovery() {
			commitTime := time.Now()
			// commit block data here so it is accessible in other threads
			if errCommitAndStart := sdb.CommitAndStart(); errCommitAndStart != nil {
				return errCommitAndStart
			}
			// For X Layer, split db and ac
			defer sdb.Rollback()
			metrics.GetLogStatistics().CumulativeTiming(metrics.BatchCommitDBTiming, time.Since(commitTime))
		}

		// remove mined transactions from the pool
		toRemove := append(batchState.blockState.builtBlockElements.txSlots, batchState.blockState.transactionsToDiscard...)
		if err := cfg.txPool.RemoveMinedTransactions(ctx, sdb.tx, header.GasLimit, toRemove); err != nil {
			return err
		}

		// now trigger sender state changes in the pool where we encountered nonce issues during execution
		if err := cfg.txPool.TriggerSenderStateChanges(ctx, sdb.tx, header.GasLimit, sendersToTriggerStatechanges); err != nil {
			return err
		}

		t.LogTimer()
		gasPerSecond := float64(0)
		elapsedSeconds := t.Elapsed().Seconds()
		if elapsedSeconds != 0 {
			gasPerSecond = float64(block.GasUsed()) / elapsedSeconds
		}

		if gasPerSecond != 0 {
			log.Info(fmt.Sprintf("[%s] Finish block %d with %d transactions... (%d gas/s)", logPrefix, blockNumber, len(batchState.blockState.builtBlockElements.transactions), int(gasPerSecond)), "info-tree-index", infoTreeIndexProgress, "taken", time.Since(startTime))
		} else {
			log.Info(fmt.Sprintf("[%s] Finish block %d with %d transactions...", logPrefix, blockNumber, len(batchState.blockState.builtBlockElements.transactions)), "info-tree-index", infoTreeIndexProgress, "taken", time.Since(startTime))
		}

		utils.LogTrace(
			"",                         // txhash
			utils.ServiceNameSequencer, // serviceName
			utils.StepSeqEndBlock.ID,   // processId
			utils.StepSeqEndBlock.Key,  // processWord
			blockNumber,                // blockHeight
			block.Hash().String(),      // blockHash
			block.Time(),               // blockTime
			-1,                         // transactionType
		)

		// For X Layer, local replay and smt alignment's feature of stateroot mismatch detection
		if cfg.zk.XLayer.SequencerReplay || shouldCheckForExecutionAndSMTAlignment == SMTAlignmentPendingResequence {
			if stateRootBeforeReplay != block.Root() {
				err := fmt.Errorf("[%s] State root mismatch of block %d after resequencing, expected %s, got %s",
					logPrefix,
					blockNumber,
					stateRootBeforeReplay.Hex(),
					block.Root().Hex(),
				)
				log.Error(err.Error())
				os.Exit(1)
			}
		}

		if err := streamWriter.WriteBlockDetailsToDatastream(batchState.forkId, batchState.batchNumber, batchState.builtBlocks); err != nil {
			return err
		}
		// For X Layer, realtime
		if cfg.zk.XLayer.Realtime.Enable && cfg.kafkaConfirmedBlockInfoChan != nil {
			cfg.kafkaConfirmedBlockInfoChan <- block
		}

		// lets commit everything after updateStreamAndCheckRollback no matter of its result unless
		// we're in L1 recovery where losing some blocks on restart doesn't matter

		if !batchState.isL1Recovery() {
			commitTime := time.Now()
			if errCommitAndStart := sdb.CommitAndStart(); errCommitAndStart != nil {
				return errCommitAndStart
			}
			// For X Layer, split db and ac
			defer sdb.Rollback()
			metrics.GetLogStatistics().CumulativeTiming(metrics.BatchCommitDBTiming, time.Since(commitTime))
		}

		if _, err := rawdb.IncrementStateVersionByBlockNumberIfNeeded(batchContext.sdb.tx, block.NumberU64()); err != nil {
			return fmt.Errorf("writing plain state version: %w", err)
		}

		// notify the done hook that we have finished processing this block - will notify subscribers etc.
		// here we -1 the block number as we know we have just created a new block so can simulate that the last block notified
		// was the previous block created
		if err := cfg.doneHook.AfterRun(batchContext.sdb.tx, block.NumberU64()-1, s.PrevUnwindPoint()); err != nil {
			return err
		}

		// For X Layer
		metrics.GetLogStatistics().SetTag(metrics.FinalizeBlockNumber, strconv.Itoa(int(blockNumber)))
		metrics.GetLogStatistics().SummaryCheckpoint()

		if breakBatchLoop {
			break BatchLoop
		}
	}

	/*
		if adding something below that line we must ensure
		- it is also handled property in processInjectedInitialBatch
		- it is also handled property in alignExecutionToDatastream
		- it is also handled property in doCheckForBadBatch
		- it is unwound correctly
	*/

	log.Info(fmt.Sprintf("[%s] Finish batch %d...", batchContext.s.LogPrefix(), batchState.batchNumber))

	// For X Layer
	metrics.GetLogStatistics().SetTag(metrics.BatchCloseReason, string(batchCloseReason))
	metrics.GetLogStatistics().SetTag(metrics.FinalizeBatchNumber, strconv.Itoa(int(batchState.batchNumber)))
	tryToSleepSequencer(cfg.zk.XLayer.SequencerBatchSleepDuration, logPrefix)
	startCommitTime := time.Now()
	// For X Layer, split db and ac
	err = sdb.Commit(s, blockNumber, false)
	metrics.GetLogStatistics().CumulativeTiming(metrics.BatchCommitDBTiming, time.Since(startCommitTime))

	return err
}

func removeInclusionTransaction(orig []types.Transaction, index int) []types.Transaction {
	if index < 0 || index >= len(orig) {
		return orig
	}
	return append(orig[:index], orig[index+1:]...)
}

func handleBadTxHashCounter(hermezDb *hermez_db.HermezDb, txHash common.Hash) (uint64, error) {
	counter, err := hermezDb.GetBadTxHashCounter(txHash)
	if err != nil {
		return 0, err
	}
	newCounter := counter + 1
	hermezDb.WriteBadTxHashCounter(txHash, newCounter)
	return newCounter, nil
}
