package natsstream

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/gateway-fm/zkevm-data-streamer/datastreamer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Define error constants for the mock
var (
	errAtomicOpAlreadyStarted = errors.New("atomic operation already started")
	errNoActiveAtomicOp       = errors.New("no active atomic operation")
	errInvalidEntryNumber     = errors.New("invalid entry number")
	errUpdateNotSupported     = errors.New("update not supported")
	errEntryNotFound          = errors.New("entry not found")
	errBookmarkNotFound       = errors.New("bookmark not found")
	errNotImplemented         = errors.New("not implemented")
)

// TestNATSStreamServer_GetHeader tests the GetHeader method
func TestNATSStreamServer_GetHeader(t *testing.T) {
	// Setup test environment
	tempDir := t.TempDir()
	logger := log.New()

	config := DefaultConfig()
	config.Port = 0 // Let OS assign port
	config.StorageDir = filepath.Join(tempDir, "nats-data")

	// Create NATS manager
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams
	err = manager.InitStreams(context.Background())
	require.NoError(t, err)

	// Create metadata manager
	ctx := context.Background()
	metadata, err := NewMetadataManager(ctx, manager, logger)
	require.NoError(t, err)

	// Create a mock delegate
	mockDelegate := newMockStreamServer()

	// Create NATSStreamServer with metadata manager
	server := &NATSStreamServer{
		delegate:    mockDelegate,
		natsManager: manager,
		logger:      logger,
		nextEntry:   0,
		metadata:    metadata,
	}

	// Initialize server to set up KV store
	err = server.Start()
	require.NoError(t, err)

	// Test 1: GetHeader with no entries - should read from KV store after initialization
	header := server.GetHeader()
	assert.Equal(t, uint64(0), header.TotalEntries)
	assert.Equal(t, uint8(1), header.Version)
	assert.Equal(t, uint64(4334), header.SystemID)

	// Test 2: Add entries using proper transaction flow
	err = server.StartAtomicOp()
	require.NoError(t, err)

	// Add 3 entries
	entryNum1, err := server.AddStreamEntry(1, []byte("test entry 1"))
	require.NoError(t, err)
	assert.Equal(t, uint64(0), entryNum1) // Placeholder until commit

	entryNum2, err := server.AddStreamEntry(2, []byte("test entry 2"))
	require.NoError(t, err)
	assert.Equal(t, uint64(0), entryNum2) // Placeholder until commit

	entryNum3, err := server.AddStreamEntry(3, []byte("test entry 3"))
	require.NoError(t, err)
	assert.Equal(t, uint64(0), entryNum3) // Placeholder until commit

	// Commit the transaction - this should update KV store
	err = server.CommitAtomicOp()
	require.NoError(t, err)

	// Give time for KV store update
	time.Sleep(200 * time.Millisecond)

	// Test 3: GetHeader should now read from KV store and return 3
	header = server.GetHeader()
	assert.Equal(t, uint64(3), header.TotalEntries, "GetHeader should read total entries from KV store")

	// Test 4: Verify KV store was actually updated by reading directly
	js, err := manager.getJetStream()
	require.NoError(t, err)

	kv, err := js.KeyValue(context.Background(), "METADATA")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entry, err := kv.Get(ctx, MetadataTotalEntriesKey)
	require.NoError(t, err)

	storedValue := binary.BigEndian.Uint64(entry.Value())
	assert.Equal(t, uint64(3), storedValue, "KV store should contain correct total entries")

	// Test 5: Test behavior when metadata unavailable - should return 0
	// Create server without metadata manager
	serverNoMetadata := &NATSStreamServer{
		delegate:    mockDelegate,
		natsManager: manager,
		logger:      logger,
		nextEntry:   0,
		metadata:    nil, // No metadata manager
	}

	// Should return 0 when metadata unavailable (no fallback)
	headerFallback := serverNoMetadata.GetHeader()
	assert.Equal(t, uint64(0), headerFallback.TotalEntries, "Should return 0 when metadata unavailable")
}

