package membatch

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ledgerwatch/erigon-lib/etl"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/log/v3"
)

type MapmutationWithDoubleCache struct {
	modifiedCache  map[string]map[string][]byte // table -> key -> value ie. blocks -> hash -> blockBod
	immutableCache map[string]map[string][]byte
	db             kv.Tx
	quit           <-chan struct{}
	clean          func()
	mu             sync.RWMutex
	size           int
	count          uint64
	tmpdir         string
	logger         log.Logger
}

// NewBatch - starts in-mem batch
//
// Common pattern:
//
// batch := db.NewBatch()
// defer batch.Close()
// ... some calculations on `batch`
// batch.Commit()
func NewHashCacheBatch(tx kv.Tx, quit <-chan struct{}, tmpdir string, logger log.Logger) *MapmutationWithDoubleCache {
	clean := func() {}
	if quit == nil {
		ch := make(chan struct{})
		clean = func() { close(ch) }
		quit = ch
	}

	return &MapmutationWithDoubleCache{
		db:            tx,
		modifiedCache: make(map[string]map[string][]byte),
		quit:          quit,
		clean:         clean,
		tmpdir:        tmpdir,
		logger:        logger,
	}
}

func (m *MapmutationWithDoubleCache) getMem(table string, key []byte) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.modifiedCache[table]; ok {
		if value, ok := m.modifiedCache[table][*(*string)(unsafe.Pointer(&key))]; ok {
			return value, ok
		}
	}

	if _, ok := m.immutableCache[table]; !ok {
		return nil, false
	}
	if value, ok := m.immutableCache[table][*(*string)(unsafe.Pointer(&key))]; ok {
		return value, ok
	}

	return nil, false
}

func (m *MapmutationWithDoubleCache) IncrementSequence(bucket string, amount uint64) (res uint64, err error) {
	v, ok := m.getMem(kv.Sequence, []byte(bucket))
	if !ok && m.db != nil {
		v, err = m.db.GetOne(kv.Sequence, []byte(bucket))
		if err != nil {
			return 0, err
		}
	}

	var currentV uint64 = 0
	if len(v) > 0 {
		currentV = binary.BigEndian.Uint64(v)
	}

	newVBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(newVBytes, currentV+amount)
	if err = m.Put(kv.Sequence, []byte(bucket), newVBytes); err != nil {
		return 0, err
	}

	return currentV, nil
}
func (m *MapmutationWithDoubleCache) ReadSequence(bucket string) (res uint64, err error) {
	v, ok := m.getMem(kv.Sequence, []byte(bucket))
	if !ok && m.db != nil {
		v, err = m.db.GetOne(kv.Sequence, []byte(bucket))
		if err != nil {
			return 0, err
		}
	}
	var currentV uint64 = 0
	if len(v) > 0 {
		currentV = binary.BigEndian.Uint64(v)
	}

	return currentV, nil
}

// Can only be called from the worker thread
func (m *MapmutationWithDoubleCache) GetOne(table string, key []byte) ([]byte, error) {
	if value, ok := m.getMem(table, key); ok {
		return value, nil
	}
	if m.db != nil {
		// TODO: simplify when tx can no longer be parent of mutation
		value, err := m.db.GetOne(table, key)
		if err != nil {
			return nil, err
		}
		return value, nil
	}
	return nil, nil
}

func (m *MapmutationWithDoubleCache) Last(table string) ([]byte, []byte, error) {
	c, err := m.db.Cursor(table)
	if err != nil {
		return nil, nil, err
	}
	defer c.Close()
	return c.Last()
}

func (m *MapmutationWithDoubleCache) Has(table string, key []byte) (bool, error) {
	if _, ok := m.getMem(table, key); ok {
		return ok, nil
	}
	if m.db != nil {
		return m.db.Has(table, key)
	}
	return false, nil
}

// puts a table key with a value and if the table is not found then it appends a table
func (m *MapmutationWithDoubleCache) Put(table string, k, v []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.modifiedCache[table]; !ok {
		m.modifiedCache[table] = make(map[string][]byte)
	}

	stringKey := string(k)

	var ok bool
	if _, ok = m.modifiedCache[table][stringKey]; ok {
		m.size += len(v) - len(m.modifiedCache[table][stringKey])
		m.modifiedCache[table][stringKey] = v
		return nil
	}
	m.modifiedCache[table][stringKey] = v
	m.size += len(k) + len(v)
	m.count++

	return nil
}

func (m *MapmutationWithDoubleCache) Append(table string, key []byte, value []byte) error {
	return m.Put(table, key, value)
}

func (m *MapmutationWithDoubleCache) AppendDup(table string, key []byte, value []byte) error {
	return m.Put(table, key, value)
}

func (m *MapmutationWithDoubleCache) BatchSize() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.size
}

