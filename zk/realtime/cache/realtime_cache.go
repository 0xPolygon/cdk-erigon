package cache

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/ledgerwatch/erigon-lib/kv"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	"github.com/ledgerwatch/log/v3"
)

const (
	// Kafka tx message cache size
	DefaultTxMsgSliceSize = 100

	DefaultPendingBlockSize = 100

	// Stateless cache sizes
	DefaultStatelessBlockCacheSize = 100
	DefaultStatelessTxCacheSize    = 1000

	// State cache size
	DefaultGlobalStateCacheSize  = 1_000_000
	DefaultPendingStateCacheSize = 1_000

	// Sync threshold config
	PendingBlocksCacheSizeThreshold = 20
)

type PendingBlockContext struct {
	// blockNum is the block number of the current pending block
	blockNum uint64
	// nextTxIndex is the next tx index to be processed in the current pending block
	nextTxIndex uint
	// txCount is the total tx count to close the current pending block. Set txCount to -1 to indicate the next block header has not been received yet.
	txCount int64
	// pendingTxs is the queue of pending txs to be processed in the current pending block
	pendingTxs *realtimeTypes.OrderedList[*kafkaTypes.TransactionMessage]
	// pendingStateCache is the pending state cache for the current pending block
	pendingStateCache *PendingStateCache
}

// String returns a formatted string representation of the PendingBlockContext
func (context *PendingBlockContext) String() string {
	pendingTxsInfo := "[]"
	if context.pendingTxs.Size() > 0 {
		txHashes := make([]string, 0, context.pendingTxs.Size())
		for _, txMsg := range context.pendingTxs.Items() {
			txHashes = append(txHashes, fmt.Sprintf("{ hash: %s, txIndex: %d }", txMsg.Hash, txMsg.Receipt.TransactionIndex))
		}
		pendingTxsInfo = fmt.Sprintf("[%s]", strings.Join(txHashes, ", "))
	}

	return fmt.Sprintf("{ blockNum: %d, nextTxIndex: %d, txCount: %d, pendingTxs: %s }",
		context.blockNum, context.nextTxIndex, context.txCount, pendingTxsInfo)
}

func NewPendingBlockContextList(size int) *realtimeTypes.OrderedList[*PendingBlockContext] {
	return realtimeTypes.NewOrderedList(size, ComparePendingBlockContext)
}

func ComparePendingBlockContext(a, b *PendingBlockContext) int {
	return int(a.blockNum) - int(b.blockNum)
}

type RealtimeCache struct {
	// Caches
	State         *GlobalStateCache
	Stateless     *StatelessCache
	CacheDumpPath string
	ReadyFlag     atomic.Bool

	// highestConfirmHeight is the highest confirmed block height closed from kafka
	highestConfirmHeight atomic.Uint64

	// highestExecutionHeight is the highest executed height on the RPC node
	highestExecutionHeight atomic.Uint64

	// highestPendingHeight is the highest pending block height received from block messages
	highestPendingHeight atomic.Uint64

	// Pending blocks list
	pendingBlocks *realtimeTypes.OrderedList[*PendingBlockContext]
}

func NewRealtimeCache(ctx context.Context, db kv.RoDB, cacheDumpPath string) (*RealtimeCache, error) {
	stateCache, err := NewGlobalStateCache(ctx, db, DefaultGlobalStateCacheSize)
	if err != nil {
		return nil, err
	}

	return &RealtimeCache{
		State:                  stateCache,
		Stateless:              NewStatelessCache(DefaultStatelessBlockCacheSize, DefaultStatelessTxCacheSize),
		CacheDumpPath:          cacheDumpPath,
		ReadyFlag:              atomic.Bool{},
		highestConfirmHeight:   atomic.Uint64{},
		highestExecutionHeight: atomic.Uint64{},
		highestPendingHeight:   atomic.Uint64{},
		pendingBlocks:          NewPendingBlockContextList(DefaultPendingBlockSize),
	}, nil
}

func (cache *RealtimeCache) Clear() {
	// Clear caches
	cache.Stateless.Clear()
	cache.State.Clear()

	cache.highestConfirmHeight.Store(0)
	cache.highestPendingHeight.Store(0)
	cache.pendingBlocks.Clear()
}

func (cache *RealtimeCache) GetHighestConfirmHeight() uint64 {
	return cache.highestConfirmHeight.Load()
}

