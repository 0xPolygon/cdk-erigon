package server

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/erigontech/mdbx-go/mdbx"
	"github.com/gateway-fm/zkevm-data-streamer/datastreamer"
)

// These constants define the MDBX tables we'll use
const (
	// Main tables
	TableEntries   = "entries"   // Store all entries sequentially
	TableBookmarks = "bookmarks" // Store bookmarks for fast seeking
	TableMetadata  = "metadata"  // Store header information

	// Index tables
	TableBlockIndex = "block_index" // Index blocks by number
	TableBatchIndex = "batch_index" // Index batches by number
)

// MDBX flags and options
const (

	// Flags for Env.Open
	NoTLS = 0x200000 // Don't use thread-local storage

	// Flags for Txn.OpenDBI
	Create = 0x40000 // Create DB if not already existing

	// Fixed StreamType value
	StreamTypeValue = 1 // Always use StreamType 1 (sequencer) in this implementation
)

// MDBXStreamStore implements StreamStore using MDBX
type MDBXStreamStore struct {
	env           *mdbx.Env
	dbi           mdbx.DBI
	bookmarksDbi  mdbx.DBI
	metadataDbi   mdbx.DBI
	header        datastreamer.HeaderEntry
	mutex         sync.RWMutex
	inTransaction bool
	txn           *mdbx.Txn // Current transaction
	streamChannel chan datastreamer.StreamAO
	atomicOp      datastreamer.StreamAO
}

// SetStreamChannel sets the channel for atomic operation notifications
func (ms *MDBXStreamStore) SetStreamChannel(ch chan datastreamer.StreamAO) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()
	ms.streamChannel = ch
}

func (ms *MDBXStreamStore) GetNextEntry() uint64 {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()
	return ms.header.TotalEntries
}

func (ms *MDBXStreamStore) PrintDumpBookmarks() error {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	// Create read transaction
	txn, err := ms.env.BeginTxn(nil, mdbx.Readonly)
	if err != nil {
		return err
	}
	defer txn.Abort()

	// Create cursor for bookmarks
	cursor, err := txn.OpenCursor(ms.bookmarksDbi)
	if err != nil {
		return err
	}
	defer cursor.Close()

	// Walk through all bookmarks
	key, val, err := cursor.Get(nil, nil, mdbx.First)
	for ; err == nil; key, val, err = cursor.Get(nil, nil, mdbx.Next) {
		if key == nil {
			break
		}
		entryNum := binary.BigEndian.Uint64(val)
		fmt.Printf("Bookmark: %X -> Entry %d\n", key, entryNum)
	}

	if err != nil && err != mdbx.NotFound {
		return err
	}

	return nil
}

// NewMDBXStreamStore creates a new MDBX-based stream store
func NewMDBXStreamStore(config *StreamStoreConfig) (*MDBXStreamStore, error) {

	// Create environment
	env, err := mdbx.NewEnv()
	if err != nil {
		return nil, err
	}

	// Configure MDBX
	if err := env.SetOption(mdbx.OptMaxDB, uint64(config.MDBXMaxDBS)); err != nil {
		env.Close()
		return nil, fmt.Errorf("failed to set maxDBs: %w", err)
	}

	const pageSize = 4096
	err = env.SetGeometry(-1, -1, 64*1024*pageSize, -1, -1, pageSize)
	if err != nil {
		env.Close()
		return nil, fmt.Errorf("failed to set geometry: %w", err)
	}

	file := config.FilePath + ".mdbx"
	// create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		env.Close()
		return nil, fmt.Errorf("failed to data-stream directory: %w", err)
	}

	if err := env.Open(file, mdbx.Create, 0644); err != nil {
		env.Close()
		return nil, err
	}

	// Initialize store
	store := &MDBXStreamStore{
		env: env,
		header: datastreamer.HeaderEntry{
			Version:      3,
			SystemID:     config.SystemID,
			TotalEntries: 0,
			TotalLength:  0,
		},
	}

	// Open DBIs in a transaction
	txn, err := env.BeginTxn(nil, 0)
	if err != nil {
		env.Close()
		return nil, err
	}

	// Create DBIs
	store.dbi, err = txn.OpenDBISimple(TableEntries, Create)
	if err != nil {
		txn.Abort()
		env.Close()
		return nil, err
	}

	store.bookmarksDbi, err = txn.OpenDBISimple(TableBookmarks, Create)
	if err != nil {
		txn.Abort()
		env.Close()
		return nil, err
	}

	store.metadataDbi, err = txn.OpenDBISimple(TableMetadata, Create)
	if err != nil {
		txn.Abort()
		env.Close()
		return nil, err
	}

	// Try to load existing header
	headerVal, err := txn.Get(store.metadataDbi, []byte("header"))
	if err == nil && len(headerVal) > 0 {
		existingHeader, err := decodeHeader(headerVal)
		if err == nil {
			store.header = *existingHeader
		}
		// If there's an error decoding, we'll keep the default header
	}

	// Commit transaction
	commit, err := txn.Commit()
	if err != nil {
		env.Close()
		return nil, err
	}

	// Ignore commit latency value: TODO add this to metrics
	_ = commit

	return store, nil
}

