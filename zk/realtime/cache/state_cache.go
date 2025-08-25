package cache

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/dbutils"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/types/accounts"
	"github.com/ledgerwatch/erigon/crypto"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	"github.com/ledgerwatch/log/v3"
)

var (
	ErrNotReady   = fmt.Errorf("state cache not initialized")
	emptyCodeHash = crypto.Keccak256(nil)
)

type stateCache struct {
	accountCache        map[libcommon.Address]*accounts.Account
	storageCache        map[string]*uint256.Int
	codeCache           map[libcommon.Hash][]byte
	incarnationMapCache map[libcommon.Address]uint64
}

func newStateCache(size int) *stateCache {
	return &stateCache{
		accountCache:        make(map[libcommon.Address]*accounts.Account, size),
		storageCache:        make(map[string]*uint256.Int, size),
		codeCache:           make(map[libcommon.Hash][]byte, size),
		incarnationMapCache: make(map[libcommon.Address]uint64, size),
	}
}

func (cache *stateCache) Clear() {
	for k := range cache.accountCache {
		delete(cache.accountCache, k)
	}
	for k := range cache.storageCache {
		delete(cache.storageCache, k)
	}
	for k := range cache.codeCache {
		delete(cache.codeCache, k)
	}
	for k := range cache.incarnationMapCache {
		delete(cache.incarnationMapCache, k)
	}
}

// GlobalStateCache implements the plain state reader with a changeset cache layer.
// The global cache holds the latest chainstate - it holds the chainstate db,
// with a changeset cache layer that stores in-memory the latest state changes.
type GlobalStateCache struct {
	ctx        context.Context
	db         kv.RoDB
	initHeight atomic.Uint64

	cacheLock sync.RWMutex
	cache     *stateCache
}

func NewGlobalStateCache(ctx context.Context, db kv.RoDB, size int) (*GlobalStateCache, error) {
	return &GlobalStateCache{
		ctx:        ctx,
		db:         db,
		initHeight: atomic.Uint64{},
		cache:      newStateCache(size),
	}, nil
}

func (cache *GlobalStateCache) TryInitCache(executionHeight uint64) error {
	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	if cache.initHeight.Load() != 0 {
		return fmt.Errorf("state cache already initialized")
	}
	cache.initHeight.Store(executionHeight)

	return nil
}

func (cache *GlobalStateCache) Clear() {
	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	// Clear all caches
	cache.cache.Clear()
	cache.initHeight.Store(0)
}

func (cache *GlobalStateCache) GetInitHeight() uint64 {
	return cache.initHeight.Load()
}

// -------------- Cache operations --------------
func (cache *GlobalStateCache) FlushState(stateCache *stateCache) error {
	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	// Apply account changes
	for address, account := range stateCache.accountCache {
		delete(cache.cache.accountCache, address)
		cache.cache.accountCache[address] = account
	}

	// Apply code changes
	for codeHash, code := range stateCache.codeCache {
		delete(cache.cache.codeCache, codeHash)
		cache.cache.codeCache[codeHash] = code
	}

	// Apply storage changes
	for key, value := range stateCache.storageCache {
		delete(cache.cache.storageCache, key)
		cache.cache.storageCache[key] = value
	}

	// Apply incarnation map changes
	for address, incarnation := range stateCache.incarnationMapCache {
		delete(cache.cache.incarnationMapCache, address)
		cache.cache.incarnationMapCache[address] = incarnation
	}

	return nil
}

// -------------- StateReader implementation --------------
func (cache *GlobalStateCache) ReadAccountData(address libcommon.Address) (*accounts.Account, error) {
	if cache.initHeight.Load() == 0 {
		return nil, ErrNotReady
	}

	cache.cacheLock.RLock()
	acc, ok := cache.cache.accountCache[address]
	if ok {
		accCopy := accounts.DeepCopyAccount(acc)
		cache.cacheLock.RUnlock()
		return accCopy, nil
	}
	cache.cacheLock.RUnlock()

	// Cache miss, read from chainstate db
	return cache.GetAccountFromChainDb(address)
}

func (cache *GlobalStateCache) ReadAccountStorage(address libcommon.Address, incarnation uint64, key *libcommon.Hash) ([]byte, error) {
	if cache.initHeight.Load() == 0 {
		return nil, ErrNotReady
	}

	compositeKey := dbutils.PlainGenerateCompositeStorageKey(address.Bytes(), incarnation, key.Bytes())

	cache.cacheLock.RLock()
	storage, ok := cache.cache.storageCache[string(compositeKey)]
	if ok {
		storageCopy := libcommon.Copy(storage.Bytes())
		cache.cacheLock.RUnlock()
		return storageCopy, nil
	}
	cache.cacheLock.RUnlock()

	// Cache miss, read from chainstate db
	return cache.GetAccountStorageFromChainDb(address, incarnation, key)
}

