package cache

import (
	"bytes"
	"context"
	"sync"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/dbutils"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/types/accounts"
)

// GlobalStateCache implements the plain state reader with a changeset cache layer.
// The global cache holds the latest chainstate - it holds the chainstate db,
// with a changeset cache layer that stores in-memory the latest state changes.
type GlobalStateCache struct {
	ctx context.Context
	db  kv.RoDB

	cacheLock sync.RWMutex
	cache     *plainStateCache
}

func NewGlobalStateCache(ctx context.Context, db kv.RoDB, size int) *GlobalStateCache {
	return &GlobalStateCache{
		ctx:   ctx,
		db:    db,
		cache: newPlainStateCache(size),
	}
}

func (cache *GlobalStateCache) Clear() {
	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	// Clear all caches
	cache.cache.Clear()
}

func (cache *GlobalStateCache) FlushState(incoming *plainStateCache) error {
	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	cache.cache.Flatten(incoming)
	return nil
}

// -------------- StateReader implementation --------------
func (cache *GlobalStateCache) ReadAccountData(address libcommon.Address) (*accounts.Account, error) {
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