// AddStreamEntry adds a new entry to the stream
func (ms *MDBXStreamStore) AddStreamEntry(entryType datastreamer.EntryType, data []byte) (uint64, error) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if !ms.inTransaction {
		return 0, errors.New("must be in transaction to add entries")
	}

	// Create entry
	entryNum := ms.header.TotalEntries
	entry := datastreamer.FileEntry{
		Type:   entryType,
		Length: uint32(len(data) + 17), // 17 is the header size
		Number: entryNum,
		Data:   data,
	}

	// Encode and store entry
	keyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBytes, entryNum)

	entryBytes, err := encodeFileEntry(entry)
	if err != nil {
		return 0, err
	}

	if err := ms.txn.Put(ms.dbi, keyBytes, entryBytes, 0); err != nil {
		return 0, err
	}

	// Update header (will be saved on commit)
	ms.header.TotalEntries++
	ms.header.TotalLength += uint64(entry.Length)

	return entryNum, nil
}

// AddStreamBookmark adds a new bookmark to the stream
func (ms *MDBXStreamStore) AddStreamBookmark(data []byte) (uint64, error) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if !ms.inTransaction {
		return 0, errors.New("must be in transaction to add bookmarks")
	}

	// Create bookmark entry
	entryNum := ms.header.TotalEntries
	entry := datastreamer.FileEntry{
		Type:   176,                    // Bookmark type
		Length: uint32(len(data) + 17), // 17 is the header size
		Number: entryNum,
		Data:   data,
	}

	// Store entry in main table
	entryKeyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(entryKeyBytes, entryNum)

	entryBytes, err := encodeFileEntry(entry)
	if err != nil {
		return 0, err
	}

	if err := ms.txn.Put(ms.dbi, entryKeyBytes, entryBytes, 0); err != nil {
		return 0, err
	}

	// Also store in bookmark table for quick lookup
	entryNumBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(entryNumBytes, entryNum)
	if err := ms.txn.Put(ms.bookmarksDbi, data, entryNumBytes, 0); err != nil {
		return 0, err
	}

	// Update header (will be saved on commit)
	ms.header.TotalEntries++
	ms.header.TotalLength += uint64(entry.Length)

	return entryNum, nil
}

// GetEntry retrieves an entry from the stream
func (ms *MDBXStreamStore) GetEntry(entryNum uint64) (datastreamer.FileEntry, error) {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	// Create read transaction, setting parent if one exists
	txn, err := ms.env.BeginTxn(ms.txn, mdbx.Readonly)
	if err != nil {
		return datastreamer.FileEntry{}, err
	}
	defer txn.Abort() // Ensure the transaction is aborted if not committed

	// Create key
	keyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBytes, entryNum)

	// Get from db
	entryBytes, err := txn.Get(ms.dbi, keyBytes)
	if err != nil {
		if err == mdbx.NotFound {
			return datastreamer.FileEntry{}, fmt.Errorf("entry not found: %d", entryNum)
		}
		return datastreamer.FileEntry{}, err
	}

	// Decode entry
	entry, err := decodeFileEntry(entryBytes)
	if err != nil {
		return datastreamer.FileEntry{}, err
	}

	return entry, nil
}

