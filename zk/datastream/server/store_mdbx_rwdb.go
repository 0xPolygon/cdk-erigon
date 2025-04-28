package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/kv/mdbx"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/gateway-fm/zkevm-data-streamer/datastreamer"
)

// MDBXRwDBStreamStore implements StreamStore using kv.RwDB interface
type MDBXRwDBStreamStore struct {
	db            kv.RwDB
	header        datastreamer.HeaderEntry
	mutex         sync.RWMutex
	inTransaction bool
	currentTx     kv.RwTx
	streamChannel chan datastreamer.StreamAO
	atomicOp      datastreamer.StreamAO
	ctx           context.Context
	logger        log.Logger
}

const (
	// Atomic operation Status
	aoNone datastreamer.AOStatus = iota + 1
	aoStarted
	aoCommitting
	aoRollbacking
)

// NewMDBXRwDBStreamStore creates a new kv.RwDB-based stream store
func NewMDBXRwDBStreamStore(config *StreamStoreConfig) (*MDBXRwDBStreamStore, error) {
	ctx := context.Background()

	// Use the logger from the config
	logger := config.Logger
	if logger == nil {
		logger = log.New() // Use default logger if none provided
	}

	// Configure database
	opts := mdbx.NewMDBX(logger).
		Path(config.FilePath + ".mdbx")

	// Open database
	db, err := opts.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open MDBX database: %w", err)
	}

	// Initialize store
	store := &MDBXRwDBStreamStore{
		db:     db,
		header: datastreamer.NewHeader(3, config.SystemID, StreamTypeValue),
		ctx:    ctx,
		logger: logger,
	}

	// Create tables if they don't exist
	err = db.Update(ctx, func(tx kv.RwTx) error {
		// Create necessary buckets
		for _, table := range []string{TableEntries, TableBookmarks, TableMetadata} {
			if err := tx.CreateBucket(table); err != nil {
				return fmt.Errorf("failed to create bucket %s: %w", table, err)
			}
		}

		// Try to load existing header
		headerVal, err := tx.GetOne(TableMetadata, []byte("header"))
		if err == nil && len(headerVal) > 0 {
			existingHeader, err := decodeHeader(headerVal)
			if err == nil {
				store.header = *existingHeader
			}
			// If there's an error decoding, we'll keep the default header
		}

		return nil
	})

	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return store, nil
}

// SetStreamChannel sets the channel for atomic operation notifications
func (ms *MDBXRwDBStreamStore) SetStreamChannel(ch chan datastreamer.StreamAO) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	ms.streamChannel = ch
}

func (ms *MDBXRwDBStreamStore) GetNextEntry() uint64 {
	return ms.header.TotalEntries
}

func (ms *MDBXRwDBStreamStore) PrintDumpBookmarks() error {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	return ms.db.View(ms.ctx, func(tx kv.Tx) error {
		cursor, err := tx.Cursor(TableBookmarks)
		if err != nil {
			return err
		}
		defer cursor.Close()

		// Walk through all bookmarks
		for k, v, err := cursor.First(); k != nil; k, v, err = cursor.Next() {
			if err != nil {
				return err
			}
			entryNum := binary.BigEndian.Uint64(v)
			fmt.Printf("Bookmark: %X -> Entry %d\n", k, entryNum)
		}

		return nil
	})
}

// addToStream handles common entry storage logic
func (ms *MDBXRwDBStreamStore) addToStream(entryType datastreamer.EntryType, data []byte) (uint64, error) {
	// Create entry
	entryNum := ms.header.TotalEntries
	entry := datastreamer.NewFileEntry(datastreamer.PtData, entryType, entryNum, data)

	// Encode and store entry
	keyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBytes, entryNum)

	entryBytes, err := encodeFileEntry(entry)
	if err != nil {
		return 0, err
	}

	if err := ms.currentTx.Put(TableEntries, keyBytes, entryBytes); err != nil {
		return 0, err
	}

	// Save the entry in the server's atomic operation tracking
	ms.atomicOp.Entries = append(ms.atomicOp.Entries, entry)

	// Update header (will be saved on commit)
	ms.header.TotalEntries++
	ms.header.TotalLength += uint64(entry.Length)

	return entryNum, nil
}

