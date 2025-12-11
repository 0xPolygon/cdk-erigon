package natsstream

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// Test helper functions
func setupTestNATSServer(t *testing.T) (*server.Server, string) {
	t.Helper()

	opts := &server.Options{
		Port:      -1, // Random port
		JetStream: true,
		StoreDir:  t.TempDir(),
	}

	ns, err := server.NewServer(opts)
	require.NoError(t, err)

	go ns.Start()

	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server failed to start")
	}

	return ns, ns.ClientURL()
}

func createTestStream(t *testing.T, url string) {
	t.Helper()

	nc, err := nats.Connect(url)
	require.NoError(t, err)
	defer nc.Close()

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Create stream with generic subject pattern
	streamName := "DATASTREAM"
	subjectPattern := "datastream.>"

	_, err = js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{subjectPattern},
		Storage:  jetstream.FileStorage,
	})
	require.NoError(t, err)

	// Create metadata KV store for bookmarks
	kvBucket := "METADATA"
	_, err = js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:  kvBucket,
		Storage: jetstream.FileStorage,
		History: 1,
	})
	require.NoError(t, err)
}

func setupTestClientWithStream(t *testing.T, ctx context.Context) (*NATSClient, *server.Server, string) {
	t.Helper()

	ns, url := setupTestNATSServer(t)
	createTestStream(t, url)

	// Create a manager that will connect to the existing server
	config := DefaultConfig()
	config.Port = -1 // Use random port for manager's embedded server
	manager := NewManager(config, NewTestLogger(t))

	client := NewNATSClient(ctx, url, false, manager, NewTestLogger(t))
	err := client.Start()
	require.NoError(t, err)

	return client, ns, url
}

func TestNATSClient_NewClient(t *testing.T) {
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	// Create a test stream for valid configuration test
	createTestStream(t, url)

	tests := []struct {
		name      string
		natsURL   string
		wantError bool
	}{
		{
			name:    "valid configuration",
			natsURL: url,
		},
		{
			name:    "invalid URL",
			natsURL: "invalid://url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use minimal manager for client creation tests
			config := DefaultConfig()
			config.Port = -1
			manager := NewManager(config, log.New())
			client := NewNATSClient(context.Background(), tt.natsURL, false, manager, log.New())
			assert.NotNil(t, client)

			// Test Start to see if connection works
			err := client.Start()
			if tt.name == "invalid URL" {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if err == nil {
					_ = client.Stop()
				}
			}
		})
	}
}

func TestNATSClient_Start(t *testing.T) {
	// Use setupTestClientWithStream for proper Manager setup
	client, ns, _ := setupTestClientWithStream(t, context.Background())
	defer ns.Shutdown()
	defer client.Stop()

	// Client is already started by helper
	// Starting again should be idempotent
	err := client.Start()
	assert.NoError(t, err)

	// Verify stream was created
	js := client.js
	require.NotNil(t, js)

	stream, err := js.Stream(context.Background(), client.streamName)
	assert.NoError(t, err)
	assert.NotNil(t, stream)
}

func TestNATSClient_GetL2BlockByNumber(t *testing.T) {
	ctx := context.Background()

	// Use the test helper to set up server and publish data
	streamServer, manager := setupTestServerWithManager(t, ctx)
	defer manager.Stop()

	// Get the server URL
	url, err := manager.URL()
	require.NoError(t, err)

	// Create client with the existing manager
	client := NewNATSClient(ctx, url, false, manager, log.New())
	defer client.Stop()

	err = client.Start()
	require.NoError(t, err)

	// Publish a complete L2 block using the helper function with transaction
	// This publishes: bookmark -> L2Block -> transactions -> L2BlockEnd
	publishCompleteL2BlockWithTx(t, streamServer, 1, 1, 3) // block 1, batch 1, 3 transactions

	// Give time for messages to be processed and stored
	time.Sleep(100 * time.Millisecond)

	// Test retrieval
	fullBlock, err := client.GetL2BlockByNumber(1)
	assert.NoError(t, err)
	require.NotNil(t, fullBlock)
	assert.Equal(t, uint64(1), fullBlock.L2BlockNumber)
	assert.Equal(t, 3, len(fullBlock.L2Txs))
}

