package cache

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/zk/metrics"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	"github.com/ledgerwatch/erigon/zk/utils"
	"github.com/ledgerwatch/log/v3"
)

const (
	// Kafka tx message cache size
	DefaultTxMsgSliceSize = 100

	DefaultPendingBlockSize = 100

	// Stateless cache sizes
	DefaultStatelessBlockCacheSize = 100
	DefaultStatelessTxCacheSize    = 1_000 * DefaultStatelessBlockCacheSize

	// State cache size
	DefaultStateBlockCacheSize = 1_000

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
	// startBlockChangeset is the changset to be applied at the start of the current pending block
	startBlockChangeset *realtimeTypes.Changeset
	// endBlockChangeset is the changset to be applied at the end of the current pending block
	endBlockChangeset *realtimeTypes.Changeset
	// pendingTxs is the queue of pending txs to be processed in the current pending block
	pendingTxs *realtimeTypes.OrderedList[*kafkaTypes.TransactionMessage]
	// pendingStateCache is the pending state cache for the current pending block
	blockStateCache *BlockStateCache
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
	// Chaindb
	ctx       context.Context
	db        kv.RoDB
	chainName string

	// Caches
	State     *StateCache
	Stateless *StatelessCache

	ReadyFlag       atomic.Bool
	CacheDumpPath   string
	HeightThreshold uint64

	// highestConfirmHeight is the highest confirmed block height closed from kafka
	highestConfirmHeight atomic.Uint64

	// highestExecutionHeight is the highest executed height on the RPC node
	highestExecutionHeight atomic.Uint64

	// highestPendingHeight is the highest pending block height received from block messages
	highestPendingHeight atomic.Uint64

	// Pending blocks list
	pendingBlocks *realtimeTypes.OrderedList[*PendingBlockContext]
}

func NewRealtimeCache(ctx context.Context, db kv.RoDB, tx kv.Tx, chainName string, cacheDumpPath string, heightThreshold uint64) (*RealtimeCache, error) {
	return &RealtimeCache{
		ctx:                    ctx,
		db:                     db,
		chainName:              chainName,
		State:                  NewStateCache(DefaultStateBlockCacheSize),
		Stateless:              NewStatelessCache(DefaultStatelessBlockCacheSize, DefaultStatelessTxCacheSize),
		ReadyFlag:              atomic.Bool{},
		CacheDumpPath:          cacheDumpPath,
		HeightThreshold:        heightThreshold,
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
	for _, context := range cache.pendingBlocks.Items() {
		if context.blockStateCache != nil {
			context.blockStateCache.Clear()
		}
	}
	cache.pendingBlocks.Clear()
}

func (cache *RealtimeCache) GetHighestConfirmHeight() uint64 {
	return cache.highestConfirmHeight.Load()
}

func (cache *RealtimeCache) PutHighestConfirmHeight(blockNum uint64) {
	if blockNum > cache.highestConfirmHeight.Load() {
		cache.highestConfirmHeight.Store(blockNum)
		metrics.SetRealtimeBlockHeight(float64(blockNum))
	}
}

func (cache *RealtimeCache) GetExecutionHeight() uint64 {
	return cache.highestExecutionHeight.Load()
}

func (cache *RealtimeCache) UpdateExecution(finishEntry realtimeTypes.FinishedEntry) error {
	if finishEntry.Height > cache.highestExecutionHeight.Load() {
		cache.highestExecutionHeight.Store(finishEntry.Height)

		// Clear cache
		if finishEntry.Height > cache.HeightThreshold {
			deleteHeight := finishEntry.Height - cache.HeightThreshold
			cache.Stateless.DeleteBlock(deleteHeight)
			return cache.State.FlushBlock(deleteHeight)
		}
	}
	return nil
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
		metrics.SetRealtimePendingBlockHeight(float64(blockNum))
	}
}

func (cache *RealtimeCache) TryInitStateCache(executionHeight uint64) error {
	err := cache.State.TryInitCache(executionHeight)
	if err != nil {
		return err
	}

	cache.PutHighestConfirmHeight(executionHeight)
	return nil
}

func (cache *RealtimeCache) TryApplyNewBlockMsg(blockNum uint64, startBlockMsg *realtimeTypes.BlockInfo) error {
	if err := cache.tryCreateNewPendingBlockContext(blockNum, startBlockMsg); err != nil {
		return err
	}
	cache.Stateless.PutNewBlockInfo(blockNum, startBlockMsg)
	return nil
}

