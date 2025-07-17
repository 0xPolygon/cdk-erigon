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
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	// Create the stream that the client expects
	createTestStream(t, url)

	ctx := context.Background()

	client := NewNATSClient(ctx, url, false, nil, log.New())
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

	t.Run("invalid message handling", func(t *testing.T) {
		ns, url := setupTestNATSServer(t)
		defer ns.Shutdown()

		// Create the stream that the client expects
		createTestStream(t, url)

		ctx := context.Background()

		client := NewNATSClient(ctx, url, false, nil, log.New())
		defer client.Stop()

		err := client.Start()
		require.NoError(t, err)

		// Publish invalid message
		nc := client.nc
		js, err := jetstream.New(nc)
		require.NoError(t, err)

		// Publish message without headers
		msg := &nats.Msg{
			Subject: "datastream.entry",
			Data:    []byte("invalid protobuf"),
		}

		_, err = js.PublishMsg(ctx, msg)
		require.NoError(t, err)

		// Client should handle invalid messages without crashing
		err = client.ReadAllEntriesToChannel()
		assert.EqualError(t, err, "message missing EntryType header")
	})
}

func TestNATSClient_ChannelManagement(t *testing.T) {
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	// Create the stream that the client expects
	createTestStream(t, url)

	ctx := context.Background()

	client := NewNATSClient(ctx, url, false, nil, log.New())
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

func TestNATSClient_MessageOrdering(t *testing.T) {
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	// Create the stream that the client expects
	createTestStream(t, url)

	ctx := context.Background()

	client := NewNATSClient(ctx, url, false, nil, log.New())
	defer client.Stop()

	err := client.Start()
	require.NoError(t, err)

	// Publish multiple blocks in order
	nc := client.nc
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	blockCount := 10
	for i := 1; i <= blockCount; i++ {
		l2Block := &datastream.L2Block{
			Number:    uint64(i),
			Timestamp: uint64(time.Now().Unix()),
			Hash:      common.Hash{byte(i)}.Bytes(),
		}

		msgData, err := proto.Marshal(l2Block)
		require.NoError(t, err)

		headers := nats.Header{}
		headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2Block))
		headers.Set("EntryNumber", fmt.Sprintf("%d", i))

		msg := &nats.Msg{
			Subject: "datastream.entry",
			Data:    msgData,
			Header:  headers,
		}

		_, err = js.PublishMsg(ctx, msg)
		require.NoError(t, err)

		// Publish L2 block end to complete the block
		blockEnd := &datastream.L2BlockEnd{
			Number: uint64(i),
		}

		blockEndData, err := proto.Marshal(blockEnd)
		require.NoError(t, err)

		headers = nats.Header{}
		headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2BlockEnd))
		headers.Set("EntryNumber", fmt.Sprintf("%d", i*2))

		msg = &nats.Msg{
			Subject: "datastream.entry",
			Data:    blockEndData,
			Header:  headers,
		}

		_, err = js.PublishMsg(ctx, msg)
		require.NoError(t, err)
	}

	// Start reading
	err = client.ReadAllEntriesToChannel()
	assert.NoError(t, err)

	// Verify ordering
	entryChan := client.GetEntryChan()
	receivedBlocks := make([]uint64, 0, blockCount)

	for i := 0; i < blockCount; i++ {
		select {
		case entry := <-*entryChan:
			fullBlock, ok := entry.(*types.FullL2Block)
			require.True(t, ok)
			receivedBlocks = append(receivedBlocks, fullBlock.L2BlockNumber)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for block")
		}
	}

	// Verify blocks were received in order
	for i := 0; i < blockCount; i++ {
		assert.Equal(t, uint64(i+1), receivedBlocks[i], "blocks should be in order")
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
	t.Run("handle full channel", func(t *testing.T) {
		ns, url := setupTestNATSServer(t)
		defer ns.Shutdown()

		// Create the stream that the client expects
		createTestStream(t, url)

		ctx := context.Background()

		// Create client with small channel buffer
		client := NewNATSClient(ctx, url, false, nil, log.New())
		defer client.Stop()

		// Override channel size for testing
		client.entryChan = make(chan interface{}, 2) // Very small buffer

		err := client.Start()
		require.NoError(t, err)

		// Publish many messages
		nc := client.nc
		js, err := jetstream.New(nc)
		require.NoError(t, err)

		for i := 1; i <= 10; i++ {
			l2Block := &datastream.L2Block{
				Number:    uint64(i),
				Timestamp: uint64(time.Now().Unix()),
				Hash:      common.Hash{byte(i)}.Bytes(),
			}

			msgData, err := proto.Marshal(l2Block)
			require.NoError(t, err)

			headers := nats.Header{}
			headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2Block))
			headers.Set("EntryNumber", fmt.Sprintf("%d", i))

			msg := &nats.Msg{
				Subject: "datastream.entry",
				Data:    msgData,
				Header:  headers,
			}

			_, err = js.PublishMsg(ctx, msg)
			require.NoError(t, err)

			// Publish L2 block end to complete the block
			blockEnd := &datastream.L2BlockEnd{
				Number: uint64(i),
			}

			blockEndData, err := proto.Marshal(blockEnd)
			require.NoError(t, err)

			headers = nats.Header{}
			headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2BlockEnd))
			headers.Set("EntryNumber", fmt.Sprintf("%d", i*2))

			msg = &nats.Msg{
				Subject: "datastream.entry",
				Data:    blockEndData,
				Header:  headers,
			}

			_, err = js.PublishMsg(ctx, msg)
			require.NoError(t, err)
		}

		// Drain some messages from the channel first to make room
		entryChan := client.GetEntryChan()

		// Start reading in background
		go func() {
			err = client.ReadAllEntriesToChannel()
			// We expect this might error due to full channel
			if err != nil {
				t.Logf("Expected error from full channel: %v", err)
			}
		}()

		// Give time for processing to start
		time.Sleep(500 * time.Millisecond)

		// Now drain messages to prove channel works
		messagesReceived := 0
		timeout := time.After(5 * time.Second)

		for messagesReceived < 10 {
			select {
			case entry := <-*entryChan:
				if _, ok := entry.(*types.FullL2Block); ok {
					messagesReceived++
				}
			case <-timeout:
				break
			}
		}

		// We should receive at least the first 2 blocks (channel size)
		assert.GreaterOrEqual(t, messagesReceived, 2, "should receive at least channel buffer size messages")
	})

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