// AddStreamEntry adds a new entry to the stream
func (ms *MDBXRwDBStreamStore) AddStreamEntry(entryType datastreamer.EntryType, data []byte) (uint64, error) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if !ms.inTransaction {
		return 0, errors.New("must be in transaction to add entries")
	}

	return ms.addToStream(entryType, data)
}

// AddStreamBookmark adds a new bookmark to the stream
func (ms *MDBXRwDBStreamStore) AddStreamBookmark(data []byte) (uint64, error) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if !ms.inTransaction {
		return 0, errors.New("must be in transaction to add bookmarks")
	}

	entryNum, err := ms.addToStream(176, data) // 176 is bookmark type
	if err != nil {
		return 0, err
	}

	// Also store in bookmark table for quick lookup
	entryNumBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(entryNumBytes, entryNum)
	if err := ms.currentTx.Put(TableBookmarks, data, entryNumBytes); err != nil {
		return 0, err
	}

	return entryNum, nil
}

// GetEntry retrieves an entry from the stream
func (ms *MDBXRwDBStreamStore) GetEntry(entryNum uint64) (datastreamer.FileEntry, error) {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	var entry datastreamer.FileEntry

	// Function to get the entry using a transaction
	getEntryFn := func(tx kv.Tx) error {
		// Create key
		keyBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(keyBytes, entryNum)

		// Get from db
		entryBytes, err := tx.GetOne(TableEntries, keyBytes)
		if err != nil {
			return err
		}
		if entryBytes == nil {
			return fmt.Errorf("entry not found: %d", entryNum)
		}

		// Decode entry
		decodedEntry, err := decodeFileEntry(entryBytes)
		if err != nil {
			return err
		}

		entry = decodedEntry
		return nil
	}

	// If we're in a transaction, use the current transaction
	if ms.inTransaction {
		err := getEntryFn(ms.currentTx)
		return entry, err
	}

	// Otherwise, start a new read transaction
	err := ms.db.View(ms.ctx, func(tx kv.Tx) error {
		return getEntryFn(tx)
	})

	return entry, err
}

// GetBookmark retrieves a bookmark from the stream
func (ms *MDBXRwDBStreamStore) GetBookmark(data []byte) (uint64, error) {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	var entryNum uint64

	// Function to get the bookmark using a transaction
	getBookmarkFn := func(tx kv.Tx) error {
		// Get from db
		entryNumBytes, err := tx.GetOne(TableBookmarks, data)
		if err != nil {
			return err
		}
		if entryNumBytes == nil {
			return fmt.Errorf("bookmark not found")
		}

		entryNum = binary.BigEndian.Uint64(entryNumBytes)
		return nil
	}

	// If we're in a transaction, use the current transaction
	if ms.inTransaction {
		err := getBookmarkFn(ms.currentTx)
		return entryNum, err
	}

	// Otherwise, start a new read transaction
	err := ms.db.View(ms.ctx, func(tx kv.Tx) error {
		return getBookmarkFn(tx)
	})

	return entryNum, err
}

// StartAtomicOp starts a transaction
func (ms *MDBXRwDBStreamStore) StartAtomicOp() error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if ms.inTransaction {
		return fmt.Errorf("already in transaction")
	}

	// Begin a new write transaction
	tx, err := ms.db.BeginRw(ms.ctx)
	if err != nil {
		return err
	}

	ms.currentTx = tx
	ms.inTransaction = true

	// Reset atomic operation
	ms.atomicOp = datastreamer.StreamAO{
		Status:     aoStarted,
		StartEntry: ms.GetNextEntry(),
		Entries:    []datastreamer.FileEntry{},
	}

	return nil
}

