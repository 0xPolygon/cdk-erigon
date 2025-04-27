package smt

import (
	"errors"
	"sync"

	"github.com/ledgerwatch/erigon/smt/pkg/utils"
)

var flushSmtCachePeriod = uint64(100)

type SmtCacheSave struct {
	SmtData     map[string]map[string][]byte
	BlockHeight uint64
}

type SmtCache struct {
	PushedHeap       *Uint64MinHeap
	LastPushedHeight uint64
	ConfirmedHeap    *Uint64MinHeap

	SmtCacheSnapshotList *SmtCacheList // used by seq, witness verify and truncated thread, need lock
	SmtCacheSnapshotLock sync.RWMutex  // Added lock for SmtCacheSnapshotList

	PreBatchSnapshotImage         map[string]map[string][]byte // used by seq, witness verify and truncated thread, need lock
	PreBatchImageLastUpdateHeight uint64
	PreBatchImageLock             sync.RWMutex // Added lock for PreBatchSnapshotImage

	LastRecordBlockHeight         uint64
	CurrentBatchBlockSnapshotList *SmtCacheList // used by seq and witness verify thread, need lock
	CurrentBatchSnapshotLock      sync.RWMutex  // Added lock for CurrentBatchBlockSnapshotList

	SmtCacheDataCh    chan SmtCacheSave
	ToPushedSmtCache  map[string]map[string][]byte // only used by seq thread, no need lock protect
	LongLivedSmtCache map[string]map[string][]byte // only used by seq thread, no need lock protect
	LastResetHeight   uint64
}

func CreateNewSmtCache() *SmtCache {
	return &SmtCache{
		PushedHeap:       NewUint64MinHeap(),
		ConfirmedHeap:    NewUint64MinHeap(),
		LastPushedHeight: 0,

		SmtCacheDataCh:       make(chan SmtCacheSave, 1000),
		SmtCacheSnapshotList: NewSmtCacheList(),
		ToPushedSmtCache:     make(map[string]map[string][]byte),
		LongLivedSmtCache:    make(map[string]map[string][]byte),
		LastResetHeight:      0,

		LastRecordBlockHeight:         0,
		CurrentBatchBlockSnapshotList: NewSmtCacheList(),
		PreBatchSnapshotImage:         make(map[string]map[string][]byte),
		PreBatchImageLastUpdateHeight: 0,
	}
}

func (cache *SmtCache) TruncateSmtCacheList(blockHeight uint64) {
	cache.ConfirmedHeap.ThreadSafePush(blockHeight)

	truncateHeight := uint64(0)
	for {
		confirmHeight, _ := cache.ConfirmedHeap.ThreadSafeTop()
		pushedHeight, _ := cache.PushedHeap.ThreadSafeTop()
		if confirmHeight == pushedHeight && confirmHeight > 0 {
			cache.ConfirmedHeap.ThreadSafePop()
			cache.PushedHeap.ThreadSafePop()

			cache.SmtCacheSnapshotLock.Lock()
			cache.SmtCacheSnapshotList.cascadeDeleteCache(confirmHeight)
			cache.SmtCacheSnapshotLock.Unlock()
			truncateHeight = confirmHeight
		} else {
			break
		}
	}

	if truncateHeight > 0 {
		//if truncateHeight-cache.PreBatchImageLastUpdateHeight > flushSmtCachePeriod*2 {
		cache.SmtCacheSnapshotLock.RLock()
		tmpSmtCache, _ := cache.SmtCacheSnapshotList.getAllCacheShapshot()
		cache.SmtCacheSnapshotLock.RUnlock()

		if tmpSmtCache == nil {
			tmpSmtCache = map[string]map[string][]byte{}
		}
		cache.PreBatchImageLock.Lock()
		cache.PreBatchSnapshotImage = tmpSmtCache
		cache.PreBatchImageLastUpdateHeight = truncateHeight
		cache.PreBatchImageLock.Unlock()
		//}
	}
}