func TestNATSClient_ConcurrentOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use the robust test infrastructure from behavior tests
	client := populateTestServerWithFixedDataReturnClient(t, ctx)

	defer client.Stop()

	err := client.Start()
	require.NoError(t, err)

	// Concurrent operations
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Multiple readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			_, err := client.GetLatestL2Block()
			if err != nil {
				errors <- err
			}
		}()
	}

	// Multiple channel renewals
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			client.RenewEntryChannel()
		}()
	}

	// Multiple progress queries
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			progress := client.GetProgressAtomic()
			_ = progress.Load()
		}()
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
	}
}

func TestNATSClient_ErrorHandling(t *testing.T) {
	t.Run("connection failure recovery", func(t *testing.T) {
		ns, url := setupTestNATSServer(t)

		// Create the stream that the client expects
		createTestStream(t, url)

		ctx := context.Background()

		client := NewNATSClient(ctx, url, false, nil, log.New())
		defer client.Stop()

		err := client.Start()
		require.NoError(t, err)

		// Shutdown server to simulate failure
		ns.Shutdown()

		// Operations should handle disconnection gracefully
		_, err = client.GetLatestL2Block()
		// May or may not error depending on timing
		t.Log("GetLatestL2Block after shutdown:", err)

		// Client should still be functional when server comes back
		// (In real scenario, NATS client would reconnect automatically)
	})
}

func TestNATSClient_ChannelManagement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use the robust test infrastructure from behavior tests
	client := populateTestServerWithFixedDataReturnClient(t, ctx)
	defer client.Stop()

	err := client.Start()
	require.NoError(t, err)

	// Get initial channel pointer
	chanPtr1 := client.GetEntryChan()
	require.NotNil(t, chanPtr1)

	// Add some data to channel
	nc := client.nc
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	l2Block := &datastream.L2Block{
		Number:    1,
		Timestamp: uint64(time.Now().Unix()),
		Hash:      common.Hash{0x01}.Bytes(),
	}

	msgData, err := proto.Marshal(l2Block)
	require.NoError(t, err)

	headers := nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2Block))
	headers.Set("EntryNumber", "1")

	msg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    msgData,
		Header:  headers,
	}

	_, err = js.PublishMsg(ctx, msg)
	require.NoError(t, err)

	// Publish L2 block end to complete the block
	blockEnd := &datastream.L2BlockEnd{
		Number: 1,
	}

	blockEndData, err := proto.Marshal(blockEnd)
	require.NoError(t, err)

	headers = nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2BlockEnd))
	headers.Set("EntryNumber", "2")

	msg = &nats.Msg{
		Subject: "datastream.entry",
		Data:    blockEndData,
		Header:  headers,
	}

	_, err = js.PublishMsg(ctx, msg)
	require.NoError(t, err)

	// Start reading
	err = client.ReadAllEntriesToChannel()
	assert.NoError(t, err)

	// Wait for message to be processed
	select {
	case <-*chanPtr1:
		// Message received
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}

	// Renew channel
	client.RenewEntryChannel()

	// Get new channel pointer - it will be same address but different channel
	chanPtr2 := client.GetEntryChan()
	require.NotNil(t, chanPtr2)

	// The pointer addresses are the same (both point to c.entryChan field)
	assert.Equal(t, chanPtr1, chanPtr2, "channel pointers should be equal (same field)")

	// Test that the new channel works by sending a value
	testVal := "test_value"
	select {
	case *chanPtr2 <- testVal:
		// Successfully sent to new channel
		t.Log("Successfully sent test value to renewed channel")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("new channel should accept values")
	}

	// Verify we can receive the test value
	select {
	case val := <-*chanPtr2:
		assert.Equal(t, testVal, val, "should receive the test value")
		t.Log("Successfully received test value from renewed channel")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("should be able to receive from new channel")
	}
}

