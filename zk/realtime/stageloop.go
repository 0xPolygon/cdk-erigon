package realtime

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/zk/realtime/cache"
	"github.com/ledgerwatch/erigon/zk/realtime/kafka"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	realtimeSub "github.com/ledgerwatch/erigon/zk/realtime/subscription"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	"github.com/ledgerwatch/erigon/zk/sequencer"
	"github.com/ledgerwatch/log/v3"
)

var (
	MaxKafkaChanSize        = 10_000
	MaxKafkaCacheSize       = 1_000
	MinRealtimeLoopWaitTime = 10 * time.Millisecond

	readyFlag  = atomic.Bool{}
	errorFlag  = atomic.Bool{}
	resetFlag  = atomic.Bool{}
	kafkaCache *cache.KafkaCache
)

func ListenTxKafkaProducer(
	ctx context.Context,
	txKafkaProducer *kafka.KafkaProducer,
	logger log.Logger,
	blockInfoChan chan *realtimeTypes.BlockInfo,
	txInfoChan chan *state.TxInfo) {
	if !sequencer.IsSequencer() {
		logger.Info("[Realtime] TxKafkaProducer is disabled on non-sequencer, skipping")
		return
	}

	for {
		var err error
		currHeight := uint64(0)

		select {
		case <-ctx.Done():
			return
		case blockInfo := <-blockInfoChan:
			currHeight = blockInfo.Header.Number.Uint64()
			err = txKafkaProducer.SendKafkaBlockInfo(ctx, blockInfo.Header, blockInfo.TxCount)
		case txInfo := <-txInfoChan:
			currHeight = txInfo.BlockNumber
			if currHeight <= 1 {
				continue
			}
			changeset := state.CollectChangeset(txInfo.Entries)
			err = txKafkaProducer.SendKafkaTransaction(ctx, txInfo.BlockNumber, txInfo.Tx, txInfo.Receipt, txInfo.InnerTxs, changeset)
		}

		if err != nil {
			logger.Error("[Realtime] Failed to send kafka message, trigger error message", "error", err, "currHeight", currHeight)
			err = txKafkaProducer.SendKafkaErrorTrigger(ctx, currHeight)
			if err != nil {
				logger.Error("[Realtime] Failed to send error trigger message", "error", err, "blockNumber", currHeight)
			}
			continue
		}
	}
}

func ListenTxKafkaConsumer(
	ctx context.Context,
	txKafkaConsumer *kafka.KafkaConsumer,
	logger log.Logger,
	realtimeCache *cache.RealtimeCache,
	finishChan chan uint64,
	subService *realtimeSub.RealtimeSubscription) {
	if sequencer.IsSequencer() {
		logger.Info("[Realtime] TxKafkaConsumer is disabled on sequencer, skipping")
		return
	}

	// Initialize kafka cache
	var err error
	kafkaCache, err = cache.NewKafkaCache(MaxKafkaCacheSize)
	if err != nil {
		logger.Error("[Realtime] Failed to initialize kafka cache", "error", err)
		return
	}

	errorFlag.Store(false)
	blockMsgsChan := make(chan kafkaTypes.BlockMessage, MaxKafkaChanSize)
	txMsgsChan := make(chan kafkaTypes.TransactionMessage, MaxKafkaChanSize)
	errorMsgsChan := make(chan kafkaTypes.ErrorTriggerMessage, MaxKafkaChanSize)
	errorChan := make(chan error, 1)

	// Start the kafka consumer
	go txKafkaConsumer.ConsumeKafka(ctx, blockMsgsChan, txMsgsChan, errorMsgsChan, errorChan, logger)

	// Start realtime loop
	go realtimeLoop(ctx, logger, realtimeCache)

	for {
		select {
		case <-ctx.Done():
			return
		case finishHeight := <-finishChan:
			if finishHeight < realtimeCache.GetExecutionHeight() {
				// Chain rollback. Reset realtime cache
				resetFlag.Store(true)
				logger.Debug("[Realtime] Chain rollback detected, resetting realtime cache", "finishHeight", finishHeight)
			}
			realtimeCache.PutExecutionHeight(finishHeight)
			logger.Debug("[Realtime] Received finish signal from execution", "finishHeight", finishHeight)
		case blockMsg := <-blockMsgsChan:
			header, _, err := blockMsg.GetBlockInfo()
			if err != nil {
				logger.Error("[Realtime] Failed to consume block message from kafka", "error", err)
				continue
			}
			if header.Number.Uint64() <= realtimeCache.GetExecutionHeight() {
				// Ignore block msgs from previous blocks
				logger.Debug("[Realtime] Ignoring block message from previous block", "blockNum", header.Number)
				continue
			}
			kafkaCache.BlockMsgCache.Add(&blockMsg)
			if subService != nil {
				// Publish block to subscriptions
				subService.BroadcastNewMsg(&blockMsg, nil)
			}
			logger.Debug("[Realtime] Received block message", "blockNum", header.Number)
		case txMsg := <-txMsgsChan:
			if err := txMsg.Validate(); err != nil {
				logger.Error("[Realtime] Failed to consume transaction message from kafka", "error", err)
				continue
			}
			if txMsg.BlockNumber <= realtimeCache.GetExecutionHeight() {
				// Ignore txs from previous blocks
				logger.Debug("[Realtime] Ignoring transaction message from previous block", "blockNum", txMsg.BlockNumber)
				continue
			}
			kafkaCache.TxMsgCache.Add(&txMsg)
			if subService != nil {
				// Publish tx to subscriptions
				subService.BroadcastNewMsg(nil, &txMsg)
			}
			logger.Debug("[Realtime] Received transaction message", "blockNum", txMsg.BlockNumber)
		case errorTriggerMsg := <-errorMsgsChan:
			resetFlag.Store(true)
			triggerHeight := errorTriggerMsg.BlockNumber
			logger.Debug("[Realtime] Received error trigger message, flushing realtime cache", "triggerHeight", triggerHeight)
		case err := <-errorChan:
			errorFlag.Store(true)
			logger.Error("[Realtime] Kafka consumer failed", "error", err)
			return
		}
	}
}