// CommitAtomicOp commits the current transaction
func (ms *MDBXRwDBStreamStore) CommitAtomicOp() error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if !ms.inTransaction {
		return fmt.Errorf("not in transaction")
	}

	// Save the header
	if err := ms.WriteHeaderEntry(); err != nil {
		ms.currentTx.Rollback()
		ms.inTransaction = false
		ms.currentTx = nil
		return err
	}

	// Commit the transaction
	if err := ms.currentTx.Commit(); err != nil {
		ms.currentTx.Rollback()
		ms.inTransaction = false
		ms.currentTx = nil
		return err
	}

	if ms.streamChannel != nil {
		// Do broadcast of the committed atomic operation to the stream clients
		atomic := datastreamer.StreamAO{
			Status:     ms.atomicOp.Status,
			StartEntry: ms.atomicOp.StartEntry,
		}
		atomic.Entries = make([]datastreamer.FileEntry, len(ms.atomicOp.Entries))
		copy(atomic.Entries, ms.atomicOp.Entries)

		ms.streamChannel <- atomic
	}
	ms.atomicOp.Entries = ms.atomicOp.Entries[:0]
	ms.atomicOp.Status = aoNone
	ms.inTransaction = false
	ms.currentTx = nil
	return nil
}

// RollbackAtomicOp aborts the current transaction
func (ms *MDBXRwDBStreamStore) RollbackAtomicOp() error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if !ms.inTransaction {
		return fmt.Errorf("not in transaction")
	}

	ms.currentTx.Rollback()
	ms.inTransaction = false
	ms.currentTx = nil
	return nil
}

// GetHeader returns the current header
func (ms *MDBXRwDBStreamStore) GetHeader() datastreamer.HeaderEntry {
	return ms.header
}

// TruncateFile truncates the stream to the specified entry number
func (ms *MDBXRwDBStreamStore) TruncateFile(entryNum uint64) error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if ms.inTransaction {
		return fmt.Errorf("cannot truncate while in transaction")
	}

	// We need to get the old header first to calculate total length reduction
	oldHeader := ms.header
	newTotalLength := uint64(0)

	// Start a transaction
	err := ms.db.Update(ms.ctx, func(tx kv.RwTx) error {
		// Read all entries up to entryNum to calculate new total length
		for i := uint64(0); i < entryNum; i++ {
			keyBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(keyBytes, i)

			entryBytes, err := tx.GetOne(TableEntries, keyBytes)
			if err != nil {
				continue // Skip missing entries
			}

			entry, err := decodeFileEntry(entryBytes)
			if err != nil {
				continue // Skip invalid entries
			}

			newTotalLength += uint64(entry.Length)
		}

		// Delete entries from entryNum onwards
		cursor, err := tx.RwCursor(TableEntries)
		if err != nil {
			return err
		}
		defer cursor.Close()

		for i := entryNum; i < oldHeader.TotalEntries; i++ {
			keyBytes := make([]byte, 8)
			binary.BigEndian.PutUint64(keyBytes, i)
			if err := cursor.Delete(keyBytes); err != nil {
				// Skip errors for missing entries
				continue
			}
		}

		// Delete bookmarks that point to deleted entries
		bookmarkCursor, err := tx.RwCursor(TableBookmarks)
		if err != nil {
			return err
		}
		defer bookmarkCursor.Close()

		for k, v, err := bookmarkCursor.First(); k != nil; k, v, err = bookmarkCursor.Next() {
			if err != nil {
				continue // Skip errors
			}

			bookmarkEntryNum := binary.BigEndian.Uint64(v)
			if bookmarkEntryNum >= entryNum {
				if err := bookmarkCursor.Delete(k); err != nil {
					// Skip errors
					continue
				}
			}
		}

		// Update header
		ms.header.TotalEntries = entryNum
		ms.header.TotalLength = newTotalLength

		// Save updated header
		headerBytes, err := encodeHeader(&ms.header)
		if err != nil {
			return err
		}

		return tx.Put(TableMetadata, []byte("header"), headerBytes)
	})

	return err
}

// IteratorFrom returns an iterator starting from the specified entry
func (ms *MDBXRwDBStreamStore) IteratorFrom(entryNum uint64, includeBookmarks bool) (*MDBXRwDBStreamStoreIterator, error) {
	return newMDBXRwDBStreamStoreIterator(ms, entryNum, includeBookmarks), nil
}

