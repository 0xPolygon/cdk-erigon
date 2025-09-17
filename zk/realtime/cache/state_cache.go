package cache

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/crypto"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
)

var emptyCodeHash = crypto.Keccak256(nil)

// StateCache holds the realtime block state caches and the chainstate db from the default
// execution logic. The highest block number in the block state cache holds the latest
// confirmed realtime state.
type StateCache struct {
	cacheLock    sync.RWMutex
	globalHeight uint64
	blocksCache  map[uint64]*BlockStateCache
}

func NewStateCache(blocksCacheSize int) *StateCache {
	return &StateCache{
		globalHeight: 0,
		blocksCache:  make(map[uint64]*BlockStateCache, blocksCacheSize),
	}
}

func (cache *StateCache) TryInitCache(executionHeight uint64) error {
	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	if cache.globalHeight != 0 {
		return fmt.Errorf("state cache already initialized")
	}
	cache.globalHeight = executionHeight

	return nil
}

func (cache *StateCache) Clear() {
	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	// Clear block state caches
	for blockNum, bc := range cache.blocksCache {
		bc.Clear()
		delete(cache.blocksCache, blockNum)
	}

	// Clear global height
	cache.globalHeight = 0
}

func (cache *StateCache) GetConfirmBlockStateCache(blockNum uint64) (*BlockStateCache, error) {
	cache.cacheLock.RLock()
	defer cache.cacheLock.RUnlock()

	if blockNum <= cache.globalHeight {
		return nil, fmt.Errorf("block number %d is less than or equal to state cache global height %d", blockNum, cache.globalHeight)
	} else {
		bc, exists := cache.blocksCache[blockNum]
		if !exists {
			return nil, fmt.Errorf("block number %d not found in the state cache", blockNum)
		}
		return bc, nil
	}
}

func (cache *StateCache) AddBlock(blockNum uint64, blockStateCache *BlockStateCache) error {
	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	_, exists := cache.blocksCache[blockNum]
	if exists {
		return fmt.Errorf("block %d already exists in the confirmed block state cache", blockNum)
	}
	cache.blocksCache[blockNum] = blockStateCache
	return nil
}

func (cache *StateCache) FlushBlock(blockNum uint64) error {
	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	if cache.globalHeight == 0 {
		return nil // Not initialized yet
	}

	if blockNum <= cache.globalHeight {
		return nil
	}

	// Process all blocks from globalHeight+1 to blockNum
	// This handles cases where blockNum might not be consecutive
	for flushHeight := cache.globalHeight + 1; flushHeight <= blockNum; flushHeight++ {
		bc, exists := cache.blocksCache[flushHeight]
		if !exists {
			return fmt.Errorf("failed to flush block %d, block state cache not found in state cache. globalHeight: %d", flushHeight, cache.globalHeight)
		}

		// Verify that the block cache is head (previous state reader is nil)
		if bc.GetPrevBlockCache() != nil {
			return fmt.Errorf("failed to flush block %d, block is not at head, prev state reader is not nil. globalHeight: %d", flushHeight, cache.globalHeight)
		}

		// Flush global height
		cache.globalHeight = flushHeight

		// Update linked list - set the next block previous reader to head
		nbc := bc.GetNextBlockCache()
		if nbc != nil {
			nbc.SetPrevBlockCache(nil)
		}

		// Remove from map and clear
		delete(cache.blocksCache, flushHeight)
		bc.Clear()
	}

	return nil
}

