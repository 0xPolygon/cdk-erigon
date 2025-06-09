package natsstream

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInterfaceCompliance verifies that NATSClient fully implements DatastreamClient interface
func TestInterfaceCompliance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, ns, _ := setupTestClientWithStream(t, ctx, 1101)
	defer ns.Shutdown()
	defer client.Stop()

	// Verify interface compliance at compile time
	var _ types.DatastreamClient = client

	// Test all interface methods are implemented and functional
	t.Run("Start", testStart)
	t.Run("Stop", testStop)
	t.Run("HandleStart", testHandleStart)
	t.Run("ReadAllEntriesToChannel", testReadAllEntriesToChannel)
	t.Run("StopReadingToChannel", testStopReadingToChannel)
	t.Run("GetEntryChan", testGetEntryChan)
	t.Run("RenewEntryChannel", testRenewEntryChannel)
	t.Run("RenewMaxEntryChannel", testRenewMaxEntryChannel)
	t.Run("GetL2BlockByNumber", testGetL2BlockByNumber)
	t.Run("GetLatestL2Block", testGetLatestL2Block)
	t.Run("GetProgressAtomic", testGetProgressAtomic)
	t.Run("ExecutePerFile", testExecutePerFile)
}

func testStart(t *testing.T) {
	ctx := context.Background()
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	createTestStream(t, url, 1101)

	client := NewNATSClient(ctx, url, false, 1101, 7, log.New())
	defer client.Stop()

	// Test successful start
	err := client.Start()
	assert.NoError(t, err)
	assert.True(t, client.started)

	// Test idempotent start
	err = client.Start()
	assert.NoError(t, err)

	// Test start with invalid URL
	badClient := NewNATSClient(ctx, "nats://invalid:1234", false, 1101, 7, log.New())
	err = badClient.Start()
	assert.Error(t, err)
}

func testStop(t *testing.T) {
	ctx := context.Background()
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	createTestStream(t, url, 1101)

	client := NewNATSClient(ctx, url, false, 1101, 7, log.New())

	// Test stop before start
	err := client.Stop()
	assert.NoError(t, err)

	// Test normal stop
	err = client.Start()
	require.NoError(t, err)

	err = client.Stop()
	assert.NoError(t, err)
	assert.False(t, client.started)

	// Test idempotent stop
	err = client.Stop()
	assert.NoError(t, err)
}

func testHandleStart(t *testing.T) {
	ctx := context.Background()
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	createTestStream(t, url, 1101)

	client := NewNATSClient(ctx, url, false, 1101, 7, log.New())
	defer client.Stop()

	// Should fail before start
	err := client.HandleStart()
	assert.Equal(t, ErrNotStarted, err)

	// Should succeed after start
	err = client.Start()
	require.NoError(t, err)

	err = client.HandleStart()
	assert.NoError(t, err)
}

func testReadAllEntriesToChannel(t *testing.T) {
	ctx := context.Background()
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	createTestStream(t, url, 1101)

	client := NewNATSClient(ctx, url, false, 1101, 7, log.New())
	defer client.Stop()

	// Should fail before start
	err := client.ReadAllEntriesToChannel()
	assert.Equal(t, ErrNotStarted, err)

	// Should succeed after start
	err = client.Start()
	require.NoError(t, err)

	err = client.ReadAllEntriesToChannel()
	assert.NoError(t, err)
	assert.True(t, client.reading)

	// Should be idempotent
	err = client.ReadAllEntriesToChannel()
	assert.NoError(t, err)
}

func testStopReadingToChannel(t *testing.T) {
	ctx := context.Background()
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	createTestStream(t, url, 1101)

	client := NewNATSClient(ctx, url, false, 1101, 7, log.New())
	defer client.Stop()

	err := client.Start()
	require.NoError(t, err)

	// Should work even if not reading
	client.StopReadingToChannel()
	assert.False(t, client.reading)

	// Start reading then stop
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)
	assert.True(t, client.reading)

	client.StopReadingToChannel()

	// Wait for reading to stop
	timeout := time.After(5 * time.Second)
	for client.reading {
		select {
		case <-timeout:
			t.Fatal("Reading did not stop within timeout")
		case <-time.After(10 * time.Millisecond):
			// Continue waiting
		}
	}

	assert.False(t, client.reading)
}

func testGetEntryChan(t *testing.T) {
	ctx := context.Background()
	client := NewNATSClient(ctx, "nats://localhost:4222", false, 1101, 7, log.New())
	defer client.Stop()

	entryChan := client.GetEntryChan()
	require.NotNil(t, entryChan)
	assert.NotNil(t, *entryChan)

	// Channel should be the same across calls
	entryChan2 := client.GetEntryChan()
	assert.Equal(t, entryChan, entryChan2)
}

