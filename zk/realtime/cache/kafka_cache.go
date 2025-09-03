package cache

import (
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
)

// -------------- Kafka Cache --------------
type KafkaCache struct {
	NewBlockMsgCache       *BlockMessageCache
	ConfirmedBlockMsgCache *BlockMessageCache
	TxMsgCache             *TransactionMessageCache
}

func NewKafkaCache(maxCacheSize int) (*KafkaCache, error) {
	newBlockCache, err := NewBlockMessageCache(maxCacheSize)
	if err != nil {
		return nil, err
	}
	confirmedBlockCache, err := NewBlockMessageCache(maxCacheSize)
	if err != nil {
		return nil, err
	}
	txCache, err := NewTransactionMessageCache(maxCacheSize)
	if err != nil {
		return nil, err
	}
	return &KafkaCache{
		NewBlockMsgCache:       newBlockCache,
		ConfirmedBlockMsgCache: confirmedBlockCache,
		TxMsgCache:             txCache,
	}, nil
}

func (cache *KafkaCache) Clear() {
	cache.NewBlockMsgCache.Clear()
	cache.ConfirmedBlockMsgCache.Clear()
	cache.TxMsgCache.Clear()
}

func (cache *KafkaCache) Flush(executionHeight uint64) {
	if executionHeight == 0 {
		return
	}

	cache.NewBlockMsgCache.Flush(executionHeight)
	cache.ConfirmedBlockMsgCache.Flush(executionHeight)
	cache.TxMsgCache.Flush(executionHeight)
}

func (cache *KafkaCache) GetLowestNewBlockHeight() uint64 {
	return cache.NewBlockMsgCache.GetLowestBlockHeight()
}

// -------------- Block Message Cache --------------
type BlockMessageCache struct {
	mu    sync.RWMutex
	cache *lru.Cache[uint64, *realtimeTypes.BlockInfo]
}

func NewBlockMessageCache(maxCacheSize int) (*BlockMessageCache, error) {
	cache, err := lru.New[uint64, *realtimeTypes.BlockInfo](maxCacheSize)
	if err != nil {
		return nil, err
	}

	return &BlockMessageCache{
		cache: cache,
	}, nil
}

func (cache *BlockMessageCache) Add(blockMsg *realtimeTypes.BlockInfo) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.cache.Add(blockMsg.Header.Number.Uint64(), blockMsg)
}

func (cache *BlockMessageCache) Clear() {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.cache.Purge()
}

func (cache *BlockMessageCache) Size() int {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	return cache.cache.Len()
}

func (cache *BlockMessageCache) Get(blockNumber uint64) (*realtimeTypes.BlockInfo, bool) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	return cache.cache.Get(blockNumber)
}

func (cache *BlockMessageCache) Flush(blockNumber uint64) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	// Get all keys and remove those <= blockNumber
	for _, k := range cache.cache.Keys() {
		if k <= blockNumber {
			cache.cache.Remove(k)
		}
	}
}

func (cache *BlockMessageCache) GetLowestBlockHeight() uint64 {
	cache.mu.RLock()
	defer cache.mu.RUnlock()

	lowestBlockHeight := uint64(0)
	for _, k := range cache.cache.Keys() {
		if lowestBlockHeight == 0 || k < lowestBlockHeight {
			lowestBlockHeight = k
		}
	}
	return lowestBlockHeight
}

// -------------- Tx Message Cache --------------
type TransactionMessageCache struct {
	mu    sync.RWMutex
	cache *lru.Cache[uint64, *realtimeTypes.OrderedList[*kafkaTypes.TransactionMessage]]
}

func NewTransactionMessageCache(maxCacheSize int) (*TransactionMessageCache, error) {
	cache, err := lru.New[uint64, *realtimeTypes.OrderedList[*kafkaTypes.TransactionMessage]](maxCacheSize)
	if err != nil {
		return nil, err
	}
	return &TransactionMessageCache{
		cache: cache,
	}, nil
}

func (cache *TransactionMessageCache) Add(txMsg *kafkaTypes.TransactionMessage) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	txMsgsList, ok := cache.cache.Get(txMsg.BlockNumber)
	if !ok {
		txMsgsList = realtimeTypes.NewOrderedList(DefaultTxMsgSliceSize, CompareTransactionMessages)
		cache.cache.Add(txMsg.BlockNumber, txMsgsList)
	}
	txMsgsList.Add(txMsg)
	txMsgsList.Sort()
}

func (cache *TransactionMessageCache) Clear() {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.cache.Purge()
}

func (cache *TransactionMessageCache) Pop(blockNumber uint64) []*kafkaTypes.TransactionMessage {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	txMsgsList, ok := cache.cache.Get(blockNumber)
	if !ok {
		return nil
	}

	// Retrieve all tx messages and clear the cache
	txs := make([]*kafkaTypes.TransactionMessage, txMsgsList.Size())
	copy(txs, txMsgsList.Items())
	txMsgsList.Clear()

	return txs
}

func (cache *TransactionMessageCache) Flush(blockNumber uint64) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	for _, k := range cache.cache.Keys() {
		if k <= blockNumber {
			cache.cache.Remove(k)
		}
	}
}

func NewOrderedListOfTransactionMessage(size int) *realtimeTypes.OrderedList[*kafkaTypes.TransactionMessage] {
	return realtimeTypes.NewOrderedList(size, CompareTransactionMessages)
}

func CompareTransactionMessages(a, b *kafkaTypes.TransactionMessage) int {
	if a.BlockNumber == b.BlockNumber {
		if a.Receipt != nil && b.Receipt != nil {
			return int(a.Receipt.TransactionIndex) - int(b.Receipt.TransactionIndex)
		}
		return 0
	}
	return int(a.BlockNumber) - int(b.BlockNumber)
}