func (cache *GlobalStateCache) ReadAccountCode(address libcommon.Address, incarnation uint64, codeHash libcommon.Hash) ([]byte, error) {
	if bytes.Equal(codeHash.Bytes(), emptyCodeHash) {
		return nil, nil
	}

	if cache.initHeight.Load() == 0 {
		return nil, ErrNotReady
	}

	cache.cacheLock.RLock()
	code, ok := cache.cache.codeCache[codeHash]
	if ok {
		codeCopy := libcommon.Copy(code)
		cache.cacheLock.RUnlock()
		return codeCopy, nil
	}
	cache.cacheLock.RUnlock()

	// Cache miss, read from chainstate db
	return cache.GetAccountCodeFromChainDb(address, incarnation, codeHash)
}

func (cache *GlobalStateCache) ReadAccountCodeSize(address libcommon.Address, incarnation uint64, codeHash libcommon.Hash) (int, error) {
	code, err := cache.ReadAccountCode(address, incarnation, codeHash)
	return len(code), err
}

func (cache *GlobalStateCache) ReadAccountIncarnation(address libcommon.Address) (uint64, error) {
	if cache.initHeight.Load() == 0 {
		return 0, ErrNotReady
	}

	cache.cacheLock.RLock()
	incarnation, ok := cache.cache.incarnationMapCache[address]
	cache.cacheLock.RUnlock()
	if ok {
		return incarnation, nil
	}

	// Cache miss, read from chainstate db
	return cache.GetAccountIncarnationFromChainDb(address)
}

// -------------- Chainstate db reader operations --------------
func (cache *GlobalStateCache) GetAccountFromChainDb(address libcommon.Address) (*accounts.Account, error) {
	tx, err := cache.db.BeginRo(cache.ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	reader := state.NewPlainStateReader(tx)
	return reader.ReadAccountData(address)
}

func (cache *GlobalStateCache) GetAccountStorageFromChainDb(address libcommon.Address, incarnation uint64, key *libcommon.Hash) ([]byte, error) {
	tx, err := cache.db.BeginRo(cache.ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	reader := state.NewPlainStateReader(tx)
	return reader.ReadAccountStorage(address, incarnation, key)
}

func (cache *GlobalStateCache) GetAccountCodeFromChainDb(address libcommon.Address, incarnation uint64, codeHash libcommon.Hash) ([]byte, error) {
	tx, err := cache.db.BeginRo(cache.ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	reader := state.NewPlainStateReader(tx)
	return reader.ReadAccountCode(address, incarnation, codeHash)
}

func (cache *GlobalStateCache) GetAccountIncarnationFromChainDb(address libcommon.Address) (uint64, error) {
	tx, err := cache.db.BeginRo(cache.ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	reader := state.NewPlainStateReader(tx)
	return reader.ReadAccountIncarnation(address)
}

// -------------- Debug operations --------------
func (cache *GlobalStateCache) DebugDumpToFile(cacheDumpPath string) error {
	cache.cacheLock.RLock()
	defer cache.cacheLock.RUnlock()

	accountData := make(map[string]string)
	for addr, acc := range cache.cache.accountCache {
		value := make([]byte, acc.EncodingLengthForStorage())
		acc.EncodeForStorage(value)
		accountData[hex.EncodeToString(addr[:])] = hex.EncodeToString(value)
	}
	if err := realtimeTypes.WriteToJSON(filepath.Join(cacheDumpPath, "account_cache.json"), accountData); err != nil {
		return fmt.Errorf("failed to dump account cache: %v", err)
	}

	storageData := make(map[string]string)
	for key, value := range cache.cache.storageCache {
		storageData[hex.EncodeToString([]byte(key))] = hex.EncodeToString(value.Bytes())
	}
	if err := realtimeTypes.WriteToJSON(filepath.Join(cacheDumpPath, "storage_cache.json"), storageData); err != nil {
		return fmt.Errorf("failed to dump storage cache: %v", err)
	}

	codeData := make(map[string]string)
	for hash, code := range cache.cache.codeCache {
		codeData[hex.EncodeToString(hash[:])] = hex.EncodeToString(code)
	}
	if err := realtimeTypes.WriteToJSON(filepath.Join(cacheDumpPath, "code_cache.json"), codeData); err != nil {
		return fmt.Errorf("failed to dump code cache: %v", err)
	}

	incarnationData := make(map[string]uint64)
	for addr, incarnation := range cache.cache.incarnationMapCache {
		incarnationData[hex.EncodeToString(addr[:])] = incarnation
	}
	if err := realtimeTypes.WriteToJSON(filepath.Join(cacheDumpPath, "incarnation_cache.json"), incarnationData); err != nil {
		return fmt.Errorf("failed to dump incarnation cache: %v", err)
	}

	return nil
}

// DebugCompare compares the state cache with the chain-state db, and returns the
// list of account addresses that have differing states.
func (cache *GlobalStateCache) DebugCompare(reader state.StateReader) []string {
	cache.cacheLock.RLock()
	defer cache.cacheLock.RUnlock()

	mismatches := []string{}
	for addr, accCache := range cache.cache.accountCache {
		log.Info(fmt.Sprintf("[Realtime] Comparing account address: %s", addr.String()))
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

	return mismatches
}