// GetBookmark retrieves a bookmark from the stream
func (ms *MDBXStreamStore) GetBookmark(data []byte) (uint64, error) {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	// Create read transaction if not in a transaction
	var txn *mdbx.Txn
	var err error
	var shouldAbort bool

	if ms.inTransaction {
		txn = ms.txn
	} else {
		txn, err = ms.env.BeginTxn(nil, mdbx.Readonly)
		if err != nil {
			return 0, err
		}
		shouldAbort = true
		defer func() {
			if shouldAbort {
				txn.Abort()
			}
		}()
	}

	// Get from db
	entryNumBytes, err := txn.Get(ms.bookmarksDbi, data)
	if err != nil {
		if err == mdbx.NotFound {
			return 0, fmt.Errorf("bookmark not found")
		}
		return 0, err
	}

	// If we created a transaction, we need to abort it now
	shouldAbort = false
	if !ms.inTransaction {
		txn.Abort()
	}

	return binary.BigEndian.Uint64(entryNumBytes), nil
}

// StartAtomicOp starts a transaction
func (ms *MDBXStreamStore) StartAtomicOp() error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if ms.inTransaction {
		return errors.New("transaction already in progress")
	}

	// Begin a new transaction
	txn, err := ms.env.BeginTxn(nil, 0)
	if err != nil {
		return err
	}

	ms.txn = txn
	ms.inTransaction = true

	return nil
}

// CommitAtomicOp commits a transaction
func (ms *MDBXStreamStore) CommitAtomicOp() error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if !ms.inTransaction {
		return errors.New("no transaction in progress")
	}

	// Save header
	headerBytes, err := encodeHeader(&ms.header)
	if err != nil {
		ms.txn.Abort()
		ms.txn = nil
		ms.inTransaction = false
		return fmt.Errorf("failed to encode header: %w", err)
	}

	if err := ms.txn.Put(ms.metadataDbi, []byte("header"), headerBytes, 0); err != nil {
		ms.txn.Abort()
		ms.txn = nil
		ms.inTransaction = false
		return fmt.Errorf("failed to save header: %w", err)
	}

	// Commit transaction
	commit, err := ms.txn.Commit()
	if err != nil {
		ms.txn.Abort()
		ms.txn = nil
		ms.inTransaction = false
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Ignore commit latency: TODO add this to metrics
	_ = commit

	// Clean up
	ms.txn = nil
	ms.inTransaction = false

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

	return nil
}

// RollbackAtomicOp rolls back a transaction
func (ms *MDBXStreamStore) RollbackAtomicOp() error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	if !ms.inTransaction {
		return errors.New("no transaction in progress")
	}

	// Abort transaction
	ms.txn.Abort()
	ms.txn = nil
	ms.inTransaction = false

	return nil
}

// GetHeader retrieves the header from the stream
func (ms *MDBXStreamStore) GetHeader() datastreamer.HeaderEntry {
	ms.mutex.RLock()
	defer ms.mutex.RUnlock()

	// Copy of header struct
	return ms.header
}