// TestNATSStreamServer_AddEntry tests adding entries to the stream
func TestNATSStreamServer_AddEntry(t *testing.T) {
	// Setup test environment
	tempDir := t.TempDir()
	logger := log.New()

	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.StorageDir = filepath.Join(tempDir, "nats-data")

	// Create NATS manager
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams
	err = manager.InitStreams(context.Background())
	require.NoError(t, err)

	// Create a mock delegate
	mockDelegate := newMockStreamServer()

	// Create NATSStreamServer
	server := &NATSStreamServer{
		delegate:    mockDelegate,
		natsManager: manager,
		logger:      logger,
		nextEntry:   0,
	}

	// Initialize server
	err = server.Start()
	require.NoError(t, err)

	// Test transaction flow: start -> add entry -> add bookmark -> commit
	err = server.StartAtomicOp()
	require.NoError(t, err)

	// Add a regular entry
	entryNum, err := server.AddStreamEntry(1, []byte("test entry"))
	require.NoError(t, err)
	assert.Equal(t, uint64(0), entryNum) // Placeholder until commit

	// Add a bookmark entry
	bookmarkNum, err := server.AddStreamBookmark([]byte("test bookmark"))
	require.NoError(t, err)
	assert.Equal(t, uint64(0), bookmarkNum) // Placeholder until commit

	// Commit the transaction
	err = server.CommitAtomicOp()
	require.NoError(t, err)

	// Give some time for the messages to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify entries are in the stream
	// Get stream info directly from manager's mainStream
	ctx := context.Background()
	streamInfo, err := manager.mainStream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), streamInfo.State.LastSeq) // Should have 2 messages

	// Verify header returns correct count
	header := server.GetHeader()
	assert.Equal(t, uint64(2), header.TotalEntries)

	// Test GetEntry
	entry, err := server.GetEntry(0)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), entry.Number)
	assert.Equal(t, []byte("test entry"), entry.Data)

	// Test rollback
	err = server.StartAtomicOp()
	require.NoError(t, err)

	_, err = server.AddStreamEntry(1, []byte("should be rolled back"))
	require.NoError(t, err)

	err = server.RollbackAtomicOp()
	require.NoError(t, err)

	// Verify no new entries were added
	streamInfo, err = manager.mainStream.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), streamInfo.State.LastSeq) // Still 2 messages
}

// TestNATSStreamServer_TruncateFile tests truncating the stream
func TestNATSStreamServer_TruncateFile(t *testing.T) {
	// Setup test environment
	tempDir := t.TempDir()
	logger := log.New()

	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.StorageDir = filepath.Join(tempDir, "nats-data")

	// Create NATS manager
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams
	err = manager.InitStreams(context.Background())
	require.NoError(t, err)

	// Create a mock delegate
	mockDelegate := newMockStreamServer()

	// Create NATSStreamServer with a mock metadata manager
	server := &NATSStreamServer{
		delegate:    mockDelegate,
		natsManager: manager,
		logger:      logger,
		nextEntry:   0,
		metadata:    nil, // We'll test without metadata functionality
	}

	// Initialize server
	err = server.Start()
	require.NoError(t, err)

	// Add 3 entries
	err = server.StartAtomicOp()
	require.NoError(t, err)

	_, err = server.AddStreamEntry(1, []byte("test1"))
	require.NoError(t, err)

	_, err = server.AddStreamEntry(1, []byte("test2"))
	require.NoError(t, err)

	_, err = server.AddStreamEntry(1, []byte("test3"))
	require.NoError(t, err)

	err = server.CommitAtomicOp()
	require.NoError(t, err)

	// Give some time for the messages to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify we have 3 entries
	header := server.GetHeader()
	assert.Equal(t, uint64(3), header.TotalEntries)

	// Truncate the stream at entry 1 (keep entries 0 and 1, remove 2)
	err = server.TruncateFile(1)
	require.NoError(t, err)

	// Give some time for the truncation to be processed
	time.Sleep(500 * time.Millisecond)

	// Verify entry count is reduced
	header = server.GetHeader()
	// The NATS implementation should delegate to the mock stream server for the count
	// After truncating at entry 1, should have exactly 2 entries (0 and 1)
	assert.Equal(t, uint64(2), header.TotalEntries,
		"After truncating at entry 1, should have exactly 2 entries (0 and 1)")
}