func (cache *SmtCache) GetSmtCache() map[string]map[string][]byte {
	return cache.LongLivedSmtCache
}

func (cache *SmtCache) CascadeGetCurrentBatchSnapshotCache(blockNumber uint64) map[string]map[string][]byte {
	cache.CurrentBatchSnapshotLock.RLock()
	cacheData, ok := cache.CurrentBatchBlockSnapshotList.cascadeGetCacheShapshot(blockNumber)
	cache.CurrentBatchSnapshotLock.RUnlock()

	if !ok {
		cache.SmtCacheSnapshotLock.RLock()
		cacheData, _ = cache.SmtCacheSnapshotList.cascadeGetCacheShapshot(blockNumber)
		cache.SmtCacheSnapshotLock.RUnlock()
		return cacheData
	}

	cache.PreBatchImageLock.RLock()
	result := make(map[string]map[string][]byte, len(cache.PreBatchSnapshotImage))

	// Use a wait group to synchronize goroutines
	var mu sync.Mutex
	var wg sync.WaitGroup

	// First process PreBatchSnapshotImage
	for table, bucket := range cache.PreBatchSnapshotImage {
		wg.Add(1)
		go func(table string, bucket map[string][]byte) {
			defer wg.Done()

			// Create the inner map with appropriate size
			innerMap := make(map[string][]byte, len(bucket)*2)
			for k, v := range bucket {
				if v != nil && len(v) > 0 {
					innerMap[k] = v
				}
			}

			mu.Lock()
			result[table] = innerMap
			mu.Unlock()
		}(table, bucket)
	}
	wg.Wait() // Wait for all PreBatchSnapshotImage copies to complete
	cache.PreBatchImageLock.RUnlock()

	// Then process cacheData
	for table, bucket := range cacheData {
		wg.Add(1)
		go func(table string, bucket map[string][]byte) {
			defer wg.Done()

			mu.Lock()
			innerMap, exists := result[table]
			if !exists {
				innerMap = make(map[string][]byte, len(bucket))
				result[table] = innerMap
			}
			mu.Unlock()

			var tableMu sync.Mutex
			tableMu.Lock()
			for k, v := range bucket {
				if v != nil && len(v) > 0 {
					innerMap[k] = v
				} else {
					delete(innerMap, k) // the latest value is empty, means that this key has been deleted from cache
				}
			}
			tableMu.Unlock()
		}(table, bucket)
	}
	wg.Wait()

	return result
}

func (cache *SmtCache) SetSmtCache(blockNumber uint64, blockCache map[string]map[string][]byte) {
	cache.SmtCacheSnapshotLock.Lock()
	cache.SmtCacheSnapshotList.Push(blockNumber, blockCache)
	cache.SmtCacheSnapshotLock.Unlock()

	cache.CurrentBatchSnapshotLock.Lock()
	cache.CurrentBatchBlockSnapshotList.Push(blockNumber, blockCache)
	cache.CurrentBatchSnapshotLock.Unlock()

	var mu sync.Mutex
	var wg sync.WaitGroup
	for table, bucket := range blockCache {
		wg.Add(1)
		go func(table string, bucket map[string][]byte) {
			defer wg.Done()

			mu.Lock()
			innerMap, exists := cache.LongLivedSmtCache[table]
			if !exists {
				innerMap = make(map[string][]byte, len(bucket))
				cache.LongLivedSmtCache[table] = innerMap
			}
			mu.Unlock()

			var tableMu sync.Mutex
			tableMu.Lock()
			for k, v := range bucket {
				innerMap[k] = v
			}
			tableMu.Unlock()
		}(table, bucket)
	}
	wg.Wait()

	cache.LastRecordBlockHeight = blockNumber

}