// TruncateFile truncates the stream to the specified entry
func (ms *MDBXStreamStore) TruncateFile(entryNum uint64) error {
	if err := ms.StartAtomicOp(); err != nil {
		return err
	}

	// Get the current header
	currentTotal := ms.header.TotalEntries

	if entryNum >= currentTotal {
		// Nothing to truncate
		ms.RollbackAtomicOp()
		return nil
	}

	// Create cursor to iterate through entries to delete
	cursor, err := ms.txn.OpenCursor(ms.dbi)
	if err != nil {
		ms.RollbackAtomicOp()
		return err
	}
	defer cursor.Close()

	// Calculate total length adjustment
	var lengthToSubtract uint64 = 0

	// Delete entries from entryNum to the end
	for i := entryNum; i < currentTotal; i++ {
		keyBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(keyBytes, i)

		// Get the entry to calculate length adjustment
		entryBytes, err := ms.txn.Get(ms.dbi, keyBytes)
		if err == nil {
			entry, err := decodeFileEntry(entryBytes)
			if err == nil {
				lengthToSubtract += uint64(entry.Length)

				// If it's a bookmark, also remove from bookmarks table
				if entry.Type == 176 { // Bookmark type
					ms.txn.Del(ms.bookmarksDbi, entry.Data, nil)
				}
			}
		}

		// Delete the entry
		if err := ms.txn.Del(ms.dbi, keyBytes, nil); err != nil {
			ms.RollbackAtomicOp()
			return err
		}
	}

	// Update header
	ms.header.TotalEntries = entryNum
	ms.header.TotalLength -= lengthToSubtract

	return ms.CommitAtomicOp()
}

// IteratorFrom creates an iterator starting from the specified entry
func (ms *MDBXStreamStore) IteratorFrom(entryNum uint64, includeBookmarks bool) (*MDBXStreamStoreIterator, error) {
	return newMDBXStreamStoreIterator(ms, entryNum, includeBookmarks), nil
}

// Helper functions

// encodeHeader encodes a HeaderEntry into bytes
func encodeHeader(header *datastreamer.HeaderEntry) ([]byte, error) {
	// Version(8) + SystemID(8) + TotalEntries(8) + TotalLength(8) = 32 bytes
	result := make([]byte, 32)

	// Write Version (bytes 0-8)
	binary.LittleEndian.PutUint64(result[0:8], uint64(header.Version))

	// Write SystemID (bytes 8-16)
	binary.LittleEndian.PutUint64(result[8:16], header.SystemID)

	// Write TotalEntries (bytes 16-24)
	binary.LittleEndian.PutUint64(result[16:24], header.TotalEntries)

	// Write TotalLength (bytes 24-32)
	binary.LittleEndian.PutUint64(result[24:32], header.TotalLength)

	return result, nil
}

// decodeHeader decodes bytes into a HeaderEntry
func decodeHeader(data []byte) (*datastreamer.HeaderEntry, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("header data too short: got %d bytes, expected at least 32", len(data))
	}

	header := &datastreamer.HeaderEntry{
		// Read from encoded data
		Version:      uint8(binary.LittleEndian.Uint64(data[0:8])),
		SystemID:     binary.LittleEndian.Uint64(data[8:16]),
		TotalEntries: binary.LittleEndian.Uint64(data[16:24]),
		TotalLength:  binary.LittleEndian.Uint64(data[24:32]),
	}

	return header, nil
}

// encodeFileEntry encodes a FileEntry to bytes
func encodeFileEntry(entry datastreamer.FileEntry) ([]byte, error) {
	result := make([]byte, 17+len(entry.Data))
	result[0] = 2 // PacketType (2 for data)
	binary.BigEndian.PutUint32(result[1:5], entry.Length)
	binary.BigEndian.PutUint32(result[5:9], uint32(entry.Type))
	binary.BigEndian.PutUint64(result[9:17], entry.Number)
	copy(result[17:], entry.Data)
	return result, nil
}

// decodeFileEntry decodes bytes to a FileEntry
func decodeFileEntry(data []byte) (datastreamer.FileEntry, error) {
	if len(data) < 17 {
		return datastreamer.FileEntry{}, errors.New("invalid file entry data")
	}

	length := binary.BigEndian.Uint32(data[1:5])
	entryType := datastreamer.EntryType(binary.BigEndian.Uint32(data[5:9]))
	number := binary.BigEndian.Uint64(data[9:17])
	entryData := data[17:]

	return datastreamer.FileEntry{
		Type:   entryType,
		Length: length,
		Number: number,
		Data:   entryData,
	}, nil
}