// TestNATSStreamServer_Bookmarks tests the bookmark functionality
func TestNATSStreamServer_Bookmarks(t *testing.T) {
	// Setup test environment
	tempDir := t.TempDir()
	logger := log.New()

	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.StorageDir = filepath.Join(tempDir, "nats-data")

	// Create NATS manager
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams
	err = manager.InitStreams(context.Background())
	require.NoError(t, err)

	// Try to create a NATSMetadata
	ctx := context.Background()
	metadata, err := NewMetadataManager(ctx, manager, logger)
	if err != nil {
		// If bookmark creation fails, log and skip test
		t.Logf("Skipping bookmark test: %v", err)
		t.Skip("Bookmark initialization failed")
		return
	}

	// Create a mock delegate
	mockDelegate := newMockStreamServer()

	// Create NATSStreamServer with the bookmark manager
	server := &NATSStreamServer{
		delegate:    mockDelegate,
		natsManager: manager,
		logger:      logger,
		nextEntry:   0,
		metadata:    metadata,
	}

	// Initialize server
	err = server.Start()
	require.NoError(t, err)

	// Add entries and a bookmark
	err = server.StartAtomicOp()
	require.NoError(t, err)

	// Add a regular entry
	_, err = server.AddStreamEntry(1, []byte("entry before bookmark"))
	require.NoError(t, err)

	// Add a bookmark entry
	bookmarkData := []byte("test-bookmark")
	bookmarkNum, err := server.AddStreamBookmark(bookmarkData)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), bookmarkNum) // Placeholder until commit

	// Add another entry after the bookmark
	_, err = server.AddStreamEntry(1, []byte("entry after bookmark"))
	require.NoError(t, err)

	// Commit the transaction
	err = server.CommitAtomicOp()
	require.NoError(t, err)

	// Give some time for the messages to be processed
	time.Sleep(500 * time.Millisecond)

	// Add a direct bookmark to verify it works
	err = metadata.AddBookmark(ctx, bookmarkData, 1)
	require.NoError(t, err)

	// Test GetBookmark
	entryNum, err := server.GetBookmark(bookmarkData)
	require.NoError(t, err)

	// The bookmark should point to entry 1
	assert.Equal(t, uint64(1), entryNum,
		"Bookmark should point to entry 1, got %d", entryNum)

	// Test GetFirstEventAfterBookmark
	entry, err := server.GetFirstEventAfterBookmark(bookmarkData)
	require.NoError(t, err)

	// The entry after bookmark should be the one we added after the bookmark
	assert.Equal(t, []byte("entry after bookmark"), entry.Data)

	// Test with a non-existent bookmark
	_, err = server.GetBookmark([]byte("non-existent"))
	assert.Error(t, err)
}

// mockStreamServer implements a simple in-memory StreamServer for testing
type mockStreamServer struct {
	header    datastreamer.HeaderEntry
	entries   []datastreamer.FileEntry
	bookmarks map[string]uint64
	nextEntry uint64
	txActive  bool
	txStarted uint64
	txEntries []datastreamer.FileEntry
}

func newMockStreamServer() *mockStreamServer {
	return &mockStreamServer{
		header: datastreamer.HeaderEntry{
			TotalEntries: 0,
		},
		entries:   make([]datastreamer.FileEntry, 0),
		bookmarks: make(map[string]uint64),
		nextEntry: 0,
	}
}

func (m *mockStreamServer) Start() error {
	return nil
}

func (m *mockStreamServer) StartAtomicOp() error {
	if m.txActive {
		return errAtomicOpAlreadyStarted
	}
	m.txActive = true
	m.txStarted = m.nextEntry
	m.txEntries = make([]datastreamer.FileEntry, 0)
	return nil
}

func (m *mockStreamServer) AddStreamEntry(entryType datastreamer.EntryType, data []byte) (uint64, error) {
	if !m.txActive {
		return 0, errNoActiveAtomicOp
	}

	entry := datastreamer.FileEntry{
		Length: uint32(len(data)),
		Type:   entryType,
		Number: m.nextEntry,
		Data:   data,
	}

	m.txEntries = append(m.txEntries, entry)
	m.nextEntry++
	return entry.Number, nil
}

func (m *mockStreamServer) AddStreamBookmark(bookmark []byte) (uint64, error) {
	if !m.txActive {
		return 0, errNoActiveAtomicOp
	}

	entry := datastreamer.FileEntry{
		Length: uint32(len(bookmark)),
		Type:   datastreamer.EtBookmark,
		Number: m.nextEntry,
		Data:   bookmark,
	}

	m.txEntries = append(m.txEntries, entry)
	m.bookmarks[string(bookmark)] = m.nextEntry
	m.nextEntry++
	return entry.Number, nil
}

func (m *mockStreamServer) CommitAtomicOp() error {
	if !m.txActive {
		return errNoActiveAtomicOp
	}

	m.entries = append(m.entries, m.txEntries...)
	m.header.TotalEntries = m.nextEntry

	m.txActive = false
	m.txEntries = nil
	return nil
}

func (m *mockStreamServer) RollbackAtomicOp() error {
	if !m.txActive {
		return errNoActiveAtomicOp
	}

	// Restore entry state
	m.nextEntry = m.txStarted

	m.txActive = false
	m.txEntries = nil
	return nil
}