func TestNATSClient_BookmarkFunctionality(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Set up test server with manager that will store bookmarks
	streamServer, manager := setupTestServerWithManager(t, ctx)
	defer manager.Stop()

	// Get the server URL
	url, err := manager.URL()
	require.NoError(t, err)

	// Create client
	client := NewNATSClient(ctx, url, false, manager, log.New())
	defer client.Stop()

	err = client.Start()
	require.NoError(t, err)

	// Publish 5 complete blocks with bookmarks using the server
	// This ensures bookmarks are properly stored in KV
	for i := 1; i <= 5; i++ {
		// Start atomic operation for each block
		err = streamServer.StartAtomicOp()
		require.NoError(t, err)

		publishCompleteL2Block(t, streamServer, uint64(i), 1, 2) // block i, batch 1, 2 txs

		// Commit the atomic operation
		err = streamServer.CommitAtomicOp()
		require.NoError(t, err)
	}

	// Give time for messages to be processed
	time.Sleep(100 * time.Millisecond)

	// Start reading to populate client state
	err = client.ReadAllEntriesToChannel()
	assert.NoError(t, err)

	// Read the entries to ensure client processes them
	entryChan := client.GetEntryChan()
	blocksReceived := 0
	timeout := time.After(2 * time.Second)

	for blocksReceived < 5 {
		select {
		case entry := <-*entryChan:
			if block, ok := entry.(*types.FullL2Block); ok {
				blocksReceived++
				t.Logf("Received block %d", block.L2BlockNumber)
			}
		case <-timeout:
			t.Logf("Received %d blocks before timeout", blocksReceived)
			break
		}
	}

	// Test bookmark retrieval using GetL2BlockByNumber
	for i := 1; i <= 5; i++ {
		block, err := client.GetL2BlockByNumber(uint64(i))
		assert.NoError(t, err, "Should retrieve block %d via bookmark", i)
		assert.NotNil(t, block)
		assert.Equal(t, uint64(i), block.L2BlockNumber)
		assert.Equal(t, 2, len(block.L2Txs), "Each block should have 2 transactions")
	}
}

func TestNATSClient_Resilience(t *testing.T) {
	t.Run("context cancellation", func(t *testing.T) {
		ns, url := setupTestNATSServer(t)
		defer ns.Shutdown()

		// Create the stream that the client expects
		createTestStream(t, url)

		ctx, cancel := context.WithCancel(context.Background())

		client := NewNATSClient(ctx, url, false, nil, log.New())
		defer client.Stop()

		err := client.Start()
		require.NoError(t, err)

		// Start reading
		err = client.ReadAllEntriesToChannel()
		assert.NoError(t, err)

		// Cancel context
		cancel()

		// Wait a bit
		time.Sleep(500 * time.Millisecond)

		// Client should stop reading
		assert.False(t, client.reading.Load())
	})
}

// Benchmark tests
func BenchmarkNATSClient_Publish(b *testing.B) {
	ns, url := setupBenchNATSServer(b)
	defer ns.Shutdown()

	ctx := context.Background()

	client := NewNATSClient(ctx, url, false, nil, log.New())
	defer client.Stop()

	err := client.Start()
	require.NoError(b, err)

	nc := client.nc
	js, err := jetstream.New(nc)
	require.NoError(b, err)

	// Prepare message
	l2Block := &datastream.L2Block{
		Number:    1,
		Timestamp: uint64(time.Now().Unix()),
		Hash:      common.Hash{0x01}.Bytes(),
	}

	msgData, err := proto.Marshal(l2Block)
	require.NoError(b, err)

	headers := nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2Block))
	headers.Set("EntryNumber", "1")

	msg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    msgData,
		Header:  headers,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err = js.PublishMsg(ctx, msg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNATSClient_Read(b *testing.B) {
	ns, url := setupBenchNATSServer(b)
	defer ns.Shutdown()

	ctx := context.Background()

	client := NewNATSClient(ctx, url, false, nil, log.New())
	defer client.Stop()

	err := client.Start()
	require.NoError(b, err)

	// Pre-populate messages
	nc := client.nc
	js, err := jetstream.New(nc)
	require.NoError(b, err)

	l2Block := &datastream.L2Block{
		Number:    1,
		Timestamp: uint64(time.Now().Unix()),
		Hash:      common.Hash{0x01}.Bytes(),
	}

	msgData, err := proto.Marshal(l2Block)
	require.NoError(b, err)

	headers := nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2Block))
	headers.Set("EntryNumber", "1")

	for i := 0; i < b.N; i++ {
		msg := &nats.Msg{
			Subject: "datastream.entry",
			Data:    msgData,
			Header:  headers,
		}
		_, err = js.PublishMsg(ctx, msg)
		require.NoError(b, err)
	}

	// Start reading
	err = client.ReadAllEntriesToChannel()
	require.NoError(b, err)

	entryChan := client.GetEntryChan()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		select {
		case <-*entryChan:
			// Message received
		case <-time.After(5 * time.Second):
			b.Fatal("timeout waiting for message")
		}
	}
}