// MDBXStreamStoreIterator implements the datastreamer.StorageIterator interface
type MDBXStreamStoreIterator struct {
	store            *MDBXStreamStore
	currentEntryNum  uint64
	maxEntryNum      uint64
	includeBookmarks bool
	txn              *mdbx.Txn
	cursor           *mdbx.Cursor
	currentEntry     datastreamer.FileEntry
	hasCurrentEntry  bool
	err              error
}

// IteratorNext advances the iterator to the next item
func (it *MDBXStreamStoreIterator) Next() (bool, error) {
	if it.currentEntryNum > it.maxEntryNum {
		it.hasCurrentEntry = false
		return false, nil
	}

	// Initialize transaction and cursor if needed
	if it.txn == nil {
		var err error
		it.txn, err = it.store.env.BeginTxn(nil, mdbx.Readonly)
		if err != nil {
			it.hasCurrentEntry = false
			it.err = err
			return false, err
		}

		it.cursor, err = it.txn.OpenCursor(it.store.dbi)
		if err != nil {
			it.txn.Abort()
			it.txn = nil
			it.hasCurrentEntry = false
			it.err = err
			return false, err
		}
	}

	// Create key for current entry
	keyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBytes, it.currentEntryNum)

	// Get entry from db
	entryBytes, err := it.txn.Get(it.store.dbi, keyBytes)
	if err != nil {
		if err == mdbx.NotFound {
			// Skip to next entry
			it.currentEntryNum++
			return it.Next()
		}
		it.cursor.Close()
		it.txn.Abort()
		it.txn = nil
		it.cursor = nil
		it.hasCurrentEntry = false
		it.err = err
		return false, err
	}

	// Decode entry
	entry, err := decodeFileEntry(entryBytes)
	if err != nil {
		it.cursor.Close()
		it.txn.Abort()
		it.txn = nil
		it.cursor = nil
		it.hasCurrentEntry = false
		it.err = err
		return false, err
	}

	it.currentEntryNum++

	// Skip bookmarks if not including them
	if !it.includeBookmarks && entry.Type == 176 { // 176 is bookmark type
		return it.Next()
	}

	it.currentEntry = entry
	it.hasCurrentEntry = true
	return true, nil
}

// IteratorEnd cleans up iterator resources
func (it *MDBXStreamStoreIterator) End() {
	if it.cursor != nil {
		it.cursor.Close()
		it.cursor = nil
	}

	if it.txn != nil {
		it.txn.Abort()
		it.txn = nil
	}

	it.hasCurrentEntry = false
}

// GetEntry returns the current entry
func (it *MDBXStreamStoreIterator) GetEntry() datastreamer.FileEntry {
	return it.currentEntry
}

// newMDBXStreamStoreIterator creates a new iterator for the MDBX-based store
func newMDBXStreamStoreIterator(store *MDBXStreamStore, startEntryNum uint64, includeBookmarks bool) *MDBXStreamStoreIterator {
	// Get max entry num
	maxEntryNum := store.GetHeader().TotalEntries - 1

	// We initialize txn and cursor on first use to avoid issues with transaction lifetimes
	return &MDBXStreamStoreIterator{
		store:            store,
		currentEntryNum:  startEntryNum,
		maxEntryNum:      maxEntryNum,
		includeBookmarks: includeBookmarks,
	}
}

// GetEntryNumberLimit returns the maximum entry number in the store
func (it *MDBXStreamStoreIterator) GetEntryNumberLimit() uint64 {
	return it.maxEntryNum + 1
}