func (cache *RealtimeCache) PutHighestConfirmHeight(blockNum uint64) {
	if blockNum > cache.highestConfirmHeight.Load() {
		cache.highestConfirmHeight.Store(blockNum)
	}
}

func (cache *RealtimeCache) GetExecutionHeight() uint64 {
	return cache.highestExecutionHeight.Load()
}

func (cache *RealtimeCache) UpdateExecution(finishEntry realtimeTypes.FinishedEntry) {
	if finishEntry.Height > cache.highestExecutionHeight.Load() {
		cache.highestExecutionHeight.Store(finishEntry.Height)
	}
}

func (cache *RealtimeCache) GetPendingHeight() uint64 {
	if cache.GetHighestPendingHeight() == 0 {
		return 0
	}
	return cache.GetHighestConfirmHeight() + 1
}

func (cache *RealtimeCache) GetHighestPendingHeight() uint64 {
	return cache.highestPendingHeight.Load()
}

func (cache *RealtimeCache) PutHighestPendingHeight(blockNum uint64) {
	if blockNum > cache.highestPendingHeight.Load() {
		cache.highestPendingHeight.Store(blockNum)
	}
}

func (cache *RealtimeCache) GetPendingStateCache(blockNum uint64) *PendingStateCache {
	if cache.pendingBlocks.Size() == 0 {
		return nil
	}

	for _, context := range cache.pendingBlocks.Items() {
		if context.blockNum == blockNum {
			return context.pendingStateCache
		}
	}
	return nil
}

func (cache *RealtimeCache) TryInitStateCache(executionHeight uint64) error {
	err := cache.State.TryInitCache(executionHeight)
	if err != nil {
		return err
	}

	cache.PutHighestConfirmHeight(executionHeight)
	return nil
}

func (cache *RealtimeCache) TryApplyNewBlockMsg(blockNum uint64, blockMsg *realtimeTypes.BlockInfo) error {
	cache.Stateless.PutNewHeader(blockNum, blockMsg)
	if err := cache.tryCreateNewPendingBlockContext(blockNum); err != nil {
		return err
	}
	return nil
}

func (cache *RealtimeCache) TryCloseBlockFromConfirmedBlockMsg(blockNum uint64, blockMsg *realtimeTypes.BlockInfo) (bool, error) {
	var pendingContext *PendingBlockContext
	for _, context := range cache.pendingBlocks.Items() {
		if context.blockNum == blockNum {
			pendingContext = context
			break
		}
		if context.blockNum > blockNum {
			// Next block header must be received first before previous block can be closed
			return false, fmt.Errorf("prev block %d is not in pending blocks", blockNum)
		}
	}
	if pendingContext == nil {
		return false, nil
	}

	// Update stateless cache
	cache.Stateless.PutConfirmedHeader(blockNum, blockMsg)

	// Update pending block context
	pendingContext.txCount = blockMsg.TxCount
	cache.tryCloseBlock(pendingContext)

	return true, nil
}

func (cache *RealtimeCache) HandlePendingBlocks(kafkaCache *KafkaCache) error {
	// Pending blocks must be handled in order
	for _, context := range cache.pendingBlocks.Items() {
		nextHeight := cache.GetHighestConfirmHeight() + 1
		if nextHeight != context.blockNum {
			break
		}

		txMsgs := kafkaCache.TxMsgCache.Pop(context.blockNum)
		err := cache.tryApplyBlockTxMsgs(context, txMsgs)
		if err != nil {
			return err
		}
	}
	return nil
}

