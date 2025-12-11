package natsstream

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNATSServerClientTotalEntriesIntegration tests the complete flow of server storing
// total entries in KV and client retrieving them
func TestNATSServerClientTotalEntriesIntegration(t *testing.T) {
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

	// Create NATSStreamServer with metadata manager
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

	// Create NATS client that connects to the same server
	clientURL, err := manager.URL()
	require.NoError(t, err)
	client := NewNATSClient(ctx, clientURL, false, manager, logger)
	defer client.Stop()

	err = client.Start()
	require.NoError(t, err)

	// Verify initial state - no entries yet
	totalEntries, err := client.GetTotalEntries()
	require.NoError(t, err)
	assert.Equal(t, uint64(0), totalEntries)

	// Add some entries to the server
	err = server.StartAtomicOp()
	require.NoError(t, err)

	// Add 5 entries
	for i := 0; i < 5; i++ {
		_, err = server.AddStreamEntry(1, []byte("test entry"))
		require.NoError(t, err)
	}

	// Commit should update KV store
	err = server.CommitAtomicOp()
	require.NoError(t, err)

	// Give time for KV update to propagate
	time.Sleep(200 * time.Millisecond)

	// Client should now see the updated total
	totalEntries, err = client.GetTotalEntries()
	require.NoError(t, err)
	assert.Equal(t, uint64(5), totalEntries)

	// Add more entries and verify again
	err = server.StartAtomicOp()
	require.NoError(t, err)

	// Add 3 more entries
	for i := 0; i < 3; i++ {
		_, err = server.AddStreamEntry(1, []byte("additional entry"))
		require.NoError(t, err)
	}

	err = server.CommitAtomicOp()
	require.NoError(t, err)

	// Give time for KV update to propagate
	time.Sleep(200 * time.Millisecond)

	// Client should see the new total
	totalEntries, err = client.GetTotalEntries()
	require.NoError(t, err)
	assert.Equal(t, uint64(8), totalEntries)
}

// TestNATSServerClientTruncationIntegration tests that truncation updates
// are reflected in client GetTotalEntries calls
func TestNATSServerClientTruncationIntegration(t *testing.T) {
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

	// Create NATSStreamServer with metadata manager
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

	// Create NATS client
	clientURL, err := manager.URL()
	require.NoError(t, err)
	client := NewNATSClient(ctx, clientURL, false, manager, logger)
	defer client.Stop()

	err = client.Start()
	require.NoError(t, err)

	// Add 10 entries initially
	err = server.StartAtomicOp()
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		_, err = server.AddStreamEntry(1, []byte("test entry"))
		require.NoError(t, err)
	}

	err = server.CommitAtomicOp()
	require.NoError(t, err)

	// Give time for KV update
	time.Sleep(200 * time.Millisecond)

	// Verify client sees 10 entries
	totalEntries, err := client.GetTotalEntries()
	require.NoError(t, err)
	assert.Equal(t, uint64(10), totalEntries)

	// Truncate at entry 5 (keep entries 0-5, total of 6)
	err = server.TruncateFile(5)
	require.NoError(t, err)

	// Give time for truncation and KV update
	time.Sleep(500 * time.Millisecond)

	// Client should now see truncated count
	totalEntries, err = client.GetTotalEntries()
	require.NoError(t, err)
	assert.Equal(t, uint64(6), totalEntries) // Entries 0-5 = 6 total
}

// TestNATSServerClientMultipleClientsIntegration tests that multiple clients
// can all read the same total entries count from KV
func TestNATSServerClientMultipleClientsIntegration(t *testing.T) {
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

	// Create server
	server := &NATSStreamServer{
		delegate:    newMockStreamServer(),
		natsManager: manager,
		logger:      logger,
		nextEntry:   0,
		metadata:    metadata,
	}

	err = server.Start()
	require.NoError(t, err)

	// Create multiple clients
	clientURL, err := manager.URL()
	require.NoError(t, err)

	client1 := NewNATSClient(ctx, clientURL, false, manager, logger)
	defer client1.Stop()
	err = client1.Start()
	require.NoError(t, err)

	client2 := NewNATSClient(ctx, clientURL, false, manager, logger)
	defer client2.Stop()
	err = client2.Start()
	require.NoError(t, err)

	client3 := NewNATSClient(ctx, clientURL, false, manager, logger)
	defer client3.Stop()
	err = client3.Start()
	require.NoError(t, err)

	// Add entries on server
	err = server.StartAtomicOp()
	require.NoError(t, err)

	for i := 0; i < 7; i++ {
		_, err = server.AddStreamEntry(1, []byte("shared entry"))
		require.NoError(t, err)
	}

	err = server.CommitAtomicOp()
	require.NoError(t, err)

	// Give time for KV update
	time.Sleep(200 * time.Millisecond)

	// All clients should see the same total
	total1, err := client1.GetTotalEntries()
	require.NoError(t, err)

	total2, err := client2.GetTotalEntries()
	require.NoError(t, err)

	total3, err := client3.GetTotalEntries()
	require.NoError(t, err)

	// All should be equal
	assert.Equal(t, uint64(7), total1)
	assert.Equal(t, total1, total2)
	assert.Equal(t, total2, total3)
}

// TestNATSServerClientKVErrorHandling tests that clients properly error when
// KV store is not available or empty (no fallback)
func TestNATSServerClientKVErrorHandling(t *testing.T) {
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

	// Create client without server (so no KV entries will be written)
	ctx := context.Background()
	clientURL, err := manager.URL()
	require.NoError(t, err)
	client := NewNATSClient(ctx, clientURL, false, manager, logger)
	defer client.Stop()

	err = client.Start()
	require.NoError(t, err)

	// Publish some messages directly to the stream to test fallback
	js, err := manager.getJetStream()
	require.NoError(t, err)

	for i := 0; i < 4; i++ {
		_, err = js.Publish(ctx, "datastream.entry", []byte("direct message"))
		require.NoError(t, err)
	}

	// Give time for messages to be stored
	time.Sleep(100 * time.Millisecond)

	// Client should error when KV metadata is missing (no fallback)
	totalEntries, err := client.GetTotalEntries()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metadata: key not found")
	assert.Equal(t, uint64(0), totalEntries)
}