// NextFileEntry returns the next file entry from the iterator
func (it *MDBXStreamStoreIterator) NextFileEntry() (*types.FileEntry, error) {
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
func (it *MDBXStreamStoreIterator) Close() {
	it.End()
}

// BookmarkPrintDump prints debug information about bookmarks
func (ms *MDBXStreamStore) BookmarkPrintDump() {
	// This is a no-op for MDBX implementation
	// Only needed to satisfy the StreamStore interface
}

// GetFirstEventAfterBookmark gets the first event after a bookmark
func (ms *MDBXStreamStore) GetFirstEventAfterBookmark(bookmark []byte) (datastreamer.FileEntry, error) {
	// Get bookmark entry number
	entryNum, err := ms.GetBookmark(bookmark)
	if err != nil {
		return datastreamer.FileEntry{}, err
	}

	// Create iterator
	iter, err := ms.IteratorFrom(entryNum, false)
	if err != nil {
		return datastreamer.FileEntry{}, err
	}

	// Skip the bookmark entry itself
	_, err = iter.NextFileEntry()
	if err != nil {
		return datastreamer.FileEntry{}, err
	}

	// Get the next entry
	entry, err := iter.NextFileEntry()
	if err != nil {
		return datastreamer.FileEntry{}, err
	}

	if entry == nil {
		return datastreamer.FileEntry{}, errors.New("no entries after bookmark")
	}

	// Convert back to datastreamer.FileEntry
	return datastreamer.FileEntry{
		Type:   datastreamer.EntryType(entry.EntryType),
		Length: entry.Length,
		Number: entry.EntryNum,
		Data:   entry.Data,
	}, nil
}

// GetDataBetweenBookmarks gets data between two bookmarks
func (ms *MDBXStreamStore) GetDataBetweenBookmarks(bookmarkFrom, bookmarkTo []byte) ([]byte, error) {
	// Get bookmark entry numbers
	fromEntryNum, err := ms.GetBookmark(bookmarkFrom)
	if err != nil {
		return nil, err
	}

	toEntryNum, err := ms.GetBookmark(bookmarkTo)
	if err != nil {
		return nil, err
	}

	// Create an iterator
	iter, err := ms.IteratorFrom(fromEntryNum, false)
	if err != nil {
		return nil, err
	}

	// Collect all data between bookmarks
	var result []byte
	for {
		entry, err := iter.NextFileEntry()
		if err != nil {
			return nil, err
		}

		if entry == nil || entry.EntryNum > toEntryNum {
			break
		}

		result = append(result, entry.Data...)
	}

	return result, nil
}

// UpdateEntryData updates the data for an entry
func (ms *MDBXStreamStore) UpdateEntryData(entryNum uint64, entryType datastreamer.EntryType, data []byte) error {
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

	if err := ms.txn.Put(ms.dbi, keyBytes, entryBytes, 0); err != nil {
		ms.RollbackAtomicOp()
		return err
	}

	return ms.CommitAtomicOp()
}

// GetIterator returns a file iterator for the MDBX stream store
func (ms *MDBXStreamStore) GetIterator(entryNum uint64, readOnly bool) (datastreamer.StorageIterator, error) {
	// Create a real iterator using our existing implementation
	iterator, err := ms.IteratorFrom(entryNum, true) // Include bookmarks for compatibility
	if err != nil {
		return nil, err
	}

	// Return the iterator as the required interface type
	return iterator, nil
}

// AddFileEntry adds a file entry directly to the stream
func (ms *MDBXStreamStore) AddFileEntry(e datastreamer.FileEntry) error {
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

	if err := ms.txn.Put(ms.dbi, keyBytes, entryBytes, 0); err != nil {
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
func (ms *MDBXStreamStore) WriteHeaderEntry() error {
	if !ms.inTransaction {
		if err := ms.StartAtomicOp(); err != nil {
			return err
		}
		defer ms.CommitAtomicOp()
	}

	headerBytes, err := encodeHeader(&ms.header)
	if err != nil {
		return fmt.Errorf("failed to encode header: %w", err)
	}

	if err := ms.txn.Put(ms.metadataDbi, []byte("header"), headerBytes, 0); err != nil {
		return fmt.Errorf("failed to save header: %w", err)
	}

	return nil
}

// Close closes the MDBX environment and releases resources
func (ms *MDBXStreamStore) Close() error {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	// If there's an active transaction, abort it
	if ms.inTransaction && ms.txn != nil {
		ms.txn.Abort()
		ms.txn = nil
		ms.inTransaction = false
	}

	// Close environment
	if ms.env != nil {
		ms.env.Close()
		ms.env = nil
	}

	return nil
}
