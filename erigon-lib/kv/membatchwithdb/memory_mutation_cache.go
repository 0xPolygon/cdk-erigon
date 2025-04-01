package membatchwithdb

import (
	"bytes"

	"github.com/ledgerwatch/erigon-lib/common"

	"github.com/c2h5oh/datasize"
	"github.com/ledgerwatch/erigon-lib/kv"
)

// MemoryMutationWithCache extends MemoryMutation with a caching layer
type MemoryMutationWithCache struct {
	*MemoryMutation
	cache       map[string]map[string][]byte // Read-only cache, passed externally
	modifyCache map[string]map[string][]byte // Writable cache for modifications
}

// NewMemoryBatchWithSizeNoSequenceWithCache creates a cached version with custom size
func NewMemoryBatchWithSizeNoSequenceWithCache(tx kv.Tx, tmpDir string, mapSize datasize.ByteSize, cache map[string]map[string][]byte) *MemoryMutationWithCache {
	base := NewMemoryBatchWithSizeNoSequence(tx, tmpDir, mapSize)
	if cache == nil {
		cache = make(map[string]map[string][]byte)
	}

	return &MemoryMutationWithCache{
		MemoryMutation: base,
		cache:          cache,
		modifyCache:    make(map[string]map[string][]byte),
	}
}

// GetOne with cache support, prioritizes modifyCache, then cache, then MemoryMutation
func (m *MemoryMutationWithCache) GetOne(table string, key []byte) ([]byte, error) {
	keyStr := string(key)

	// 1. Check modifyCache first
	if modKeys, ok := m.modifyCache[table]; ok {
		if val, exists := modKeys[keyStr]; exists {
			return val, nil
		}
	}

	// 2. Check read-only cache
	if keys, ok := m.cache[table]; ok {
		if val, exists := keys[keyStr]; exists {
			return val, nil
		}
	}

	// 3. Fall back to MemoryMutation
	c, err := m.statelessCursor(table)
	if err != nil {
		return nil, err
	}
	_, v, err := c.SeekExact(key)
	if err == nil && v != nil {
		// Store in modifyCache (not cache, as cache is read-only)
		if _, ok := m.modifyCache[table]; !ok {
			m.modifyCache[table] = make(map[string][]byte)
		}
		m.modifyCache[table][keyStr] = common.Copy(v)
	}
	return v, err
}

// Has with cache support, prioritizes modifyCache, then cache, then MemoryMutation
func (m *MemoryMutationWithCache) Has(table string, key []byte) (bool, error) {
	keyStr := string(key)

	// 1. Check modifyCache first
	if modKeys, ok := m.modifyCache[table]; ok {
		if _, exists := modKeys[keyStr]; exists {
			return true, nil
		}
	}

	// 2. Check read-only cache
	if keys, ok := m.cache[table]; ok {
		if _, exists := keys[keyStr]; exists {
			return true, nil
		}
	}

	// 3. Fall back to MemoryMutation
	c, err := m.statelessCursor(table)
	if err != nil {
		return false, err
	}
	k, _, err := c.Seek(key)
	if err != nil {
		return false, err
	}
	exists := bytes.Equal(key, k)
	if exists {
		// Store presence in modifyCache
		if _, ok := m.modifyCache[table]; !ok {
			m.modifyCache[table] = make(map[string][]byte)
		}
		m.modifyCache[table][keyStr] = nil // Presence only, value updated on GetOne
	}
	return exists, nil
}

// Put with cache support, writes to modifyCache only
func (m *MemoryMutationWithCache) Put(table string, k, v []byte) error {
	err := m.memTx.Put(table, k, v)
	if err != nil {
		return err
	}
	// Write to modifyCache only
	if _, ok := m.modifyCache[table]; !ok {
		m.modifyCache[table] = make(map[string][]byte)
	}
	m.modifyCache[table][string(k)] = common.Copy(v)
	return nil
}

// Append with cache support, writes to modifyCache only
func (m *MemoryMutationWithCache) Append(table string, key []byte, value []byte) error {
	err := m.memTx.Append(table, key, value)
	if err != nil {
		return err
	}
	// Write to modifyCache only
	if _, ok := m.modifyCache[table]; !ok {
		m.modifyCache[table] = make(map[string][]byte)
	}
	m.modifyCache[table][string(key)] = common.Copy(value)
	return nil
}

// Delete with cache support, modifies modifyCache only
func (m *MemoryMutationWithCache) Delete(table string, k []byte) error {
	// Update modifyCache only
	if modKeys, ok := m.modifyCache[table]; ok {
		delete(modKeys, string(k))
	}
	return m.MemoryMutation.Delete(table, k)
}

// Commit with cache support, clears modifyCache
func (m *MemoryMutationWithCache) Commit() error {
	err := m.MemoryMutation.Commit()
	if err != nil {
		return err
	}
	// Clear modifyCache, cache remains unchanged
	m.modifyCache = make(map[string]map[string][]byte)
	return nil
}

// Rollback with cache support, clears modifyCache
func (m *MemoryMutationWithCache) Rollback() {
	m.MemoryMutation.Rollback()
	// Clear modifyCache, cache remains unchanged
	m.modifyCache = make(map[string]map[string][]byte)
}

// ClearBucket with cache support, clears modifyCache entry
func (m *MemoryMutationWithCache) ClearBucket(bucket string) error {
	err := m.MemoryMutation.ClearBucket(bucket)
	if err != nil {
		return err
	}
	// Clear from modifyCache only
	delete(m.modifyCache, bucket)
	return nil
}