func (cache *RealtimeCache) TryCloseBlockFromConfirmedBlockMsg(blockNum uint64, blockMsg *realtimeTypes.BlockInfo) error {
	var pendingContext *PendingBlockContext
	for _, context := range cache.pendingBlocks.Items() {
		if context.blockNum == blockNum {
			pendingContext = context
			break
		}
		if context.blockNum > blockNum {
			return fmt.Errorf("block %d is not in pending blocks", blockNum)
		}
	}
	if pendingContext == nil {
		return fmt.Errorf("block %d is not in pending blocks", blockNum)
	}

	// Update stateless cache
	cache.Stateless.PutConfirmedBlockInfo(blockNum, blockMsg)

	// Update pending block context
	pendingContext.txCount = blockMsg.TxCount
	pendingContext.endBlockChangeset = blockMsg.Changeset
	return cache.tryCloseBlock(pendingContext)
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
	// Apply start block changeset
	if blockContext.startBlockChangeset != nil && blockContext.nextTxIndex == 0 {
		err := blockContext.blockStateCache.ApplyChangeset(blockContext.startBlockChangeset, blockContext.blockNum)
		if err != nil {
			return fmt.Errorf("failed to apply start block changeset. Block number: %d, error: %v", blockContext.blockNum, err)
		}
		blockContext.startBlockChangeset = nil
	}

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
		blockContext.blockStateCache.ApplyChangeset(txMsg.Changeset, txMsg.BlockNumber)
		blockContext.nextTxIndex++
		processed++
	}

	newPendingTxs := blockContext.pendingTxs.Items()[processed:]
	blockContext.pendingTxs.SetItems(newPendingTxs)
	blockContext.pendingTxs.Sort()

	// Try to close block
	return cache.tryCloseBlock(blockContext)
}

