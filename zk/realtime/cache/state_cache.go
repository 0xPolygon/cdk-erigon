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
	"github.com/ledgerwatch/erigon/turbo/trie"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	"github.com/ledgerwatch/erigon/zkevm/log"
)

var (
	ErrNotReady   = fmt.Errorf("state cache not initialized")
	emptyCodeHash = crypto.Keccak256(nil)
)

// PlainStateCache implements the plain state reader with a changeset cache layer.
// The cache holds the chainstate db, with a changeset cache layer that stores
// in-memory the state changes.
type PlainStateCache struct {
	ctx        context.Context
	db         kv.RoDB
	initHeight atomic.Uint64

	cacheLock           sync.RWMutex
	accountCache        map[libcommon.Address]*accounts.Account
	storageCache        map[string]*uint256.Int
	codeCache           map[libcommon.Hash][]byte
	incarnationMapCache map[libcommon.Address]uint64
}

func NewPlainStateCache(ctx context.Context, db kv.RoDB, size int) (*PlainStateCache, error) {
	return &PlainStateCache{
		ctx:                 ctx,
		db:                  db,
		initHeight:          atomic.Uint64{},
		accountCache:        make(map[libcommon.Address]*accounts.Account, size),
		storageCache:        make(map[string]*uint256.Int, size),
		codeCache:           make(map[libcommon.Hash][]byte, size),
		incarnationMapCache: make(map[libcommon.Address]uint64, size),
	}, nil
}

func (cache *PlainStateCache) TryInitCache(executionHeight uint64) error {
	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	if cache.initHeight.Load() != 0 {
		return fmt.Errorf("state cache already initialized")
	}
	cache.initHeight.Store(executionHeight)

	return nil
}

func (cache *PlainStateCache) Clear() {
	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	// Clear all caches
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
	cache.initHeight.Store(0)
}

// -------------- State cache write operations --------------
func (cache *PlainStateCache) ApplyChangeset(changeset *realtimeTypes.Changeset, blockNumber uint64, txIndex uint) error {
	// Handle account data changes
	addressChanges := make(map[libcommon.Address]*accounts.Account)
	cache.applyChangesetToAccountData(changeset, addressChanges)

	cache.cacheLock.Lock()
	defer cache.cacheLock.Unlock()

	// Apply code changes
	for codeHash, code := range changeset.CodeChanges {
		cache.codeCache[codeHash] = code
	}

	// Apply storage changes
	for address, storage := range changeset.StorageChanges {
		account, err := cache.getOrCreateAccount(address, addressChanges)
		if err != nil {
			return fmt.Errorf("apply storage changes failed: %v", err)
		}

		for key, value := range storage {
			compositeKey := dbutils.PlainGenerateCompositeStorageKey(address.Bytes(), account.Incarnation, key.Bytes())
			cache.storageCache[string(compositeKey)] = value
		}
	}

	// Apply incarnation map changes
	for address, incarnation := range changeset.IncarnationMapChanges {
		cache.incarnationMapCache[address] = incarnation
	}

	// Apply deleted accounts changes
	for address := range changeset.DeletedAccounts {
		// Non-existent / deleted accounts are set to nil
		addressChanges[address] = nil
	}

	// Apply account changes
	for address, account := range addressChanges {
		delete(cache.accountCache, address)
		cache.accountCache[address] = account
		log.Debug("[Realtime] ApplyChangeset: ", address)
	}

	log.Debug(fmt.Sprintf("[Realtime] Apply changeset from tx with height: %d, txIndex: %d\n", blockNumber, txIndex))

	return nil
}