func (m *MapmutationWithDoubleCache) ForEach(bucket string, fromPrefix []byte, walker func(k, v []byte) error) error {
	m.panicOnEmptyDB()

	// take a readlock on the cache
	m.mu.RLock()
	defer m.mu.RUnlock()

	tmpCache := make(map[string]map[string][]byte, len(m.immutableCache))
	for table, bucket := range m.immutableCache {
		if _, ok := tmpCache[table]; !ok {
			tmpCache[table] = make(map[string][]byte, len(bucket)*2)
		}

		for k, v := range bucket {
			tmpCache[table][k] = v
		}
	}

	for table, bucket := range m.modifiedCache {
		if _, ok := tmpCache[table]; !ok {
			tmpCache[table] = make(map[string][]byte, len(bucket))
		}

		for k, v := range bucket {
			tmpCache[table][k] = v
		}
	}

	// if the bucket is not in the cache, then we can just use the db
	if _, ok := tmpCache[bucket]; !ok {
		return m.db.ForEach(bucket, fromPrefix, walker)
	}

	// create an ordered structure to hold our data
	keys := make([]string, 0, len(tmpCache[bucket]))
	values := make(map[string][]byte)

	// otherwise fill the ordered data structure
	// range the db table
	err := m.db.ForEach(bucket, fromPrefix, func(k, v []byte) error {
		keys = append(keys, string(k))
		values[string(k)] = v
		return nil
	})
	if err != nil {
		return err
	}

	// range the cache, and perform an ordered insert to the local structure
	for k, v := range tmpCache[bucket] {
		// ordered insert to keys
		index := sort.Search(len(keys), func(i int) bool { return keys[i] >= k })
		keys = append(keys, "")
		copy(keys[index+1:], keys[index:])
		keys[index] = k

		// collect value in map
		values[k] = v
	}

	// temp check to see if we are in order
	sort.SliceStable(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})

	// range the ordered structure and call the walker
	for _, k := range keys {
		// only where the prefix matches
		sp := string(fromPrefix)
		if !strings.HasPrefix(k, sp) {
			continue
		}
		err := walker([]byte(k), values[k])
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *MapmutationWithDoubleCache) ForPrefix(bucket string, prefix []byte, walker func(k, v []byte) error) error {
	m.panicOnEmptyDB()
	return m.db.ForPrefix(bucket, prefix, walker)
}

func (m *MapmutationWithDoubleCache) ForAmount(bucket string, prefix []byte, amount uint32, walker func(k, v []byte) error) error {
	m.panicOnEmptyDB()
	return m.db.ForAmount(bucket, prefix, amount, walker)
}

func (m *MapmutationWithDoubleCache) Delete(table string, k []byte) error {
	return m.Put(table, k, nil)
}

func (m *MapmutationWithDoubleCache) doCommit(tx kv.RwTx) error {
	logEvery := time.NewTicker(30 * time.Second)
	defer logEvery.Stop()
	count := 0
	total := float64(m.count)
	for table, bucket := range m.modifiedCache {
		collector := etl.NewCollector("", m.tmpdir, etl.NewSortableBuffer(etl.BufferOptimalSize/2), m.logger)
		defer collector.Close()
		collector.SortAndFlushInBackground(true)
		for key, value := range bucket {
			if err := collector.Collect([]byte(key), value); err != nil {
				return err
			}
			count++
			select {
			default:
			case <-logEvery.C:
				progress := fmt.Sprintf("%.1fM/%.1fM", float64(count)/1_000_000, total/1_000_000)
				m.logger.Info("Write to db", "progress", progress, "current table", table)
				tx.CollectMetrics()
			}
		}
		if err := collector.Load(tx, table, etl.IdentityLoadFunc, etl.TransformArgs{Quit: m.quit}); err != nil {
			return err
		}
		collector.Close()
	}

	tx.CollectMetrics()
	return nil
}

func (m *MapmutationWithDoubleCache) Flush(ctx context.Context, tx kv.RwTx) error {
	if tx == nil {
		return errors.New("rwTx needed")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.doCommit(tx); err != nil {
		return err
	}

	m.modifiedCache = map[string]map[string][]byte{}
	m.immutableCache = map[string]map[string][]byte{}
	m.size = 0
	m.count = 0
	return nil
}

func (m *MapmutationWithDoubleCache) Close() {
	if m.clean == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.modifiedCache = map[string]map[string][]byte{}
	m.immutableCache = map[string]map[string][]byte{}
	m.size = 0
	m.count = 0
	m.size = 0

	m.clean()
	m.clean = nil

}
func (m *MapmutationWithDoubleCache) Commit() error { panic("not db txn, use .Flush method") }
func (m *MapmutationWithDoubleCache) Rollback()     { panic("not db txn, use .Close method") }

func (m *MapmutationWithDoubleCache) panicOnEmptyDB() {
	if m.db == nil {
		panic("Not implemented")
	}
}

func (m *MapmutationWithDoubleCache) SetCache(cache map[string]map[string][]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.immutableCache = cache
}

func (m *MapmutationWithDoubleCache) RetrieveAndCleanSmtCache(smtTables []string) map[string]map[string][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()

	deltaTargetCached := make(map[string]map[string][]byte, len(smtTables))

	for _, table := range smtTables {
		if bucket, ok := m.modifiedCache[table]; ok {
			deltaTargetCached[table] = bucket

			delete(m.modifiedCache, table)
		}
	}

	return deltaTargetCached
}