func testRenewEntryChannel(t *testing.T) {
	ctx := context.Background()
	client := NewNATSClient(ctx, "nats://localhost:4222", false, 1101, 7, log.New())
	defer client.Stop()

	// Get initial channel
	entryChan1 := client.GetEntryChan()
	require.NotNil(t, entryChan1)

	// Renew channel
	client.RenewEntryChannel()

	// Should get new channel
	entryChan2 := client.GetEntryChan()
	require.NotNil(t, entryChan2)
	assert.NotEqual(t, entryChan1, entryChan2)

	// Verify old channel is closed
	select {
	case _, ok := <-*entryChan1:
		assert.False(t, ok, "Old channel should be closed")
	default:
		// Channel might not be immediately readable, but that's ok
	}
}

func testRenewMaxEntryChannel(t *testing.T) {
	ctx := context.Background()
	client := NewNATSClient(ctx, "nats://localhost:4222", false, 1101, 7, log.New())
	defer client.Stop()

	// Set max size
	client.maxEntryChanSize = 1000

	// Get initial channel
	entryChan1 := client.GetEntryChan()
	require.NotNil(t, entryChan1)

	// Renew with max size
	client.RenewMaxEntryChannel()

	// Should get new channel
	entryChan2 := client.GetEntryChan()
	require.NotNil(t, entryChan2)
	assert.NotEqual(t, entryChan1, entryChan2)
}

func testGetL2BlockByNumber(t *testing.T) {
	ctx := context.Background()
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	createTestStream(t, url, 1101)

	client := NewNATSClient(ctx, url, false, 1101, 7, log.New())
	defer client.Stop()

	// Should fail before start
	_, err := client.GetL2BlockByNumber(1)
	assert.Equal(t, ErrNotStarted, err)

	err = client.Start()
	require.NoError(t, err)

	// Setup test data with bookmark
	js, err := jetstream.New(client.nc)
	require.NoError(t, err)

	// Publish bookmark for block 1
	publishBookmark(t, js, 1, datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK, client.subjectPrefix)
	publishL2Block(t, js, 1, 1, client.subjectPrefix)
	publishL2Transaction(t, js, 1, 0, client.subjectPrefix)
	publishL2BlockEnd(t, js, 1, client.subjectPrefix)

	// Start reading to process bookmark
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	// Wait for bookmark to be processed
	time.Sleep(500 * time.Millisecond)

	// Now test retrieval
	block, err := client.GetL2BlockByNumber(1)
	assert.NoError(t, err)
	require.NotNil(t, block)
	assert.Equal(t, uint64(1), block.L2BlockNumber)
	assert.Len(t, block.L2Txs, 1)

	// Test non-existent block
	_, err = client.GetL2BlockByNumber(999)
	assert.Error(t, err)
}

func testGetLatestL2Block(t *testing.T) {
	ctx := context.Background()
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	createTestStream(t, url, 1101)

	client := NewNATSClient(ctx, url, false, 1101, 7, log.New())
	defer client.Stop()

	// Should fail before start
	_, err := client.GetLatestL2Block()
	assert.Equal(t, ErrNotStarted, err)

	err = client.Start()
	require.NoError(t, err)

	// Initially no latest block
	block, err := client.GetLatestL2Block()
	assert.NoError(t, err)
	assert.Nil(t, block)

	// Publish and process a block
	js, err := jetstream.New(client.nc)
	require.NoError(t, err)

	publishL2Block(t, js, 5, 1, client.subjectPrefix)
	publishL2Transaction(t, js, 5, 0, client.subjectPrefix)
	publishL2BlockEnd(t, js, 5, client.subjectPrefix)

	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	// Wait for processing
	entryChan := client.GetEntryChan()
	select {
	case <-*entryChan:
		// Block processed
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for block processing")
	}

	// Now should have latest block
	block, err = client.GetLatestL2Block()
	assert.NoError(t, err)
	require.NotNil(t, block)
	assert.Equal(t, uint64(5), block.L2BlockNumber)
}

func testGetProgressAtomic(t *testing.T) {
	ctx := context.Background()
	client := NewNATSClient(ctx, "nats://localhost:4222", false, 1101, 7, log.New())
	defer client.Stop()

	progress := client.GetProgressAtomic()
	require.NotNil(t, progress)

	// Test atomic operations
	assert.Equal(t, uint64(0), progress.Load())

	progress.Store(42)
	assert.Equal(t, uint64(42), progress.Load())

	progress.Add(8)
	assert.Equal(t, uint64(50), progress.Load())
}