// GetFirstEventAfterBookmark gets the first event after a bookmark
func (ms *MDBXRwDBStreamStore) GetFirstEventAfterBookmark(bookmark []byte) (datastreamer.FileEntry, error) {
	// Get entry number from bookmark
	entryNum, err := ms.GetBookmark(bookmark)
	if err != nil {
		return datastreamer.FileEntry{}, err
	}

	// Create iterator from that entry
	iterator, err := ms.IteratorFrom(entryNum, false) // Skip bookmarks
	if err != nil {
		return datastreamer.FileEntry{}, err
	}
	defer iterator.Close()

	// Get first event entry after bookmark
	hasNext, err := iterator.Next()
	if err != nil {
		return datastreamer.FileEntry{}, err
	}

	if !hasNext {
		return datastreamer.FileEntry{}, fmt.Errorf("no events after bookmark")
	}

	return iterator.GetEntry(), nil
}

// GetDataBetweenBookmarks gets all data between two bookmarks
func (ms *MDBXRwDBStreamStore) GetDataBetweenBookmarks(bookmarkFrom, bookmarkTo []byte) ([]byte, error) {
	// Get entry numbers from bookmarks
	fromEntryNum, err := ms.GetBookmark(bookmarkFrom)
	if err != nil {
		return nil, err
	}

	toEntryNum, err := ms.GetBookmark(bookmarkTo)
	if err != nil {
		return nil, err
	}

	if fromEntryNum >= toEntryNum {
		return nil, fmt.Errorf("invalid bookmark range")
	}

	// Collect all data in the range
	var data []byte
	for i := fromEntryNum + 1; i < toEntryNum; i++ {
		entry, err := ms.GetEntry(i)
		if err != nil {
			continue // Skip errors
		}

		// Skip bookmarks
		if entry.Type == 176 { // Bookmark type
			continue
		}

		data = append(data, entry.Data...)
	}

	return data, nil
}

// UpdateEntryData updates the data for an existing entry
func (ms *MDBXRwDBStreamStore) UpdateEntryData(entryNum uint64, entryType datastreamer.EntryType, data []byte) error {
	if err := ms.StartAtomicOp(); err != nil {
		return err
	}

	// Get existing entry
	entry, err := ms.GetEntry(entryNum)
	if err != nil {
		ms.RollbackAtomicOp()
		return err
	}

	// Update entry
	entry.Type = entryType
	entry.Data = data
	entry.Length = uint32(len(data) + 17) // 17 is the header size

	// Store updated entry
	keyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBytes, entryNum)

	entryBytes, err := encodeFileEntry(entry)
	if err != nil {
		ms.RollbackAtomicOp()
		return err
	}

	if err := ms.currentTx.Put(TableEntries, keyBytes, entryBytes); err != nil {
		ms.RollbackAtomicOp()
		return err
	}

	return ms.CommitAtomicOp()
}

// GetIterator returns a file iterator for the stream store
func (ms *MDBXRwDBStreamStore) GetIterator(entryNum uint64, readOnly bool) (datastreamer.StorageIterator, error) {
	// Create a real iterator using our existing implementation
	iterator, err := ms.IteratorFrom(entryNum, true) // Include bookmarks for compatibility
	if err != nil {
		return nil, err
	}

	// Return the iterator as the required interface type
	return iterator, nil
}

// AddFileEntry adds a file entry directly to the stream
func (ms *MDBXRwDBStreamStore) AddFileEntry(e datastreamer.FileEntry) error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if !ms.inTransaction {
		return errors.New("must be in transaction to add entries")
	}

	// Encode and store entry
	keyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBytes, e.Number)

	entryBytes, err := encodeFileEntry(e)
	if err != nil {
		return err
	}

	if err := ms.currentTx.Put(TableEntries, keyBytes, entryBytes); err != nil {
		return err
	}

	// Only update the header if this is a new entry
	if e.Number >= ms.header.TotalEntries {
		ms.header.TotalEntries = e.Number + 1
		ms.header.TotalLength += uint64(e.Length)
	}

	return nil
}