func setupBenchNATSServer(b *testing.B) (*server.Server, string) {
	b.Helper()

	opts := &server.Options{
		Port:      -1,
		JetStream: true,
		StoreDir:  b.TempDir(),
		NoLog:     true,
		NoSigs:    true,
	}

	ns, err := server.NewServer(opts)
	if err != nil {
		b.Fatal(err)
	}

	go ns.Start()

	if !ns.ReadyForConnections(5 * time.Second) {
		b.Fatal("NATS server failed to start")
	}

	return ns, fmt.Sprintf("nats://localhost:%d", ns.Addr().(*net.TCPAddr).Port)
}

func TestNATSClient_BasicFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use the robust test infrastructure from behavior tests
	client := populateTestServerWithDefaultDataReturnClient(t, ctx)

	// Launch processing in background so we can read from channel concurrently
	errChan := make(chan error, 1)
	go func() {
		err := client.ReadAllEntriesToChannel()
		errChan <- err
	}()

	entryChan := client.GetEntryChan()

	// Should receive the default data (1 complete block)
	var receivedBlock *types.FullL2Block
	for receivedBlock == nil {
		select {
		case entry := <-*entryChan:
			t.Logf("Received entry of type: %T", entry)
			if block, ok := entry.(*types.FullL2Block); ok {
				receivedBlock = block
				t.Logf("Successfully received complete block %d with %d transactions", block.L2BlockNumber, len(block.L2Txs))
			}
		case err := <-errChan:
			require.NoError(t, err, "ReadAllEntriesToChannel should not error for valid data")
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for complete block from default data")
		}
	}

	// Verify the block we received
	require.NotNil(t, receivedBlock, "Should have received a FullL2Block")
	assert.Equal(t, uint64(1), receivedBlock.L2BlockNumber)
	assert.True(t, len(receivedBlock.L2Txs) >= 1, "Block should have at least 1 transaction from default batch")
}

// TestNATSClient_GetTotalEntriesFromKV tests retrieving total entries from KV store
func TestNATSClient_GetTotalEntriesFromKV(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use the robust test infrastructure from behavior tests
	client := populateTestServerWithFixedDataReturnClient(t, ctx)
	defer client.Stop()

	err := client.Start()
	require.NoError(t, err)

	// Test GetTotalEntries should now read from KV
	totalEntries, err := client.GetTotalEntries()
	assert.NoError(t, err)
	assert.Equal(t, uint64(14), totalEntries)
}

// TestNATSClient_GetTotalEntriesTimeout tests timeout handling
func TestNATSClient_GetTotalEntriesTimeout(t *testing.T) {
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	// Create the stream first
	createTestStream(t, url)

	// Create client with normal context first
	client := NewNATSClient(context.Background(), url, false, nil, log.New())
	defer client.Stop()

	err := client.Start()
	require.NoError(t, err)

	// Create a cancelled context just for the GetTotalEntries call
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Override the client's context temporarily
	oldCtx := client.ctx
	client.ctx = ctx
	defer func() { client.ctx = oldCtx }()

	// GetTotalEntries should handle context cancellation gracefully
	_, err = client.GetTotalEntries()
	// Should either get a timeout error or succeed depending on timing
	// The important thing is it doesn't hang
	t.Logf("GetTotalEntries with cancelled context returned: %v", err)
}
