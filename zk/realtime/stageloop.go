package realtime

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/zk/realtime/cache"
	"github.com/ledgerwatch/erigon/zk/realtime/kafka"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	realtimeSub "github.com/ledgerwatch/erigon/zk/realtime/subscription"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	"github.com/ledgerwatch/erigon/zk/sequencer"
	"github.com/ledgerwatch/erigon/zkevm/log"
)

var (
	MaxKafkaChanSize        = 10_000
	MaxKafkaCacheSize       = 1_000
	MinRealtimeLoopWaitTime = 10 * time.Millisecond

	errorFlag  = atomic.Bool{}
	resetFlag  = atomic.Bool{}
	kafkaCache *cache.KafkaCache
)

func ListenKafkaProducer(
	ctx context.Context,
	kafkaProducer *kafka.KafkaProducer,
	blockInfoChan chan *types.Header,
	txInfoChan chan state.TxInfo) {
	if !sequencer.IsSequencer() {
		log.Info("[Realtime] KafkaProducer is disabled on non-sequencer, skipping")
		return
	}

	for {
		currHeight := uint64(0)

		select {
		case <-ctx.Done():
			return
		case header := <-blockInfoChan:
			currHeight = header.Number.Uint64()
			err := kafkaProducer.SendKafkaBlockInfo(header)
			if err != nil {
				log.Error(fmt.Sprintf("[Realtime] Failed to send kafka block info message. error: %v, currHeight: %d", err, currHeight))
				err = kafkaProducer.SendKafkaErrorTrigger(currHeight)
				if err != nil {
					log.Error(fmt.Sprintf("[Realtime] Failed to send error trigger message. error: %v, currHeight: %d", err, currHeight))
				}
			} else {
				log.Debug(fmt.Sprintf("[Realtime] Sent kafka block info message for block number %d", header.Number))
			}
		case txInfo := <-txInfoChan:
			currHeight = txInfo.BlockNumber
			changeset := state.CollectChangeset(txInfo.Entries)
			err := kafkaProducer.SendKafkaTransaction(txInfo.BlockNumber, txInfo.Tx, txInfo.Receipt, txInfo.InnerTxs, changeset)
			if err != nil {
				log.Error(fmt.Sprintf("[Realtime] Failed to send kafka tx message. error: %v, currHeight: %d", err, currHeight))
				err = kafkaProducer.SendKafkaErrorTrigger(currHeight)
				if err != nil {
					log.Error(fmt.Sprintf("[Realtime] Failed to send error trigger message. error: %v, currHeight: %d", err, currHeight))
				}
			} else {
				log.Debug(fmt.Sprintf("[Realtime] Sent kafka tx message for block number %d with txHash %x", txInfo.BlockNumber, txInfo.Tx.Hash()))
			}
		}
	}
}

func ListenKafkaConsumer(
	ctx context.Context,
	kafkaConsumer *kafka.KafkaConsumer,
	realtimeCache *cache.RealtimeCache,
	finishChan chan realtimeTypes.FinishedEntry,
	subService *realtimeSub.RealtimeSubscription) {
	if sequencer.IsSequencer() {
		log.Info("[Realtime] KafkaConsumer is disabled on sequencer, skipping")
		return
	}

	// Initialize kafka cache
	var err error
	kafkaCache, err = cache.NewKafkaCache(MaxKafkaCacheSize)
	if err != nil {
		log.Error(fmt.Sprintf("[Realtime] Failed to initialize kafka cache. error: %v", err))
		return
	}

	errorFlag.Store(false)
	blockMsgsChan := make(chan kafkaTypes.BlockMessage, MaxKafkaChanSize)
	txMsgsChan := make(chan kafkaTypes.TransactionMessage, MaxKafkaChanSize)
	errorMsgsChan := make(chan kafkaTypes.ErrorTriggerMessage, MaxKafkaChanSize)
	errorChan := make(chan error, 1)

	// Start the kafka consumer
	go kafkaConsumer.ConsumeKafka(ctx, blockMsgsChan, txMsgsChan, errorMsgsChan, errorChan)

	// Start realtime loop
	go realtimeLoop(ctx, realtimeCache)

	for {
		select {
		case <-ctx.Done():
			return
		case finishEntry := <-finishChan:
			if finishEntry.Height < realtimeCache.GetExecutionHeight() {
				// Chain rollback. Reset realtime cache
				resetFlag.Store(true)
				log.Debug(fmt.Sprintf("[Realtime] Chain rollback detected, resetting realtime cache. finishHeight: %d", finishEntry.Height))
			}
			realtimeCache.UpdateExecution(finishEntry)
			log.Debug(fmt.Sprintf("[Realtime] Received finish signal from execution. finishHeight: %d", finishEntry.Height))
		case blockMsg := <-blockMsgsChan:
			if err := blockMsg.Validate(realtimeCache.GetExecutionHeight()); err != nil {
				log.Error(fmt.Sprintf("[Realtime] Failed to consume block message from kafka. error: %v", err))
				continue
			}
			kafkaCache.BlockMsgCache.Add(&blockMsg)
			if subService != nil {
				// Publish block to subscriptions
				subService.BroadcastNewMsg(&blockMsg, nil)
			}
			log.Debug(fmt.Sprintf("[Realtime] Received block message. blockNum: %d", blockMsg.Header.Number))
		case txMsg := <-txMsgsChan:
			if err := txMsg.Validate(); err != nil {
				log.Error(fmt.Sprintf("[Realtime] Failed to consume transaction message from kafka. error: %v", err))
				continue
			}
			if txMsg.BlockNumber <= realtimeCache.GetExecutionHeight() {
				// Ignore txs from previous blocks
				log.Debug(fmt.Sprintf("[Realtime] Ignoring transaction message from previous block. blockNum: %d", txMsg.BlockNumber))
				continue
			}
			kafkaCache.TxMsgCache.Add(&txMsg)
			if subService != nil {
				// Publish tx to subscriptions
				subService.BroadcastNewMsg(nil, &txMsg)
			}
			log.Debug(fmt.Sprintf("[Realtime] Received transaction message. blockNum: %d", txMsg.BlockNumber))
		case errorTriggerMsg := <-errorMsgsChan:
			resetFlag.Store(true)
			triggerHeight := errorTriggerMsg.BlockNumber
			log.Debug(fmt.Sprintf("[Realtime] Received error trigger message, flushing realtime cache. triggerHeight: %d", triggerHeight))
		case err := <-errorChan:
			errorFlag.Store(true)
			log.Error(fmt.Sprintf("[Realtime] Kafka consumer failed. error: %v", err))
			return
		}
	}
}