// -------------- Debug operations --------------
func (cache *StateCache) DebugDumpToFile(cacheDumpPath string) error {
	flatten, err := cache.flattenState()
	if err != nil {
		return err
	}

	accountData := make(map[string]string)
	for addr, acc := range flatten.accountCache {
		value := make([]byte, acc.EncodingLengthForStorage())
		acc.EncodeForStorage(value)
		accountData[hex.EncodeToString(addr[:])] = hex.EncodeToString(value)
	}
	if err := realtimeTypes.WriteToJSON(filepath.Join(cacheDumpPath, "account_cache.json"), accountData); err != nil {
		return fmt.Errorf("failed to dump account cache: %v", err)
	}

	storageData := make(map[string]string)
	for key, value := range flatten.storageCache {
		storageData[hex.EncodeToString([]byte(key))] = hex.EncodeToString(value.Bytes())
	}
	if err := realtimeTypes.WriteToJSON(filepath.Join(cacheDumpPath, "storage_cache.json"), storageData); err != nil {
		return fmt.Errorf("failed to dump storage cache: %v", err)
	}

	codeData := make(map[string]string)
	for hash, code := range flatten.codeCache {
		codeData[hex.EncodeToString(hash[:])] = hex.EncodeToString(code)
	}
	if err := realtimeTypes.WriteToJSON(filepath.Join(cacheDumpPath, "code_cache.json"), codeData); err != nil {
		return fmt.Errorf("failed to dump code cache: %v", err)
	}

	incarnationData := make(map[string]uint64)
	for addr, incarnation := range flatten.incarnationMapCache {
		incarnationData[hex.EncodeToString(addr[:])] = incarnation
	}
	if err := realtimeTypes.WriteToJSON(filepath.Join(cacheDumpPath, "incarnation_cache.json"), incarnationData); err != nil {
		return fmt.Errorf("failed to dump incarnation cache: %v", err)
	}

	return nil
}

// DebugCompare compares the state cache with the chain-state db, and returns the
// list of account addresses that have differing states.
func (cache *StateCache) DebugCompare(reader state.StateReader) ([]string, error) {
	flatten, err := cache.flattenState()
	if err != nil {
		return nil, err
	}

	mismatches := []string{}
	for addr, accCache := range flatten.accountCache {
		accDb, err := reader.ReadAccountData(addr)
		if err != nil {
			mismatch := fmt.Sprintf("chain-state db reader error, failed to read account. address: %s, error: %v", addr.String(), err)
			mismatches = append(mismatches, mismatch)
			continue
		}
		if accDb == nil {
			mismatch := fmt.Sprintf("account %s not found in database", addr.String())
			mismatches = append(mismatches, mismatch)
			continue
		}

		if accCache.Nonce != accDb.Nonce {
			mismatch := fmt.Sprintf("nonce mismatch, account %s, cache nonce: %d, db nonce: %d", addr.String(), accCache.Nonce, accDb.Nonce)
			mismatches = append(mismatches, mismatch)
		}

		if accCache.Balance.Cmp(&accDb.Balance) != 0 {
			mismatch := fmt.Sprintf("balance mismatch, account %s, cache balance: %d, db balance: %d", addr.String(), accCache.Balance.ToBig(), accDb.Balance.ToBig())
			mismatches = append(mismatches, mismatch)
		}

		if accCache.Root != accDb.Root {
			mismatch := fmt.Sprintf("root mismatch, account %s, cache root: %s, db root: %s", addr.String(), accCache.Root.String(), accDb.Root.String())
			mismatches = append(mismatches, mismatch)
		}

		if accCache.CodeHash != accDb.CodeHash {
			mismatch := fmt.Sprintf("codehash mismatch, account %s, cache codehash: %s, db codehash: %s", addr.String(), accCache.CodeHash.String(), accDb.CodeHash.String())
			mismatches = append(mismatches, mismatch)
		}
	}

	return mismatches, nil
}

func (cache *StateCache) flattenState() (*plainStateCache, error) {
	cache.cacheLock.RLock()
	defer cache.cacheLock.RUnlock()

	flatten := newPlainStateCache(DefaultStateBlockCacheSize * DefaultPlainStateCacheSize)
	blockNum := cache.globalHeight + 1
	for {
		bc, exists := cache.blocksCache[blockNum]
		if !exists {
			break
		}
		flatten.Flatten(bc.cache)
		blockNum++
	}

	return flatten, nil
}