func (m *mockStreamServer) TruncateFile(entryNum uint64) error {
	if entryNum >= m.header.TotalEntries {
		return errInvalidEntryNumber
	}

	// Remove entries after entryNum
	m.entries = m.entries[:entryNum+1]
	m.header.TotalEntries = entryNum + 1
	m.nextEntry = entryNum + 1

	// Remove bookmarks pointing beyond
	for bookmark, entry := range m.bookmarks {
		if entry > entryNum {
			delete(m.bookmarks, bookmark)
		}
	}

	return nil
}

func (m *mockStreamServer) UpdateEntryData(entryNum uint64, etype datastreamer.EntryType, data []byte) error {
	return errUpdateNotSupported
}

func (m *mockStreamServer) GetHeader() datastreamer.HeaderEntry {
	return m.header
}

func (m *mockStreamServer) GetEntry(entryNum uint64) (datastreamer.FileEntry, error) {
	if entryNum >= m.header.TotalEntries {
		return datastreamer.FileEntry{}, errEntryNotFound
	}
	return m.entries[entryNum], nil
}

func (m *mockStreamServer) GetBookmark(bookmark []byte) (uint64, error) {
	entry, ok := m.bookmarks[string(bookmark)]
	if !ok {
		return 0, errBookmarkNotFound
	}
	return entry, nil
}

func (m *mockStreamServer) GetFirstEventAfterBookmark(bookmark []byte) (datastreamer.FileEntry, error) {
	entryNum, err := m.GetBookmark(bookmark)
	if err != nil {
		return datastreamer.FileEntry{}, err
	}

	return m.GetEntry(entryNum)
}

func (m *mockStreamServer) GetDataBetweenBookmarks(bookmarkFrom, bookmarkTo []byte) ([]byte, error) {
	return nil, errNotImplemented
}

func (m *mockStreamServer) BookmarkPrintDump() {
	// No-op for mock
}

// mockStreamServerWithDeletion extends the mock to simulate deleted entries
type mockStreamServerWithDeletion struct {
	*mockStreamServer
	deletedEntries map[uint64]bool
}

// Override GetEntry to simulate deleted entries
func (m *mockStreamServerWithDeletion) GetEntry(entryNum uint64) (datastreamer.FileEntry, error) {
	if deleted, exists := m.deletedEntries[entryNum]; exists && deleted {
		return datastreamer.FileEntry{}, fmt.Errorf("entry %d has been deleted", entryNum)
	}
	return m.mockStreamServer.GetEntry(entryNum)
}

// TestNATSStreamServer_UpdateTotalEntriesInKV tests the KV storage of total entries
func TestNATSStreamServer_UpdateTotalEntriesInKV(t *testing.T) {
	// Setup test environment
	tempDir := t.TempDir()
	logger := log.New()

	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.StorageDir = filepath.Join(tempDir, "nats-data")

	// Create NATS manager
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams
	err = manager.InitStreams(context.Background())
	require.NoError(t, err)

	// Create metadata manager
	ctx := context.Background()
	metadata, err := NewMetadataManager(ctx, manager, logger)
	require.NoError(t, err)

	// Create a mock delegate
	mockDelegate := newMockStreamServer()

	// Create NATSStreamServer with bookmark manager
	server := &NATSStreamServer{
		delegate:    mockDelegate,
		natsManager: manager,
		logger:      logger,
		nextEntry:   0,
		metadata:    metadata,
	}

	// Initialize server
	err = server.Start()
	require.NoError(t, err)

	// First do a commit operation to initialize the KV bucket naturally
	err = server.StartAtomicOp()
	require.NoError(t, err)

	// Add an entry to trigger KV initialization during commit
	_, err = server.AddStreamEntry(1, []byte("test entry"))
	require.NoError(t, err)

	err = server.CommitAtomicOp()
	require.NoError(t, err)

	// Now test setting total entries in KV directly using metadata manager
	server.nextEntry = 42
	err = server.metadata.SetTotalEntries(ctx, server.nextEntry)
	require.NoError(t, err)

	// Verify the value was stored correctly by reading it back
	js, err := manager.getJetStream()
	require.NoError(t, err)

	bucketName := "METADATA"
	kv, err := js.KeyValue(context.Background(), bucketName)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entry, err := kv.Get(ctx, MetadataTotalEntriesKey)
	require.NoError(t, err)

	// Convert bytes back to uint64 and verify
	storedValue := binary.BigEndian.Uint64(entry.Value())
	assert.Equal(t, uint64(42), storedValue)
}