func realtimeLoop(ctx context.Context, realtimeCache *cache.RealtimeCache) {
	log.Info("[Realtime] Starting realtime loop")
	for {
		select {
		case <-ctx.Done():
			log.Debug("[Realtime] context done, stopping realtime loop")
			return
		default:
		}

		startTime := time.Now()

		// Check for kafka error
		if errorFlag.Load() {
			realtimeCache.ReadyFlag.Store(false)
			log.Error("[Realtime] Kafka error, stopping realtime loop")
			return
		}

		// Check for reset trigger
		if resetFlag.Load() {
			resetRealtimeCache(realtimeCache)
			continue
		}

		// Check if realtime cache is ready
		if !realtimeCache.ReadyFlag.Load() {
			if ok := tryInitRealtimeCache(realtimeCache); !ok {
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
			log.Error(fmt.Sprintf("[Realtime] Execution height is ahead of cache confirm height. pendingHeight: %d, lastExecutionHeight: %d", pendingHeight, lastExecutionHeight))
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
					log.Error(fmt.Sprintf("[Realtime] Failed to apply block msg and tx msgs. error: %v, nextHeight: %d", err, nextHeight))
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
			log.Error(fmt.Sprintf("[Realtime] Handle pending blocks failed. error: %v", err))
		}

		duration := time.Since(startTime)
		if duration < MinRealtimeLoopWaitTime {
			time.Sleep(MinRealtimeLoopWaitTime - duration)
		}
	}
}

// tryInitRealtimeCache checks if the realtime cache can be initialized by comparing
// the current execution height with the lowest kafka cache height.
func tryInitRealtimeCache(realtimeCache *cache.RealtimeCache) bool {
	log.Debug("[Realtime] Trying to initialize realtime cache")
	executionHeight := realtimeCache.GetExecutionHeight()
	lowestKafkaHeight := kafkaCache.GetLowestBlockHeight()
	if executionHeight == 0 || lowestKafkaHeight == 0 {
		// No kafka message or rpc execution. Skip init
		log.Debug(fmt.Sprintf("[Realtime] Init realtime cache failed, no kafka message or rpc execution. lowestKafkaHeight: %d, executionHeight: %d", lowestKafkaHeight, executionHeight))
		return false
	}

	if lowestKafkaHeight > executionHeight {
		// The current execution height is behind kafka cache height. We will wait for the execution
		// height to catch up to kafka cache height before re-initializing the state cache.
		log.Info(fmt.Sprintf("[Realtime] Init realtime cache failed, waiting for execution height to catch up to kafka cache height. lowestKafkaHeight: %d, executionHeight: %d", lowestKafkaHeight, executionHeight))
		return false
	}

	realtimeCache.Clear()
	err := realtimeCache.TryInitStateCache(executionHeight)
	if err != nil {
		log.Error(fmt.Sprintf("[Realtime] Failed to initialize state cache. error: %v", err))
		return false
	}

	// Flush all kafka data less than or equal to state cache height
	kafkaCache.Flush(executionHeight)
	realtimeCache.ReadyFlag.Store(true)
	log.Info(fmt.Sprintf("[Realtime] Realtime cache initialized. executionHeight: %d", executionHeight))

	return true
}

// resetRealtimeCache clears the realtime cache and resets the state flags
func resetRealtimeCache(realtimeCache *cache.RealtimeCache) {
	// Reset and clear realtime cache
	log.Debug("[Realtime] Resetting realtime cache")
	realtimeCache.ReadyFlag.Store(false)
	realtimeCache.Clear()

	resetFlag.Store(false)
}