func (cache *SmtCache) FlushSmtCache(batchPush, grace bool) error {
	// 1. merge current batch cache image to PreBatchSnapshotImage and ToPushedSmtCache
	cache.CurrentBatchSnapshotLock.RLock()
	currentBatchImage, _ := cache.CurrentBatchBlockSnapshotList.getAllCacheShapshot()
	cache.CurrentBatchSnapshotLock.RUnlock()

	cache.PreBatchImageLock.Lock()
	for table, bucket := range currentBatchImage {
		if _, exists := cache.ToPushedSmtCache[table]; !exists {
			cache.ToPushedSmtCache[table] = make(map[string][]byte, len(bucket))
		}

		if _, exists := cache.PreBatchSnapshotImage[table]; !exists {
			cache.PreBatchSnapshotImage[table] = make(map[string][]byte, len(bucket))
		}

		for key, value := range bucket {
			cache.ToPushedSmtCache[table][key] = value
			cache.PreBatchSnapshotImage[table][key] = value // replace the old value with the latest one
		}
	}
	cache.PreBatchImageLock.Unlock()

	// 2. clean current batch cache image
	cache.CurrentBatchSnapshotLock.Lock()
	cache.CurrentBatchBlockSnapshotList = NewSmtCacheList()
	cache.CurrentBatchSnapshotLock.Unlock()

	height, err := utils.ConvertBytesToUint64(cache.ToPushedSmtCache["HermezSmtStats"]["lastHeight"])
	if err != nil {
		return err
	}

	if height-cache.LastResetHeight > 10*flushSmtCachePeriod {
		cache.SmtCacheSnapshotLock.RLock()
		tmpSmtCache, _ := cache.SmtCacheSnapshotList.getAllCacheShapshot()
		cache.SmtCacheSnapshotLock.RUnlock()

		if tmpSmtCache == nil {
			tmpSmtCache = map[string]map[string][]byte{}
		}

		// Reset LongLivedSmtCache, prevent excessive memory usage.
		cache.LongLivedSmtCache = tmpSmtCache
		cache.LastResetHeight = height
	}

	if batchPush && (height-cache.LastPushedHeight < flushSmtCachePeriod) && !grace {
		return nil
	}

	data := SmtCacheSave{
		cache.ToPushedSmtCache,
		height,
	}

	select {
	case cache.SmtCacheDataCh <- data:
		cache.PushedHeap.ThreadSafePush(height)
		cache.LastPushedHeight = height

		cache.ToPushedSmtCache = map[string]map[string][]byte{}
		return nil
	default:
		return errors.New("failed to flush: channel is full or no receiver")
	}
}

func (cache *SmtCache) ResetCurrentBatch(resetBlockHeight uint64) {
	if resetBlockHeight > cache.LastRecordBlockHeight {
		return
	}
	deleteBlockList := make([]uint64, resetBlockHeight-cache.LastRecordBlockHeight+1)

	for i := resetBlockHeight; i <= cache.LastRecordBlockHeight; i++ {
		deleteBlockList = append(deleteBlockList, i)
	}

	// 1. clean CurrentBatchBlockSnapshotList
	cache.CurrentBatchSnapshotLock.Lock()
	//currentBatchBlockList := cache.CurrentBatchBlockSnapshotList.getBlockList()
	//cache.CurrentBatchBlockSnapshotList = NewSmtCacheList()
	for _, blockNumber := range deleteBlockList {
		cache.CurrentBatchBlockSnapshotList.deleteTargetCache(blockNumber)
	}
	cache.CurrentBatchSnapshotLock.Unlock()

	// 2. delete all block cache in current batch
	cache.SmtCacheSnapshotLock.Lock()
	for _, blockNumber := range deleteBlockList {
		cache.SmtCacheSnapshotList.deleteTargetCache(blockNumber)
	}

	// 3. reset longLive cache for it has already been polluted
	tmpSmtCache, _ := cache.SmtCacheSnapshotList.getAllCacheShapshot()
	cache.SmtCacheSnapshotLock.Unlock()

	if tmpSmtCache == nil {
		tmpSmtCache = map[string]map[string][]byte{}
	}

	cache.LongLivedSmtCache = tmpSmtCache
	cache.LastResetHeight = resetBlockHeight - 1
}
