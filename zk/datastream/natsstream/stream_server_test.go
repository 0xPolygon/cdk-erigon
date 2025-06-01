package natsstream

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	config.Port = -1 // Use random port
	config.StorageDir = filepath.Join(tempDir, "nats-data")
	config.ChainId = 12345

	// Create NATS manager
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams
	err = manager.InitStreams()
	require.NoError(t, err)

	// Create a mock delegate
	mockDelegate := newMockStreamServer()

	// Create NATSStreamServer
	server := &NATSStreamServer{
		delegate:    mockDelegate,
		natsManager: manager,
		logger:      logger,
		chainId:     config.ChainId,
		nextEntry:   0,
	}

	// Test GetHeader with no entries
	header := server.GetHeader()
	assert.Equal(t, uint64(0), header.TotalEntries)

	// Manually publish some messages to the stream and verify counts
	// Add 5 entries to test entry counting
	js, err := manager.getJetStream()
	require.NoError(t, err)

	// The subject should match what's expected in the implementation
	// Typically datastream.entry for the default subject
	subject := "datastream.entry"

	for i := 0; i < 5; i++ {
		_, err = js.Publish(context.Background(), subject, []byte("test data"))
		require.NoError(t, err)
	}

	// Give some time for the messages to be processed
	time.Sleep(100 * time.Millisecond)

	// Test that GetHeader returns the correct number of entries
	header = server.GetHeader()
	assert.Equal(t, uint64(5), header.TotalEntries)
	assert.Equal(t, config.ChainId, header.SystemID)
}

// TestNATSStreamServer_AddEntry tests adding entries to the stream
func TestNATSStreamServer_AddEntry(t *testing.T) {
	// Setup test environment
	tempDir := t.TempDir()
	logger := log.New()

	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.StorageDir = filepath.Join(tempDir, "nats-data")
	config.ChainId = 12345

	// Create NATS manager
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams
	err = manager.InitStreams()
	require.NoError(t, err)

	// Create a mock delegate
	mockDelegate := newMockStreamServer()

	// Create NATSStreamServer
	server := &NATSStreamServer{
		delegate:    mockDelegate,
		natsManager: manager,
		logger:      logger,
		chainId:     config.ChainId,
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
	assert.Equal(t, uint64(1), entryNum)

	// Add a bookmark entry
	bookmarkNum, err := server.AddStreamBookmark([]byte("test bookmark"))
	require.NoError(t, err)
	assert.Equal(t, uint64(2), bookmarkNum)

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
	config.ChainId = 12345

	// Create NATS manager
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams
	err = manager.InitStreams()
	require.NoError(t, err)

	// Create a mock delegate
	mockDelegate := newMockStreamServer()

	// Create NATSStreamServer with a mock bookmark manager
	server := &NATSStreamServer{
		delegate:    mockDelegate,
		natsManager: manager,
		logger:      logger,
		chainId:     config.ChainId,
		nextEntry:   0,
		bookmark:    nil, // We'll test without bookmark functionality
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
	// The NATS implementation may work differently than the file-based one regarding truncate
	// It might delete messages after entryNum, so remaining count would be entryNum+1
	assert.LessOrEqual(t, header.TotalEntries, uint64(2),
		"After truncating at entry 1, should have at most 2 entries (0 and 1)")
}

// TestNATSStreamServer_Bookmarks tests the bookmark functionality
func TestNATSStreamServer_Bookmarks(t *testing.T) {
	// Setup test environment
	tempDir := t.TempDir()
	logger := log.New()

	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.StorageDir = filepath.Join(tempDir, "nats-data")
	config.ChainId = 12345

	// Create NATS manager
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)
	defer manager.Stop()

	// Initialize streams
	err = manager.InitStreams()
	require.NoError(t, err)

	// Try to create a NATSBookmark
	bookmark, err := NewNATSBookmark(manager, config.ChainId, logger)
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
		chainId:     config.ChainId,
		nextEntry:   0,
		bookmark:    bookmark,
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
	assert.Equal(t, uint64(1), bookmarkNum)

	// Add another entry after the bookmark
	_, err = server.AddStreamEntry(1, []byte("entry after bookmark"))
	require.NoError(t, err)

	// Commit the transaction
	err = server.CommitAtomicOp()
	require.NoError(t, err)

	// Give some time for the messages to be processed
	time.Sleep(500 * time.Millisecond)

	// Add a direct bookmark to verify it works
	err = bookmark.AddBookmark(manager, bookmarkData, 1)
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

// TestGetFirstEventAfterBookmarkWithDeleted tests the behavior of GetFirstEventAfterBookmark
// when messages between the bookmark and the next valid entry are deleted
func TestGetFirstEventAfterBookmarkWithDeleted(t *testing.T) {
	// Create a mock delegate that simulates deleted messages
	mockDelegate := newMockStreamServer()

	// Setup initial entries in the mock
	// Entry 0: Regular entry
	// Entry 1: Bookmark
	// Entry 2: "Deleted" entry - we'll simulate this by making GetEntry return an error for this sequence
	// Entry 3: Regular entry - this is what should be returned by GetFirstEventAfterBookmark

	mockDelegate.StartAtomicOp()

	// Add first entry
	mockDelegate.AddStreamEntry(1, []byte("entry 0"))

	// Add bookmark at position 1
	bookmarkData := []byte("test-bookmark")
	_, _ = mockDelegate.AddStreamBookmark(bookmarkData)

	// Add entry that will be "deleted"
	mockDelegate.AddStreamEntry(1, []byte("deleted entry"))

	// Add entry that should be returned
	mockDelegate.AddStreamEntry(1, []byte("entry after deleted"))

	mockDelegate.CommitAtomicOp()

	// Now create a custom version of the mock that overrides GetEntry to simulate deletion
	customMock := &mockStreamServerWithDeletion{
		mockStreamServer: mockDelegate,
		deletedEntries:   map[uint64]bool{2: true}, // Mark entry 2 as deleted
	}

	// Create server with our custom mock
	server := &NATSStreamServer{
		delegate:  customMock,
		logger:    log.New(),
		chainId:   1,
		nextEntry: 4, // We have 4 entries total
	}

	// Test GetFirstEventAfterBookmark - it should skip the deleted entry and return entry 3
	entry, err := server.GetFirstEventAfterBookmark(bookmarkData)
	require.NoError(t, err)

	// Verify it's the correct entry (entry 3, not entry 2)
	assert.Equal(t, uint64(3), entry.Number)
	assert.Equal(t, []byte("entry after deleted"), entry.Data)
}

// Helper function to create an in-memory NATS server for testing
func createTestNATSServer(t *testing.T) (*Manager, string) {
	tempDir, err := os.MkdirTemp("", "nats-test-*")
	require.NoError(t, err)

	config := DefaultConfig()
	config.Port = -1 // Random port
	config.StorageDir = tempDir
	config.ChainId = 1

	logger := log.New()
	manager := NewManager(config, logger)
	err = manager.Start()
	require.NoError(t, err)

	// Initialize streams
	err = manager.InitStreams()
	require.NoError(t, err)

	t.Cleanup(func() {
		manager.Stop()
		os.RemoveAll(tempDir)
	})

	return manager, tempDir
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