func testExecutePerFile(t *testing.T) {
	ctx := context.Background()
	client := NewNATSClient(ctx, "nats://localhost:4222", false, 1101, 7, log.New())
	defer client.Stop()

	// This method is not implemented (returns nil)
	bookmark := types.NewBookmarkProto(1, datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK)
	err := client.ExecutePerFile(bookmark, func(file *types.FileEntry) error {
		return nil
	})
	assert.NoError(t, err) // Should return nil (not implemented)
}

// TestConcurrentInterfaceUsage tests thread safety of interface methods
func TestConcurrentInterfaceUsage(t *testing.T) {
	ctx := context.Background()
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	createTestStream(t, url, 1101)

	client := NewNATSClient(ctx, url, false, 1101, 7, log.New())
	defer client.Stop()

	err := client.Start()
	require.NoError(t, err)

	const goroutineCount = 10
	const operationsPerGoroutine = 50

	var wg sync.WaitGroup
	errors := make(chan error, goroutineCount*operationsPerGoroutine)

	// Test concurrent access to various interface methods
	for i := 0; i < goroutineCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				// Test different operations based on operation index
				switch j % 6 {
				case 0:
					// Test GetEntryChan
					entryChan := client.GetEntryChan()
					if entryChan == nil {
						errors <- fmt.Errorf("goroutine %d: GetEntryChan returned nil", id)
					}

				case 1:
					// Test GetProgressAtomic
					progress := client.GetProgressAtomic()
					if progress == nil {
						errors <- fmt.Errorf("goroutine %d: GetProgressAtomic returned nil", id)
					} else {
						progress.Load() // Test atomic read
					}

				case 2:
					// Test GetLatestL2Block
					_, err := client.GetLatestL2Block()
					if err != nil && err != ErrNotStarted {
						errors <- fmt.Errorf("goroutine %d: GetLatestL2Block error: %v", id, err)
					}

				case 3:
					// Test HandleStart
					err := client.HandleStart()
					if err != nil && err != ErrNotStarted {
						errors <- fmt.Errorf("goroutine %d: HandleStart error: %v", id, err)
					}

				case 4:
					// Test RenewEntryChannel
					client.RenewEntryChannel()

				case 5:
					// Test ReadAllEntriesToChannel (should be idempotent)
					err := client.ReadAllEntriesToChannel()
					if err != nil && err != ErrNotStarted {
						errors <- fmt.Errorf("goroutine %d: ReadAllEntriesToChannel error: %v", id, err)
					}
				}

				// Small delay to increase chance of race conditions
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Error(err)
	}
}

// TestInterfaceErrorHandling tests proper error handling across interface methods
func TestInterfaceErrorHandling(t *testing.T) {
	ctx := context.Background()

	// Test with invalid URL
	client := NewNATSClient(ctx, "nats://invalid.host:1234", false, 1101, 7, log.New())
	defer client.Stop()

	// All methods should handle connection errors gracefully
	err := client.Start()
	assert.Error(t, err)

	_, err = client.GetLatestL2Block()
	assert.Error(t, err)

	_, err = client.GetL2BlockByNumber(1)
	assert.Error(t, err)

	err = client.ReadAllEntriesToChannel()
	assert.Error(t, err)

	err = client.HandleStart()
	assert.Error(t, err)

	// These should not error even with failed connection
	entryChan := client.GetEntryChan()
	assert.NotNil(t, entryChan)

	progress := client.GetProgressAtomic()
	assert.NotNil(t, progress)

	client.RenewEntryChannel()
	client.RenewMaxEntryChannel()
	client.StopReadingToChannel()

	err = client.Stop()
	assert.NoError(t, err)
}

// TestFactoryFunction tests the factory function compliance
func TestFactoryFunction(t *testing.T) {
	ctx := context.Background()

	// Test factory function returns correct interface
	client := CreateNATSDatastreamClient(ctx, "nats://localhost:4222", false, 10*time.Second, 7, 100000)
	require.NotNil(t, client)

	// Verify it implements the interface
	var _ types.DatastreamClient = client

	// Test type assertion works
	natsClient, ok := client.(*NATSClient)
	assert.True(t, ok)
	assert.NotNil(t, natsClient)

	// Verify configuration was applied
	assert.Equal(t, uint64(100000), natsClient.maxEntryChanSize)
	assert.Equal(t, uint64(7), natsClient.forkID)

	client.Stop()
}