func realtimeLoop(ctx context.Context, logger log.Logger, realtimeCache *cache.RealtimeCache) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		startTime := time.Now()

		// Check for kafka error
		if errorFlag.Load() {
			logger.Error("[Realtime] Kafka error, stopping realtime loop")
			return
		}

		// Check for reset trigger
		if resetFlag.Load() {
			resetRealtimeCache(realtimeCache)
			continue
		}

		// Check if realtime cache is ready
		if !readyFlag.Load() {
			if ok := tryInitRealtimeCache(realtimeCache, logger); !ok {
				time.Sleep(1 * time.Second)
			}
			continue
		}

		// Check for corrupted cache
		pendingHeight := realtimeCache.GetHighestPendingHeight()
		lastExecutionHeight := realtimeCache.GetExecutionHeight()
		if pendingHeight != 0 && pendingHeight < lastExecutionHeight {
			// Execution is ahead of pending cache. This should not happen
			resetFlag.Store(true)
			logger.Error("[Realtime] Execution height is ahead of cache confirm height", "pendingHeight", pendingHeight, "lastExecutionHeight", lastExecutionHeight)
			continue
		}

		// Sync state cache with kafka data
		lowestKafkaHeight := kafkaCache.GetLowestBlockHeight()
		if lowestKafkaHeight != 0 {
			// New block msg to process. Enforce that header msgs are processed in order
			nextHeight := pendingHeight + 1
			if pendingHeight == 0 {
				// First block msg after cache init
				nextHeight = realtimeCache.State.GetInitHeight() + 1
			}

			// Get next block msg and tx msgs
			blockMsg, ok := kafkaCache.BlockMsgCache.Pop(nextHeight)
			if ok {
				// Try close the previous block
				realtimeCache.TryCloseBlockFromBlockMsg(pendingHeight, blockMsg)

				// Process block msg
				err := realtimeCache.TryApplyBlockMsg(nextHeight, blockMsg)
				if err != nil {
					// Apply state error. Reset cache
					resetFlag.Store(true)
					logger.Error("[Realtime] Failed to apply block msg and tx msgs", "error", err, "nextHeight", nextHeight)
				}

				// Flush block msg cache
				kafkaCache.BlockMsgCache.Flush(nextHeight)
			}
		}

		// Handle pending blocks
		err := realtimeCache.HandlePendingBlocks(kafkaCache)
		if err != nil {
			// Handle pending blocks error. Reset cache
			resetFlag.Store(true)
			logger.Error("[Realtime] Handle pending blocks failed", "error", err)
		}

		duration := time.Since(startTime)
		if duration < MinRealtimeLoopWaitTime {
			time.Sleep(MinRealtimeLoopWaitTime - duration)
		}
	}
}

// tryInitRealtimeCache checks if the realtime cache can be initialized by comparing
// the current execution height with the lowest kafka cache height.
func tryInitRealtimeCache(realtimeCache *cache.RealtimeCache, logger log.Logger) bool {
	executionHeight := realtimeCache.GetExecutionHeight()
	lowestKafkaHeight := kafkaCache.GetLowestBlockHeight()
	if executionHeight == 0 || lowestKafkaHeight == 0 {
		// No kafka message or rpc execution. Skip init
		return false
	}

	if lowestKafkaHeight > executionHeight {
		// The current execution height is behind kafka cache height. We will wait for the execution
		// height to catch up to kafka cache height before re-initializing the state cache.
		logger.Info("[Realtime] Init realtime cache failed, waiting for execution height to catch up to kafka cache height", "lowestKafkaHeight", lowestKafkaHeight, "executionHeight", executionHeight)
		return false
	}

	realtimeCache.Clear()
	err := realtimeCache.TryInitStateCache(executionHeight)
	if err != nil {
		logger.Error("[Realtime] Failed to initialize state cache", "error", err)
		return false
	}

	// Flush all kafka data less than or equal to state cache height
	kafkaCache.Flush(executionHeight)
	readyFlag.Store(true)
	logger.Info("[Realtime] Realtime cache initialized", "executionHeight", executionHeight)

	return true
}

// resetRealtimeCache clears the realtime cache and resets the state flags
func resetRealtimeCache(realtimeCache *cache.RealtimeCache) {
	realtimeCache.Clear()
	resetFlag.Store(false)
	readyFlag.Store(false)
}