func (cache *RealtimeCache) tryCreateNewPendingBlockContext(blockNum uint64, startBlockMsg *realtimeTypes.BlockInfo) error {
	confirmHeight := cache.GetHighestConfirmHeight()
	if cache.pendingBlocks.Size() > PendingBlocksCacheSizeThreshold {
		// Find the pending block context that is blocking, and log out the block context
		var blockedContext *PendingBlockContext
		nextHeight := confirmHeight + 1
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

	// Create block state cache
	bc := NewBlockStateCache(cache.ctx, cache.db, cache.chainName, blockNum)
	// Get previous block state reader
	prevBlockNum := blockNum - 1
	if prevBlockNum == confirmHeight {
		pbc, err := cache.State.GetConfirmBlockStateCache(prevBlockNum)
		if err != nil {
			pbc = nil
		}

		bc.SetPrevBlockCache(pbc)
		if pbc != nil {
			pbc.SetNextBlockCache(bc)
		}
	} else if prevBlockNum > confirmHeight {
		pbc, err := cache.GetPendingBlockStateCache(prevBlockNum)
		if err != nil || pbc == nil {
			return fmt.Errorf("failed to get prev block state reader from pending cache, prevBlockNum: %d, err: %v", prevBlockNum, err)
		}
		bc.SetPrevBlockCache(pbc)
		pbc.SetNextBlockCache(bc)
	} else {
		return fmt.Errorf("failed to get prev block state reader, block num behind confirm height. prevBlockNum: %d, confirmHeight: %d", prevBlockNum, confirmHeight)
	}

	// Create new pending block context
	newPendingBlockContext := &PendingBlockContext{
		blockNum:            blockNum,
		nextTxIndex:         0,
		pendingTxs:          realtimeTypes.NewOrderedList(DefaultTxMsgSliceSize, CompareTransactionMessages),
		txCount:             -1,
		startBlockChangeset: startBlockMsg.Changeset,
		endBlockChangeset:   nil,
		blockStateCache:     bc,
	}
	cache.pendingBlocks.Add(newPendingBlockContext)
	cache.pendingBlocks.Sort()
	cache.PutHighestPendingHeight(blockNum)
	log.Debug(fmt.Sprintf("[Realtime] Opened block %d, pending blocks queue size: %d", blockNum, cache.pendingBlocks.Size()))

	return nil
}

func (cache *RealtimeCache) tryCloseBlock(pendingBlockContext *PendingBlockContext) error {
	if pendingBlockContext == nil {
		return fmt.Errorf("pending block context is nil")
	}

	if pendingBlockContext.txCount < 0 {
		// confirmed block info is not received yet. Skip close
		return nil
	}

	if pendingBlockContext.pendingTxs.Size() > 0 || pendingBlockContext.txCount != int64(pendingBlockContext.nextTxIndex) {
		// Cannot close block yet, missing txs
		return nil
	}

	nextHeight := cache.GetHighestConfirmHeight() + 1
	if pendingBlockContext.blockNum != nextHeight {
		// Block must be closed in order
		return nil
	}

	// Apply close block changeset
	if pendingBlockContext.endBlockChangeset != nil {
		if err := pendingBlockContext.blockStateCache.ApplyChangeset(pendingBlockContext.endBlockChangeset, pendingBlockContext.blockNum); err != nil {
			log.Error(fmt.Sprintf("[Realtime] Failed to apply closeBlock changeset. Block number: %d, error: %v", pendingBlockContext.blockNum, err))
			return err
		}
		pendingBlockContext.endBlockChangeset = nil
	}

	// Close block
	err := cache.State.AddBlock(pendingBlockContext.blockNum, pendingBlockContext.blockStateCache)
	if err != nil {
		return err
	}
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
	pendingBlockContext.blockStateCache = nil

	cache.PutHighestConfirmHeight(pendingBlockContext.blockNum)
	log.Info(fmt.Sprintf("[Realtime] Closed block %d, pending blocks queue size: %d", pendingBlockContext.blockNum, cache.pendingBlocks.Size()))

	header, _, blockHash, ok := cache.Stateless.GetBlockInfo(pendingBlockContext.blockNum)
	if !ok { // Should never happen
		log.Error(fmt.Sprintf("[Realtime] Block info not found for block number %d", pendingBlockContext.blockNum))
		return nil
	}
	utils.LogTrace(
		"",                               // txhash
		utils.ServiceNameRPC,             // serviceName
		utils.StepRealtimeCloseBlock.ID,  // processId
		utils.StepRealtimeCloseBlock.Key, // processWord
		pendingBlockContext.blockNum,     // blockHeight
		blockHash.String(),               // blockHash
		header.Time,                      // blockTime
		-1,                               // transactionType
	)

	return nil
}

// -------------- Retrieve state readers utiliy operations --------------
func (cache *RealtimeCache) GetPendingBlockStateCache(blockNum uint64) (*BlockStateCache, error) {
	for _, context := range cache.pendingBlocks.Items() {
		if context.blockNum == blockNum {
			return context.blockStateCache, nil
		}
	}
	return nil, fmt.Errorf("blockNum %d is not in the pending blocks", blockNum)
}

func (cache *RealtimeCache) GetPendingStateCache() (state.StateReader, uint64) {
	if cache.pendingBlocks.Size() == 0 {
		return nil, 0
	}
	pendingHeight := cache.GetPendingHeight()
	stateReader, err := cache.GetPendingBlockStateCache(pendingHeight)
	if err != nil {
		return nil, 0
	}
	return stateReader, pendingHeight
}

func (cache *RealtimeCache) GetLatestStateCache() (state.StateReader, uint64) {
	confirmHeight := cache.GetHighestConfirmHeight()
	stateReader, err := cache.State.GetConfirmBlockStateCache(confirmHeight)
	if err != nil {
		return nil, 0
	}
	return stateReader, confirmHeight
}

func (cache *RealtimeCache) GetStateCacheByHeight(blockNum uint64) state.StateReader {
	var reader state.StateReader
	var err error

	confirmHeight := cache.GetHighestConfirmHeight()
	if blockNum > confirmHeight {
		reader, err = cache.GetPendingBlockStateCache(blockNum)
		if err != nil {
			reader = nil
		}
	}
	if reader == nil {
		reader, err = cache.State.GetConfirmBlockStateCache(blockNum)
		if err != nil {
			reader = nil
		}
	}
	return reader
}

// -------------- Debug operations --------------
func (cache *RealtimeCache) DebugDumpToFile() error {
	err := cache.State.DebugDumpToFile(cache.CacheDumpPath)
	if err != nil {
		return err
	}
	return cache.Stateless.DebugDumpToFile(cache.CacheDumpPath)
}