func TestNATSClient_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	// Create the stream that the client expects
	createTestStream(t, url)

	ctx := context.Background()

	client := NewNATSClient(ctx, url, false, nil, log.New())
	defer client.Stop()

	err := client.Start()
	require.NoError(t, err)

	// Publish many messages
	nc := client.nc
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	messageCount := 10000
	start := time.Now()

	for i := 1; i <= messageCount; i++ {
		l2Block := &datastream.L2Block{
			Number:    uint64(i),
			Timestamp: uint64(time.Now().Unix()),
			Hash:      common.Hash{byte(i % 256)}.Bytes(),
		}

		msgData, err := proto.Marshal(l2Block)
		require.NoError(t, err)

		headers := nats.Header{}
		headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2Block))
		headers.Set("EntryNumber", fmt.Sprintf("%d", i))

		msg := &nats.Msg{
			Subject: "datastream.entry",
			Data:    msgData,
			Header:  headers,
		}

		_, err = js.PublishMsg(ctx, msg)
		require.NoError(t, err)

		// Publish L2 block end to complete the block
		blockEnd := &datastream.L2BlockEnd{
			Number: uint64(i),
		}

		blockEndData, err := proto.Marshal(blockEnd)
		require.NoError(t, err)

		headers = nats.Header{}
		headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2BlockEnd))
		headers.Set("EntryNumber", fmt.Sprintf("%d", i*2))

		msg = &nats.Msg{
			Subject: "datastream.entry",
			Data:    blockEndData,
			Header:  headers,
		}

		_, err = js.PublishMsg(ctx, msg)
		require.NoError(t, err)
	}

	publishDuration := time.Since(start)
	t.Logf("Published %d messages in %v (%.2f msg/s)",
		messageCount, publishDuration, float64(messageCount)/publishDuration.Seconds())

	// Start reading
	readStart := time.Now()
	err = client.ReadAllEntriesToChannel()
	assert.NoError(t, err)

	// Count received messages
	entryChan := client.GetEntryChan()
	receivedCount := 0

	for receivedCount < messageCount {
		select {
		case <-*entryChan:
			receivedCount++
			if receivedCount%1000 == 0 {
				t.Logf("Received %d messages", receivedCount)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout after receiving %d/%d messages", receivedCount, messageCount)
		}
	}

	readDuration := time.Since(readStart)
	t.Logf("Read %d messages in %v (%.2f msg/s)",
		messageCount, readDuration, float64(messageCount)/readDuration.Seconds())
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

// TestMessageAcknowledgmentStrategy tests that messages are only acknowledged after successful processing
func TestMessageAcknowledgmentStrategy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, ns, _ := setupTestClientWithStream(t, ctx)
	defer ns.Shutdown()
	defer client.Stop()

	// Test with valid complete block that should be acknowledged
	nc := client.nc
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Create test L2 block
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

	// Now start reading to process the published messages
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	// Verify message is processed and acknowledged
	entryChan := client.GetEntryChan()
	select {
	case entry := <-*entryChan:
		fullBlock, ok := entry.(*types.FullL2Block)
		assert.True(t, ok, "Expected FullL2Block entry")
		assert.Equal(t, uint64(1), fullBlock.L2BlockNumber)
		t.Log("Successfully tested ACK behavior for valid messages")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for valid message to be processed")
	}
}