func (cache *RealtimeCache) tryApplyBlockTxMsgs(blockContext *PendingBlockContext, sortedTxMsgs []*kafkaTypes.TransactionMessage) error {
	// Add to pending queue
	for _, txMsg := range sortedTxMsgs {
		blockContext.pendingTxs.Add(txMsg)
	}
	blockContext.pendingTxs.Sort()

	// Process pending txs
	processed := 0
	for _, txMsg := range blockContext.pendingTxs.Items() {
		txIndex := txMsg.Receipt.TransactionIndex
		if txIndex < blockContext.nextTxIndex {
			// Duplicate tx received. Skip
			processed++
			continue
		} else if txIndex > blockContext.nextTxIndex {
			break
		}

		// Tx is the next tx to be processed. Apply tx msg
		tx, _, err := txMsg.GetTransaction()
		if err != nil {
			return fmt.Errorf("failed to get tx. Block number: %d, tx index: %d, error: %v", txMsg.BlockNumber, blockContext.nextTxIndex, err)
		}
		receipt, err := txMsg.GetReceipt()
		if err != nil {
			return fmt.Errorf("failed to get tx receipt. Block number: %d, tx index: %d, error: %v", txMsg.BlockNumber, blockContext.nextTxIndex, err)
		}
		innerTxs, err := txMsg.GetInnerTxs()
		if err != nil {
			return fmt.Errorf("failed to get inner txs. Block number: %d, tx index: %d, error: %v", txMsg.BlockNumber, blockContext.nextTxIndex, err)
		}
		cache.Stateless.PutTxInfo(blockContext.blockNum, txMsg.Hash, tx, receipt, innerTxs)
		blockContext.pendingStateCache.ApplyChangeset(txMsg.Changeset, txMsg.BlockNumber, txMsg.Receipt.TransactionIndex)
		blockContext.nextTxIndex++
		processed++
	}

	newPendingTxs := blockContext.pendingTxs.Items()[processed:]
	blockContext.pendingTxs.SetItems(newPendingTxs)
	blockContext.pendingTxs.Sort()

	// Try to close block
	cache.tryCloseBlock(blockContext)

	return nil
}

func (cache *RealtimeCache) tryCreateNewPendingBlockContext(blockNum uint64) error {
	if cache.pendingBlocks.Size() > PendingBlocksCacheSizeThreshold {
		// Find the pending block context that is blocking, and log out the block context
		var blockedContext *PendingBlockContext
		nextHeight := cache.GetHighestConfirmHeight() + 1
		for _, context := range cache.pendingBlocks.Items() {
			if context.blockNum == nextHeight {
				blockedContext = context
				break
			}
		}
		if blockedContext != nil {
			return fmt.Errorf("too many pending blocks, block took too long to close. Pending blocks queue size: %d, block context: %s", cache.pendingBlocks.Size(), blockedContext.String())
		}
		// We should never reach here
		return fmt.Errorf("too many pending blocks, realtime cache state corrupted. Pending blocks queue size: %d", cache.pendingBlocks.Size())
	}

	// Create new pending block context
	newPendingBlockContext := &PendingBlockContext{
		blockNum:          blockNum,
		nextTxIndex:       0,
		pendingTxs:        realtimeTypes.NewOrderedList(DefaultTxMsgSliceSize, CompareTransactionMessages),
		txCount:           -1,
		pendingStateCache: NewPendingStateCache(cache.State, DefaultPendingStateCacheSize),
	}
	cache.pendingBlocks.Add(newPendingBlockContext)
	cache.pendingBlocks.Sort()
	cache.PutHighestPendingHeight(blockNum)
	log.Debug(fmt.Sprintf("[Realtime] Opened block %d, pending blocks queue size: %d", blockNum, cache.pendingBlocks.Size()))

	return nil
}

func (cache *RealtimeCache) tryCloseBlock(pendingBlockContext *PendingBlockContext) {
	if pendingBlockContext == nil {
		return
	}

	if pendingBlockContext.txCount < 0 {
		// Header not received yet. Skip close
		return
	}

	if pendingBlockContext.pendingTxs.Size() > 0 || pendingBlockContext.txCount != int64(pendingBlockContext.nextTxIndex) {
		// Cannot close block yet, missing txs
		return
	}

	nextHeight := cache.GetHighestConfirmHeight() + 1
	if pendingBlockContext.blockNum != nextHeight {
		// Block must be closed in order
		return
	}

	// Close block
	items := cache.pendingBlocks.Items()
	for i, item := range items {
		if item.blockNum == pendingBlockContext.blockNum {
			// Blocks have to be closed in order of the previous highest confirm height. Thus it is safe to
			// remove older pending block entries as we can assume they are stale and will never be closed.
			cache.pendingBlocks.SetItems(items[i+1:])
			cache.pendingBlocks.Sort()
			break
		}
	}
	cache.PutHighestConfirmHeight(pendingBlockContext.blockNum)
	cache.State.FlushState(pendingBlockContext.pendingStateCache.cache)
	log.Info(fmt.Sprintf("[Realtime] Closed block %d, pending blocks queue size: %d", pendingBlockContext.blockNum, cache.pendingBlocks.Size()))
}

// -------------- Debug operations --------------
func (cache *RealtimeCache) DebugDumpToFile() error {
	err := cache.State.DebugDumpToFile(cache.CacheDumpPath)
	if err != nil {
		return err
	}
	return cache.Stateless.DebugDumpToFile(cache.CacheDumpPath)
}