// WriteHeaderEntry writes the current header to storage
func (ms *MDBXRwDBStreamStore) WriteHeaderEntry() error {
	if !ms.inTransaction {
		return errors.New("must be in transaction to write header")
	}

	headerBytes, err := encodeHeader(&ms.header)
	if err != nil {
		return err
	}

	return ms.currentTx.Put(TableMetadata, []byte("header"), headerBytes)
}

// BookmarkPrintDump prints debug information about bookmarks
func (ms *MDBXRwDBStreamStore) BookmarkPrintDump() {
	// This is a no-op implementation
	// Only needed to satisfy the StreamStore interface
}

// Close closes the stream store
func (ms *MDBXRwDBStreamStore) Close() error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if ms.inTransaction {
		ms.currentTx.Rollback()
		ms.inTransaction = false
		ms.currentTx = nil
	}

	// Close the database
	ms.db.Close()
	return nil
}

// MDBXRwDBStreamStoreIterator implements the datastreamer.StorageIterator interface
type MDBXRwDBStreamStoreIterator struct {
	store            *MDBXRwDBStreamStore
	currentEntryNum  uint64
	maxEntryNum      uint64
	includeBookmarks bool
	tx               kv.Tx
	currentEntry     datastreamer.FileEntry
	hasCurrentEntry  bool
	err              error
}

// Next advances the iterator to the next item
func (it *MDBXRwDBStreamStoreIterator) Next() (bool, error) {
	if it.currentEntryNum > it.maxEntryNum {
		it.hasCurrentEntry = false
		return false, nil
	}

	for {
		// Get entry at current position
		entry, err := it.store.GetEntry(it.currentEntryNum)
		it.currentEntryNum++

		if err != nil {
			// Skip missing entries
			if it.currentEntryNum > it.maxEntryNum {
				it.hasCurrentEntry = false
				return false, nil
			}
			continue
		}

		// Skip bookmarks if not including them
		if !it.includeBookmarks && entry.Type == 176 { // 176 is bookmark type
			if it.currentEntryNum > it.maxEntryNum {
				it.hasCurrentEntry = false
				return false, nil
			}
			continue
		}

		it.currentEntry = entry
		it.hasCurrentEntry = true
		return true, nil
	}
}

// End cleans up iterator resources
func (it *MDBXRwDBStreamStoreIterator) End() {
	// Nothing to clean up for this implementation
	it.hasCurrentEntry = false
}

// GetEntry returns the current entry
func (it *MDBXRwDBStreamStoreIterator) GetEntry() datastreamer.FileEntry {
	return it.currentEntry
}

// newMDBXRwDBStreamStoreIterator creates a new iterator for the kv.RwDB-based store
func newMDBXRwDBStreamStoreIterator(store *MDBXRwDBStreamStore, startEntryNum uint64, includeBookmarks bool) *MDBXRwDBStreamStoreIterator {
	// Get max entry num
	maxEntryNum := store.GetHeader().TotalEntries - 1

	return &MDBXRwDBStreamStoreIterator{
		store:            store,
		currentEntryNum:  startEntryNum,
		maxEntryNum:      maxEntryNum,
		includeBookmarks: includeBookmarks,
	}
}

// GetEntryNumberLimit returns the maximum entry number in the store
func (it *MDBXRwDBStreamStoreIterator) GetEntryNumberLimit() uint64 {
	return it.maxEntryNum + 1
}

// NextFileEntry returns the next file entry from the iterator
func (it *MDBXRwDBStreamStoreIterator) NextFileEntry() (*types.FileEntry, error) {
	hasNext, err := it.Next()
	if err != nil {
		return nil, err
	}

	if !hasNext {
		return nil, nil
	}

	// Convert from datastreamer.FileEntry to types.FileEntry for compatibility
	dsEntry := it.GetEntry()
	return &types.FileEntry{
		PacketType: uint8(dsEntry.Type),
		Length:     dsEntry.Length,
		EntryType:  types.EntryType(dsEntry.Type),
		EntryNum:   dsEntry.Number,
		Data:       dsEntry.Data,
	}, nil
}

// Close closes the iterator and frees associated resources
func (it *MDBXRwDBStreamStoreIterator) Close() {
	it.End()
}