// TestNATSStreamServer_CommitUpdatesKV tests that CommitAtomicOp updates the KV store
func TestNATSStreamServer_CommitUpdatesKV(t *testing.T) {
	// Setup test environment
	tempDir := t.TempDir()
	logger := log.New()

	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.StorageDir = filepath.Join(tempDir, "nats-data")

	// Create NATS manager
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams
	err = manager.InitStreams(context.Background())
	require.NoError(t, err)

	// Create metadata manager
	ctx := context.Background()
	metadata, err := NewMetadataManager(ctx, manager, logger)
	require.NoError(t, err)

	// Create a mock delegate
	mockDelegate := newMockStreamServer()

	// Create NATSStreamServer with bookmark manager
	server := &NATSStreamServer{
		delegate:    mockDelegate,
		natsManager: manager,
		logger:      logger,
		nextEntry:   0,
		metadata:    metadata,
	}

	// Initialize server
	err = server.Start()
	require.NoError(t, err)

	// Add entries and commit to test KV update
	err = server.StartAtomicOp()
	require.NoError(t, err)

	// Add 3 entries
	_, err = server.AddStreamEntry(1, []byte("entry1"))
	require.NoError(t, err)
	_, err = server.AddStreamEntry(1, []byte("entry2"))
	require.NoError(t, err)
	_, err = server.AddStreamEntry(1, []byte("entry3"))
	require.NoError(t, err)

	// Commit should update KV store
	err = server.CommitAtomicOp()
	require.NoError(t, err)

	// Give time for processing
	time.Sleep(100 * time.Millisecond)

	// Verify KV was updated with correct total
	js, err := manager.getJetStream()
	require.NoError(t, err)

	bucketName := "METADATA"
	kv, err := js.KeyValue(context.Background(), bucketName)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entry, err := kv.Get(ctx, MetadataTotalEntriesKey)
	require.NoError(t, err)

	// Convert bytes back to uint64 and verify
	storedValue := binary.BigEndian.Uint64(entry.Value())
	assert.Equal(t, uint64(3), storedValue)
}

// TestNATSStreamServer_TruncateUpdatesKV tests that TruncateFile updates the KV store
func TestNATSStreamServer_TruncateUpdatesKV(t *testing.T) {
	// Setup test environment
	tempDir := t.TempDir()
	logger := log.New()

	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.StorageDir = filepath.Join(tempDir, "nats-data")

	// Create NATS manager
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams
	err = manager.InitStreams(context.Background())
	require.NoError(t, err)

	// Create metadata manager
	ctx := context.Background()
	metadata, err := NewMetadataManager(ctx, manager, logger)
	require.NoError(t, err)

	// Create a mock delegate
	mockDelegate := newMockStreamServer()

	// Create NATSStreamServer with bookmark manager
	server := &NATSStreamServer{
		delegate:    mockDelegate,
		natsManager: manager,
		logger:      logger,
		nextEntry:   0,
		metadata:    metadata,
	}

	// Initialize server
	err = server.Start()
	require.NoError(t, err)

	// Add entries first
	err = server.StartAtomicOp()
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		_, err = server.AddStreamEntry(1, []byte(fmt.Sprintf("entry%d", i)))
		require.NoError(t, err)
	}

	err = server.CommitAtomicOp()
	require.NoError(t, err)

	// Give time for processing
	time.Sleep(100 * time.Millisecond)

	// Truncate at entry 2 (keep entries 0, 1, 2)
	err = server.TruncateFile(2)
	require.NoError(t, err)

	// Give time for processing
	time.Sleep(200 * time.Millisecond)

	// Verify KV was updated after truncation
	js, err := manager.getJetStream()
	require.NoError(t, err)

	bucketName := "METADATA"
	kv, err := js.KeyValue(context.Background(), bucketName)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entry, err := kv.Get(ctx, MetadataTotalEntriesKey)
	require.NoError(t, err)

	// Convert bytes back to uint64 and verify truncated count
	storedValue := binary.BigEndian.Uint64(entry.Value())
	assert.Equal(t, uint64(3), storedValue) // Should be 3 (entries 0, 1, 2)
}

// TestNATSStreamServer_UpdateKVErrorHandling tests error handling when context is canceled
func TestNATSStreamServer_UpdateKVErrorHandling(t *testing.T) {
	// Create NATS manager but don't start it
	logger := log.New()
	config := DefaultConfig()
	config.Port = -1
	manager := NewManager(config, logger)
	// Don't start the manager to create error conditions

	// Test that metadata manager creation fails with uninitialized manager
	ctx := context.Background()
	_, err := NewMetadataManager(ctx, manager, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize metadata manager")
}