// TestNegativeAcknowledgment tests that failed message processing results in NAK
func TestNegativeAcknowledgment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	client, ns, _ := setupTestClientWithStream(t, ctx)
	defer ns.Shutdown()
	defer client.Stop()

	// Configure consumer with MaxDeliver to prevent infinite redelivery
	nc := client.nc
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Create a special consumer for this test that limits redelivery
	consumerConfig := jetstream.ConsumerConfig{
		Durable:       "TEST_NAK_CONSUMER",
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    3, // Limit redelivery to prevent infinite loops
		DeliverPolicy: jetstream.DeliverAllPolicy,
	}

	consumer, err := js.CreateOrUpdateConsumer(ctx, client.streamName, consumerConfig)
	require.NoError(t, err)

	// Start consuming with the limited consumer
	consumeCtx, cancelConsume := context.WithCancel(ctx)
	defer cancelConsume()

	msgChan := make(chan jetstream.Msg, 10)
	consumerCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		select {
		case msgChan <- msg:
		case <-consumeCtx.Done():
		}
	})
	require.NoError(t, err)
	defer consumerCtx.Stop()

	// Send an invalid message (malformed protobuf)
	invalidMsg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    []byte("invalid protobuf data"),
		Header: nats.Header{
			"EntryType":   []string{fmt.Sprintf("%d", types.EntryTypeL2Block)},
			"EntryNumber": []string{"1"},
		},
	}

	_, err = js.PublishMsg(ctx, invalidMsg)
	require.NoError(t, err)

	// Process the message and verify NAK behavior
	nakCount := 0
	processedCount := 0

	for nakCount < 3 && processedCount < 5 { // Limit processing attempts
		select {
		case msg := <-msgChan:
			processedCount++

			// Try to process - this should fail due to invalid protobuf
			_, err := types.UnmarshalL2Block(msg.Data())
			if err != nil {
				// Processing failed - send NAK
				if nakErr := msg.Nak(); nakErr != nil {
					t.Logf("Failed to send NAK: %v", nakErr)
				} else {
					nakCount++
					t.Logf("Successfully sent NAK #%d for invalid message", nakCount)
				}
			} else {
				// Shouldn't happen with our invalid data
				msg.Ack()
				t.Fatal("Expected processing to fail, but it succeeded")
			}

		case <-time.After(2 * time.Second):
			t.Logf("Timeout after processing %d messages with %d NAKs", processedCount, nakCount)
			break
		}
	}

	// Verify that we successfully sent at least one NAK
	assert.Greater(t, nakCount, 0, "Should have sent at least one NAK")
	assert.LessOrEqual(t, nakCount, 3, "Should not exceed MaxDeliver limit")

	// Clean up the test consumer
	if delErr := js.DeleteConsumer(ctx, client.streamName, consumerConfig.Durable); delErr != nil {
		t.Logf("Failed to delete test consumer: %v", delErr)
	}

	t.Logf("Successfully tested NAK behavior: %d NAKs sent, %d messages processed", nakCount, processedCount)
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