func (cache *PlainStateCache) applyChangesetToAccountData(changeset *realtimeTypes.Changeset, addressChanges map[libcommon.Address]*accounts.Account) (err error) {
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

// -------------- State cache read operations --------------
func (cache *PlainStateCache) GetInitHeight() uint64 {
	return cache.initHeight.Load()
}

func (cache *PlainStateCache) ReadAccountData(address libcommon.Address) (*accounts.Account, error) {
	if cache.initHeight.Load() == 0 {
		return nil, ErrNotReady
	}

	cache.cacheLock.RLock()
	acc, ok := cache.accountCache[address]
	if ok {
		accCopy := accounts.DeepCopyAccount(acc)
		cache.cacheLock.RUnlock()
		return accCopy, nil
	}
	cache.cacheLock.RUnlock()

	// Cache miss, read from chainstate db
	return cache.GetAccountFromChainDb(address)
}

func (cache *PlainStateCache) ReadAccountStorage(address libcommon.Address, incarnation uint64, key *libcommon.Hash) ([]byte, error) {
	if cache.initHeight.Load() == 0 {
		return nil, ErrNotReady
	}

	compositeKey := dbutils.PlainGenerateCompositeStorageKey(address.Bytes(), incarnation, key.Bytes())

	cache.cacheLock.RLock()
	storage, ok := cache.storageCache[string(compositeKey)]
	if ok {
		storageCopy := libcommon.Copy(storage.Bytes())
		cache.cacheLock.RUnlock()
		return storageCopy, nil
	}
	cache.cacheLock.RUnlock()

	// Cache miss, read from chainstate db
	return cache.GetAccountStorageFromChainDb(address, incarnation, key)
}

func (cache *PlainStateCache) ReadAccountCode(address libcommon.Address, incarnation uint64, codeHash libcommon.Hash) ([]byte, error) {
	if bytes.Equal(codeHash.Bytes(), emptyCodeHash) {
		return nil, nil
	}

	if cache.initHeight.Load() == 0 {
		return nil, ErrNotReady
	}

	cache.cacheLock.RLock()
	code, ok := cache.codeCache[codeHash]
	if ok {
		codeCopy := libcommon.Copy(code)
		cache.cacheLock.RUnlock()
		return codeCopy, nil
	}
	cache.cacheLock.RUnlock()

	// Cache miss, read from chainstate db
	return cache.GetAccountCodeFromChainDb(address, incarnation, codeHash)
}

func (cache *PlainStateCache) ReadAccountCodeSize(address libcommon.Address, incarnation uint64, codeHash libcommon.Hash) (int, error) {
	code, err := cache.ReadAccountCode(address, incarnation, codeHash)
	return len(code), err
}

func (cache *PlainStateCache) ReadAccountIncarnation(address libcommon.Address) (uint64, error) {
	if cache.initHeight.Load() == 0 {
		return 0, ErrNotReady
	}

	cache.cacheLock.RLock()
	incarnation, ok := cache.incarnationMapCache[address]
	cache.cacheLock.RUnlock()
	if ok {
		return incarnation, nil
	}

	// Cache miss, read from chainstate db
	return cache.GetAccountIncarnationFromChainDb(address)
}

func (cache *PlainStateCache) getOrCreateAccount(address libcommon.Address, addressChanges map[libcommon.Address]*accounts.Account) (*accounts.Account, error) {
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

func (cache *PlainStateCache) unsafeReadAccountData(address libcommon.Address) (*accounts.Account, error) {
	acc, ok := cache.accountCache[address]
	if ok {
		return acc, nil
	}

	// Cache miss, read from chainstate db
	return cache.GetAccountFromChainDb(address)
}

func (cache *PlainStateCache) createAccount() (*accounts.Account, error) {
	return &accounts.Account{
		Initialised: true,
		Root:        libcommon.BytesToHash(trie.EmptyRoot[:]),
		CodeHash:    libcommon.BytesToHash(emptyCodeHash),
	}, nil
}

// -------------- Chain state reader operations --------------
func (cache *PlainStateCache) GetAccountFromChainDb(address libcommon.Address) (*accounts.Account, error) {
	tx, err := cache.db.BeginRo(cache.ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	reader := state.NewPlainStateReader(tx)
	return reader.ReadAccountData(address)
}

func (cache *PlainStateCache) GetAccountStorageFromChainDb(address libcommon.Address, incarnation uint64, key *libcommon.Hash) ([]byte, error) {
	tx, err := cache.db.BeginRo(cache.ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	reader := state.NewPlainStateReader(tx)
	return reader.ReadAccountStorage(address, incarnation, key)
}

func (cache *PlainStateCache) GetAccountCodeFromChainDb(address libcommon.Address, incarnation uint64, codeHash libcommon.Hash) ([]byte, error) {
	tx, err := cache.db.BeginRo(cache.ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	reader := state.NewPlainStateReader(tx)
	return reader.ReadAccountCode(address, incarnation, codeHash)
}

func (cache *PlainStateCache) GetAccountIncarnationFromChainDb(address libcommon.Address) (uint64, error) {
	tx, err := cache.db.BeginRo(cache.ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	reader := state.NewPlainStateReader(tx)
	return reader.ReadAccountIncarnation(address)
}

// -------------- Debug operations --------------
func (cache *PlainStateCache) DebugDumpToFile(cacheDumpPath string) error {
	cache.cacheLock.RLock()
	defer cache.cacheLock.RUnlock()

	accountData := make(map[string]string)
	for addr, acc := range cache.accountCache {
		value := make([]byte, acc.EncodingLengthForStorage())
		acc.EncodeForStorage(value)
		accountData[hex.EncodeToString(addr[:])] = hex.EncodeToString(value)
	}
	if err := realtimeTypes.WriteToJSON(filepath.Join(cacheDumpPath, "account_cache.json"), accountData); err != nil {
		return fmt.Errorf("failed to dump account cache: %v", err)
	}

	storageData := make(map[string]string)
	for key, value := range cache.storageCache {
		storageData[hex.EncodeToString([]byte(key))] = hex.EncodeToString(value.Bytes())
	}
	if err := realtimeTypes.WriteToJSON(filepath.Join(cacheDumpPath, "storage_cache.json"), storageData); err != nil {
		return fmt.Errorf("failed to dump storage cache: %v", err)
	}

	codeData := make(map[string]string)
	for hash, code := range cache.codeCache {
		codeData[hex.EncodeToString(hash[:])] = hex.EncodeToString(code)
	}
	if err := realtimeTypes.WriteToJSON(filepath.Join(cacheDumpPath, "code_cache.json"), codeData); err != nil {
		return fmt.Errorf("failed to dump code cache: %v", err)
	}

	incarnationData := make(map[string]uint64)
	for addr, incarnation := range cache.incarnationMapCache {
		incarnationData[hex.EncodeToString(addr[:])] = incarnation
	}
	if err := realtimeTypes.WriteToJSON(filepath.Join(cacheDumpPath, "incarnation_cache.json"), incarnationData); err != nil {
		return fmt.Errorf("failed to dump incarnation cache: %v", err)
	}

	return nil
}

// DebugCompare compares the state cache with the chain-state db, and returns the
// list of account addresses that have differing states.
func (cache *PlainStateCache) DebugCompare(reader state.StateReader) []string {
	cache.cacheLock.RLock()
	defer cache.cacheLock.RUnlock()

	mismatches := []string{}
	for addr, accCache := range cache.accountCache {
		log.Info("[Realtime] Comparing account", "address", addr.String())
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
