package cache

import (
	"bytes"
	"fmt"
	"sync"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv/dbutils"
	"github.com/ledgerwatch/erigon/core/types/accounts"
	"github.com/ledgerwatch/erigon/turbo/trie"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	"github.com/ledgerwatch/log/v3"
)

// GlobalStateCache implements the plain state reader with a changeset cache layer.
// The pending cache holds the pending chainstate - it holds the global state cache,
// with a changeset cache layer that stores in-memory the pending state changes.
type PendingStateCache struct {
	globalCache *GlobalStateCache
	cacheLock   sync.RWMutex
	cache       *stateCache
}

func NewPendingStateCache(globalCache *GlobalStateCache, size int) *PendingStateCache {
	return &PendingStateCache{
		globalCache: globalCache,
		cache:       newStateCache(size),
	}
}

func (cache *PendingStateCache) ApplyChangeset(changeset *realtimeTypes.Changeset, blockNumber uint64, txIndex uint) error {
	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	// Handle account data changes
	addressChanges := make(map[libcommon.Address]*accounts.Account)
	cache.applyChangesetToAccountData(changeset, addressChanges)

	// Apply code changes
	for codeHash, code := range changeset.CodeChanges {
		cache.cache.codeCache[codeHash] = code
	}

	// Apply storage changes
	for address, storage := range changeset.StorageChanges {
		account, err := cache.getOrCreateAccount(address, addressChanges)
		if err != nil {
			return fmt.Errorf("apply storage changes failed: %v", err)
		}

		for key, value := range storage {
			compositeKey := dbutils.PlainGenerateCompositeStorageKey(address.Bytes(), account.Incarnation, key.Bytes())
			cache.cache.storageCache[string(compositeKey)] = value
		}
	}

	// Apply incarnation map changes
	for address, incarnation := range changeset.IncarnationMapChanges {
		cache.cache.incarnationMapCache[address] = incarnation
	}

	// Apply deleted accounts changes
	for address := range changeset.DeletedAccounts {
		// Non-existent / deleted accounts are set to nil
		addressChanges[address] = nil
	}

	// Apply account changes
	for address, account := range addressChanges {
		delete(cache.cache.accountCache, address)
		cache.cache.accountCache[address] = account
		log.Debug("[Realtime] ApplyChangeset: ", address)
	}

	log.Debug(fmt.Sprintf("[Realtime] Apply changeset from tx with height: %d, txIndex: %d\n", blockNumber, txIndex))

	return nil
}

func (cache *PendingStateCache) applyChangesetToAccountData(changeset *realtimeTypes.Changeset, addressChanges map[libcommon.Address]*accounts.Account) (err error) {
	// Apply balance changes
	for address, balance := range changeset.BalanceChanges {
		if _, ok := changeset.DeletedAccounts[address]; ok {
			continue
		}

		account, err := cache.getOrCreateAccount(address, addressChanges)
		if err != nil {
			return fmt.Errorf("apply balance changes failed: %v", err)
		}
		account.Balance.Set(balance)
	}

	// Apply nonce changes
	for address, nonce := range changeset.NonceChanges {
		if _, ok := changeset.DeletedAccounts[address]; ok {
			continue
		}

		account, err := cache.getOrCreateAccount(address, addressChanges)
		if err != nil {
			return fmt.Errorf("apply nonce changes failed: %v", err)
		}
		account.Nonce = nonce
	}

	// Apply code hash changes
	for address, codeHash := range changeset.CodeHashChanges {
		if _, ok := changeset.DeletedAccounts[address]; ok {
			continue
		}

		account, err := cache.getOrCreateAccount(address, addressChanges)
		if err != nil {
			return fmt.Errorf("apply code hash changes failed: %v", err)
		}
		account.CodeHash = codeHash
	}

	// Apply incarnation changes
	for address, incarnation := range changeset.IncarnationChanges {
		if _, ok := changeset.DeletedAccounts[address]; ok {
			continue
		}

		account, err := cache.getOrCreateAccount(address, addressChanges)
		if err != nil {
			return fmt.Errorf("apply incarnation changes failed: %v", err)
		}
		account.Incarnation = incarnation
	}

	return nil
}

func (cache *PendingStateCache) getOrCreateAccount(address libcommon.Address, addressChanges map[libcommon.Address]*accounts.Account) (*accounts.Account, error) {
	account, ok := addressChanges[address]
	if !ok {
		var err error
		account, err = cache.unsafeReadAccountData(address)
		if err != nil {
			return nil, err
		}

		if account == nil {
			// Non-existent account, create new account
			account, err = cache.createAccount()
			if err != nil {
				return nil, err
			}
		}
		addressChanges[address] = account
	}

	return account, nil
}

func (cache *PendingStateCache) unsafeReadAccountData(address libcommon.Address) (*accounts.Account, error) {
	acc, ok := cache.cache.accountCache[address]
	if ok {
		return acc, nil
	}

	// Cache miss, read from global cache
	return cache.globalCache.ReadAccountData(address)
}

func (cache *PendingStateCache) createAccount() (*accounts.Account, error) {
	return &accounts.Account{
		Initialised: true,
		Root:        libcommon.BytesToHash(trie.EmptyRoot[:]),
		CodeHash:    libcommon.BytesToHash(emptyCodeHash),
	}, nil
}

// -------------- StateReader implementation --------------
func (cache *PendingStateCache) ReadAccountData(address libcommon.Address) (*accounts.Account, error) {
	cache.cacheLock.RLock()
	acc, ok := cache.cache.accountCache[address]
	if ok {
		accCopy := accounts.DeepCopyAccount(acc)
		cache.cacheLock.RUnlock()
		return accCopy, nil
	}
	cache.cacheLock.RUnlock()

	// Cache miss, read from global cache
	return cache.globalCache.ReadAccountData(address)
}

func (cache *PendingStateCache) ReadAccountStorage(address libcommon.Address, incarnation uint64, key *libcommon.Hash) ([]byte, error) {
	compositeKey := dbutils.PlainGenerateCompositeStorageKey(address.Bytes(), incarnation, key.Bytes())

	cache.cacheLock.RLock()
	storage, ok := cache.cache.storageCache[string(compositeKey)]
	if ok {
		storageCopy := libcommon.Copy(storage.Bytes())
		cache.cacheLock.RUnlock()
		return storageCopy, nil
	}
	cache.cacheLock.RUnlock()

	// Cache miss, read from global cache
	return cache.globalCache.ReadAccountStorage(address, incarnation, key)
}

func (cache *PendingStateCache) ReadAccountCode(address libcommon.Address, incarnation uint64, codeHash libcommon.Hash) ([]byte, error) {
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

	// Cache miss, read from global cache
	return cache.globalCache.ReadAccountCode(address, incarnation, codeHash)
}

func (cache *PendingStateCache) ReadAccountCodeSize(address libcommon.Address, incarnation uint64, codeHash libcommon.Hash) (int, error) {
	code, err := cache.ReadAccountCode(address, incarnation, codeHash)
	return len(code), err
}

func (cache *PendingStateCache) ReadAccountIncarnation(address libcommon.Address) (uint64, error) {
	cache.cacheLock.RLock()
	incarnation, ok := cache.cache.incarnationMapCache[address]
	cache.cacheLock.RUnlock()
	if ok {
		return incarnation, nil
	}

	// Cache miss, read from global cache
	return cache.globalCache.ReadAccountIncarnation(address)
}
