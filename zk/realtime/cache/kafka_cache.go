package cache

import (
	"fmt"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	kafkaTypes "github.com/ledgerwatch/erigon/zk/realtime/kafka/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	"github.com/ledgerwatch/log/v3"
)

// -------------- Kafka Cache --------------
type KafkaCache struct {
	BlockMsgCache *BlockMessageCache
	TxMsgCache    *TransactionMessageCache
}

func NewKafkaCache(maxCacheSize int) (*KafkaCache, error) {
	blockCache, err := NewBlockMessageCache(maxCacheSize)
	if err != nil {
		return nil, err
	}
	txCache, err := NewTransactionMessageCache(maxCacheSize)
	if err != nil {
		return nil, err
	}
	return &KafkaCache{
		BlockMsgCache: blockCache,
		TxMsgCache:    txCache,
	}, nil
}

func (cache *KafkaCache) Clear() {
	cache.BlockMsgCache.Clear()
	cache.TxMsgCache.Clear()
}

func (cache *KafkaCache) Flush(blockNumber uint64) {
	if blockNumber == 0 {
		return
	}

	cache.BlockMsgCache.Flush(blockNumber)
	cache.TxMsgCache.Flush(blockNumber)
}

func (cache *KafkaCache) GetLowestBlockHeight() uint64 {
	return cache.BlockMsgCache.GetLowestBlockHeight()
}

// -------------- Block Message Cache --------------
type BlockMessageCache struct {
	mu    sync.RWMutex
	cache *lru.Cache[uint64, *kafkaTypes.BlockMessage]
}

func NewBlockMessageCache(maxCacheSize int) (*BlockMessageCache, error) {
	cache, err := lru.New[uint64, *kafkaTypes.BlockMessage](maxCacheSize)
	if err != nil {
		return nil, err
	}

	return &BlockMessageCache{
		cache: cache,
	}, nil
}

func (cache *BlockMessageCache) Add(blockMsg *kafkaTypes.BlockMessage) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cache.cache.Add(blockMsg.Header.Number.Uint64(), blockMsg)
	log.Debug(fmt.Sprintf("[Realtime] Added block message to kafka cache for block number %d", blockMsg.Header.Number.Uint64()))
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

// GetAndFlush pops the block message for the given block number
func (cache *BlockMessageCache) Pop(blockNumber uint64) (*kafkaTypes.BlockMessage, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	blockMsg, ok := cache.cache.Get(blockNumber)
	if ok {
		cache.cache.Remove(blockNumber)
	}
	return blockMsg, ok
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
	log.Debug(fmt.Sprintf("[Realtime] Added tx message to kafka cache for block number %d with txhash: %x", txMsg.BlockNumber, txMsg.Hash))
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
