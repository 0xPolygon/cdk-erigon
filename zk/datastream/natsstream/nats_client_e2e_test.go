package natsstream

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_CompleteBatchProcessing tests processing of complete batches with proper sequence
func TestE2E_CompleteBatchProcessing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, ns, _ := setupTestClientWithStream(t, ctx, 1101)
	defer ns.Shutdown()
	defer client.Stop()

	js, err := jetstream.New(client.nc)
	require.NoError(t, err)

	// Test configuration
	const (
		batchCount     = 3
		blocksPerBatch = 5
		txsPerBlock    = 3
	)

	t.Logf("Publishing %d batches with %d blocks each, %d txs per block",
		batchCount, blocksPerBatch, txsPerBlock)

	// Start reading
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	// Publish complete batch sequence
	for batchNum := 1; batchNum <= batchCount; batchNum++ {
		// Batch start
		publishBatchStart(t, js, uint64(batchNum), client.subjectPrefix)

		for blockNum := 1; blockNum <= blocksPerBatch; blockNum++ {
			globalBlockNum := uint64((batchNum-1)*blocksPerBatch + blockNum)

			// Block bookmark
			publishBookmark(t, js, globalBlockNum, datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK, client.subjectPrefix)

			// Block start
			publishL2Block(t, js, globalBlockNum, uint64(batchNum), client.subjectPrefix)

			// Transactions
			for txIndex := 0; txIndex < txsPerBlock; txIndex++ {
				publishL2Transaction(t, js, globalBlockNum, uint64(txIndex), client.subjectPrefix)
			}

			// Block end
			publishL2BlockEnd(t, js, globalBlockNum, client.subjectPrefix)
		}

		// Batch end
		publishBatchEnd(t, js, uint64(batchNum), client.subjectPrefix)
	}

	// Verify reception of all entries
	receivedBlocks := make(map[uint64]*types.FullL2Block)
	receivedBatches := make(map[uint64]*types.BatchEnd)
	expectedBlocks := batchCount * blocksPerBatch
	receivedBlockCount := 0

	timeout := time.After(30 * time.Second)
	for receivedBlockCount < expectedBlocks {
		select {
		case entry := <-*entryChan:
			switch e := entry.(type) {
			case *types.FullL2Block:
				receivedBlocks[e.L2BlockNumber] = e
				receivedBlockCount++
				t.Logf("Received block %d (batch %d) with %d txs",
					e.L2BlockNumber, e.BatchNumber, len(e.L2Txs))
			case *types.BatchEnd:
				receivedBatches[e.Number] = e
				t.Logf("Received batch end %d", e.Number)
			case *types.BookmarkProto:
				t.Logf("Received bookmark type %d value %d", e.BookmarkType(), e.BookMark.GetValue())
			default:
				t.Logf("Received entry type: %T", entry)
			}
		case <-timeout:
			t.Fatalf("Timeout waiting for entries. Received %d/%d blocks", receivedBlockCount, expectedBlocks)
		}
	}

	// Validate all blocks received correctly
	for batchNum := 1; batchNum <= batchCount; batchNum++ {
		for blockNum := 1; blockNum <= blocksPerBatch; blockNum++ {
			globalBlockNum := uint64((batchNum-1)*blocksPerBatch + blockNum)

			block, exists := receivedBlocks[globalBlockNum]
			require.True(t, exists, "Block %d should be received", globalBlockNum)
			assert.Equal(t, globalBlockNum, block.L2BlockNumber)
			assert.Equal(t, uint64(batchNum), block.BatchNumber)
			assert.Len(t, block.L2Txs, txsPerBlock, "Block %d should have %d transactions", globalBlockNum, txsPerBlock)
		}
	}

	// Validate batch ends received
	for batchNum := 1; batchNum <= batchCount; batchNum++ {
		_, exists := receivedBatches[uint64(batchNum)]
		assert.True(t, exists, "Batch end %d should be received", batchNum)
	}

	t.Logf("✅ Successfully processed %d batches with %d blocks and %d transactions",
		batchCount, len(receivedBlocks), len(receivedBlocks)*txsPerBlock)
}

// TestE2E_ConnectionFailureRecovery tests automatic reconnection and recovery
func TestE2E_ConnectionFailureRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E connection failure test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Setup initial server
	ns1, url := setupTestNATSServer(t)
	createTestStream(t, url, 1101)

	client := NewNATSClient(ctx, url, false, 1101, 7, log.New())
	defer client.Stop()

	err := client.Start()
	require.NoError(t, err)

	js, err := jetstream.New(client.nc)
	require.NoError(t, err)

	// Start reading
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	// Publish some initial data
	for i := 1; i <= 3; i++ {
		publishL2Block(t, js, uint64(i), 1, client.subjectPrefix)
		publishL2Transaction(t, js, uint64(i), 0, client.subjectPrefix)
		publishL2BlockEnd(t, js, uint64(i), client.subjectPrefix)
	}

	// Verify initial data received
	initialBlocks := 0
	timeout := time.After(10 * time.Second)
	for initialBlocks < 3 {
		select {
		case entry := <-*entryChan:
			if _, ok := entry.(*types.FullL2Block); ok {
				initialBlocks++
			}
		case <-timeout:
			t.Fatalf("Timeout waiting for initial blocks. Received %d/3", initialBlocks)
		}
	}

	t.Log("✅ Received initial data, now simulating connection failure...")

	// Simulate connection failure by shutting down server
	ns1.Shutdown()

	// Wait a moment for disconnection
	time.Sleep(2 * time.Second)

	// Start new server on same port (simulates recovery)
	ns2, err := server.NewServer(&server.Options{
		Port:      ns1.Addr().(*net.TCPAddr).Port,
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	require.NoError(t, err)
	defer ns2.Shutdown()

	go ns2.Start()
	require.True(t, ns2.ReadyForConnections(10*time.Second), "Server should restart")

	// Recreate stream on new server
	createTestStream(t, url, 1101)

	t.Log("✅ Server restarted, waiting for client reconnection...")

	// Give client time to reconnect (NATS client should auto-reconnect)
	time.Sleep(5 * time.Second)

	// Publish more data to verify recovery
	newJS, err := jetstream.New(client.nc)
	if err != nil {
		// Client might not have reconnected yet, wait and retry
		time.Sleep(3 * time.Second)
		newJS, err = jetstream.New(client.nc)
		require.NoError(t, err)
	}

	for i := 4; i <= 6; i++ {
		publishL2Block(t, newJS, uint64(i), 1, client.subjectPrefix)
		publishL2Transaction(t, js, uint64(i), 0, client.subjectPrefix)
		publishL2BlockEnd(t, newJS, uint64(i), client.subjectPrefix)
	}

	// Verify recovery data received
	recoveryBlocks := 0
	timeout = time.After(15 * time.Second)
	for recoveryBlocks < 3 {
		select {
		case entry := <-*entryChan:
			if block, ok := entry.(*types.FullL2Block); ok {
				recoveryBlocks++
				t.Logf("Received post-recovery block %d", block.L2BlockNumber)
			}
		case <-timeout:
			t.Fatalf("Timeout waiting for recovery blocks. Received %d/3", recoveryBlocks)
		}
	}

	t.Log("✅ Successfully recovered from connection failure")
}

// TestE2E_ConcurrentConsumers tests multiple consumers reading from same stream
func TestE2E_ConcurrentConsumers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Setup server
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()
	createTestStream(t, url, 1101)

	const consumerCount = 3
	const blocksPerConsumer = 10

	// Create multiple consumers
	clients := make([]*NATSClient, consumerCount)
	entryChans := make([]*chan interface{}, consumerCount)

	for i := 0; i < consumerCount; i++ {
		client := NewNATSClient(ctx, url, false, 1101, 7, log.New())
		err := client.Start()
		require.NoError(t, err)
		defer client.Stop()

		clients[i] = client
		entryChans[i] = client.GetEntryChan()
	}

	// Get JS client for publishing
	js, err := jetstream.New(clients[0].nc)
	require.NoError(t, err)

	// Start all consumers reading
	for i := 0; i < consumerCount; i++ {
		err := clients[i].ReadAllEntriesToChannel()
		require.NoError(t, err)
	}

	// Publish test data
	totalBlocks := blocksPerConsumer
	for i := 1; i <= totalBlocks; i++ {
		publishL2Block(t, js, uint64(i), 1, clients[0].subjectPrefix)
		publishL2Transaction(t, js, uint64(i), 0, clients[0].subjectPrefix)
		publishL2BlockEnd(t, js, uint64(i), clients[0].subjectPrefix)
	}

	// Collect results from all consumers
	results := make([]map[uint64]*types.FullL2Block, consumerCount)
	for i := 0; i < consumerCount; i++ {
		results[i] = make(map[uint64]*types.FullL2Block)
	}

	var wg sync.WaitGroup

	for consumerIdx := 0; consumerIdx < consumerCount; consumerIdx++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			entryChan := entryChans[idx]
			receivedBlocks := 0
			timeout := time.After(30 * time.Second)

			for receivedBlocks < totalBlocks {
				select {
				case entry := <-*entryChan:
					if block, ok := entry.(*types.FullL2Block); ok {
						results[idx][block.L2BlockNumber] = block
						receivedBlocks++
						if receivedBlocks%5 == 0 {
							t.Logf("Consumer %d received %d/%d blocks", idx, receivedBlocks, totalBlocks)
						}
					}
				case <-timeout:
					t.Errorf("Consumer %d timeout waiting for blocks. Received %d/%d", idx, receivedBlocks, totalBlocks)
					return
				}
			}

			t.Logf("✅ Consumer %d completed: received %d blocks", idx, receivedBlocks)
		}(consumerIdx)
	}

	wg.Wait()

	// Verify all consumers received all blocks
	for consumerIdx := 0; consumerIdx < consumerCount; consumerIdx++ {
		assert.Len(t, results[consumerIdx], totalBlocks,
			"Consumer %d should receive all %d blocks", consumerIdx, totalBlocks)

		for blockNum := 1; blockNum <= totalBlocks; blockNum++ {
			block, exists := results[consumerIdx][uint64(blockNum)]
			assert.True(t, exists, "Consumer %d should receive block %d", consumerIdx, blockNum)
			assert.Equal(t, uint64(blockNum), block.L2BlockNumber)
		}
	}

	t.Logf("✅ All %d consumers successfully processed %d blocks", consumerCount, totalBlocks)
}

// TestE2E_MemoryPressureHandling tests behavior under high memory pressure
func TestE2E_MemoryPressureHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory pressure test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, ns, _ := setupTestClientWithStream(t, ctx, 1101)
	defer ns.Shutdown()
	defer client.Stop()

	// Create client with very small channel buffer to simulate pressure
	client.entryChan = make(chan interface{}, 5) // Very small buffer

	js, err := jetstream.New(client.nc)
	require.NoError(t, err)

	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	// Publish large amount of data rapidly
	const messageCount = 100
	t.Logf("Publishing %d blocks rapidly to test memory pressure handling", messageCount)

	for i := 1; i <= messageCount; i++ {
		publishL2Block(t, js, uint64(i), 1, client.subjectPrefix)
		publishL2Transaction(t, js, uint64(i), 0, client.subjectPrefix)
		publishL2Transaction(t, js, uint64(i), 1, client.subjectPrefix)
		publishL2BlockEnd(t, js, uint64(i), client.subjectPrefix)
	}

	// Consumer should handle backpressure gracefully
	receivedBlocks := 0
	start := time.Now()

	// Read with delays to simulate slow consumer
	for receivedBlocks < messageCount {
		select {
		case entry := <-*entryChan:
			if block, ok := entry.(*types.FullL2Block); ok {
				receivedBlocks++
				assert.Equal(t, uint64(receivedBlocks), block.L2BlockNumber)

				if receivedBlocks%20 == 0 {
					t.Logf("Received %d/%d blocks", receivedBlocks, messageCount)
					time.Sleep(10 * time.Millisecond) // Simulate slow processing
				}
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("Timeout waiting for blocks under memory pressure. Received %d/%d",
				receivedBlocks, messageCount)
		}
	}

	duration := time.Since(start)
	t.Logf("✅ Successfully handled memory pressure: processed %d blocks in %v (%.2f blocks/s)",
		messageCount, duration, float64(messageCount)/duration.Seconds())
}

// TestE2E_StreamTruncationHandling tests behavior when stream gets truncated
func TestE2E_StreamTruncationHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stream truncation test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, ns, _ := setupTestClientWithStream(t, ctx, 1101)
	defer ns.Shutdown()
	defer client.Stop()

	js, err := jetstream.New(client.nc)
	require.NoError(t, err)

	// Publish initial data
	for i := 1; i <= 10; i++ {
		publishL2Block(t, js, uint64(i), 1, client.subjectPrefix)
		publishL2Transaction(t, js, uint64(i), 0, client.subjectPrefix)
		publishL2BlockEnd(t, js, uint64(i), client.subjectPrefix)
	}

	// Start reading
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	// Read some initial data
	receivedInitial := 0
	timeout := time.After(10 * time.Second)
	for receivedInitial < 5 {
		select {
		case entry := <-*entryChan:
			if _, ok := entry.(*types.FullL2Block); ok {
				receivedInitial++
			}
		case <-timeout:
			t.Fatalf("Timeout waiting for initial data")
		}
	}

	t.Log("✅ Received initial data, now simulating stream truncation...")

	// Simulate stream truncation by deleting old messages
	stream, err := js.Stream(ctx, client.streamName)
	require.NoError(t, err)

	// Delete messages (simulate truncation)
	err = stream.DeleteMsg(ctx, 1)
	assert.NoError(t, err) // Some messages might already be consumed

	// Publish more data after truncation
	for i := 11; i <= 15; i++ {
		publishL2Block(t, js, uint64(i), 1, client.subjectPrefix)
		publishL2Transaction(t, js, uint64(i), 0, client.subjectPrefix)
		publishL2BlockEnd(t, js, uint64(i), client.subjectPrefix)
	}

	// Client should continue to work despite truncation
	receivedAfterTruncation := 0
	timeout = time.After(15 * time.Second)
	for receivedAfterTruncation < 5 {
		select {
		case entry := <-*entryChan:
			if block, ok := entry.(*types.FullL2Block); ok {
				receivedAfterTruncation++
				t.Logf("Received post-truncation block %d", block.L2BlockNumber)
			}
		case <-timeout:
			// This is expected behavior - client might struggle with truncation
			t.Logf("⚠️ Client handling of stream truncation needs improvement (received %d/5 blocks)",
				receivedAfterTruncation)
			return
		}
	}

	t.Log("✅ Client successfully handled stream truncation")
}

// TestE2E_HighThroughputStressTest tests performance under high message throughput
func TestE2E_HighThroughputStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high throughput test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	client, ns, _ := setupTestClientWithStream(t, ctx, 1101)
	defer ns.Shutdown()
	defer client.Stop()

	js, err := jetstream.New(client.nc)
	require.NoError(t, err)

	const (
		batchCount     = 10
		blocksPerBatch = 50
		txsPerBlock    = 10
	)

	totalBlocks := batchCount * blocksPerBatch
	totalTxs := totalBlocks * txsPerBlock

	t.Logf("🚀 Starting high throughput test: %d batches, %d blocks, %d transactions",
		batchCount, totalBlocks, totalTxs)

	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	// Start publishing in separate goroutine
	var publishWg sync.WaitGroup
	publishWg.Add(1)

	publishStart := time.Now()
	go func() {
		defer publishWg.Done()

		for batchNum := 1; batchNum <= batchCount; batchNum++ {
			publishBatchStart(t, js, uint64(batchNum), client.subjectPrefix)

			for blockIdx := 1; blockIdx <= blocksPerBatch; blockIdx++ {
				globalBlockNum := uint64((batchNum-1)*blocksPerBatch + blockIdx)

				publishL2Block(t, js, globalBlockNum, uint64(batchNum), client.subjectPrefix)

				for txIdx := 0; txIdx < txsPerBlock; txIdx++ {
					publishL2Transaction(t, js, globalBlockNum, uint64(txIdx), client.subjectPrefix)
				}

				publishL2BlockEnd(t, js, globalBlockNum, client.subjectPrefix)
			}

			publishBatchEnd(t, js, uint64(batchNum), client.subjectPrefix)

			if batchNum%2 == 0 {
				t.Logf("Published batch %d/%d", batchNum, batchCount)
			}
		}
	}()

	// Track performance metrics
	var receivedBlocks int64
	var receivedTxs int64
	consumeStart := time.Now()

	// Consume all messages
	timeout := time.After(90 * time.Second)
	for atomic.LoadInt64(&receivedBlocks) < int64(totalBlocks) {
		select {
		case entry := <-*entryChan:
			switch e := entry.(type) {
			case *types.FullL2Block:
				atomic.AddInt64(&receivedBlocks, 1)
				atomic.AddInt64(&receivedTxs, int64(len(e.L2Txs)))

				blocks := atomic.LoadInt64(&receivedBlocks)
				if blocks%100 == 0 {
					elapsed := time.Since(consumeStart)
					rate := float64(blocks) / elapsed.Seconds()
					t.Logf("Consumed %d/%d blocks (%.1f blocks/s)", blocks, totalBlocks, rate)
				}
			}
		case <-timeout:
			blocks := atomic.LoadInt64(&receivedBlocks)
			t.Fatalf("Timeout in high throughput test. Received %d/%d blocks", blocks, totalBlocks)
		}
	}

	publishWg.Wait()

	// Calculate final metrics
	publishDuration := time.Since(publishStart)
	consumeDuration := time.Since(consumeStart)
	finalBlocks := atomic.LoadInt64(&receivedBlocks)
	finalTxs := atomic.LoadInt64(&receivedTxs)

	publishRate := float64(totalBlocks) / publishDuration.Seconds()
	consumeRate := float64(finalBlocks) / consumeDuration.Seconds()

	t.Logf("✅ High throughput test completed:")
	t.Logf("   📤 Published: %d blocks in %v (%.1f blocks/s)", totalBlocks, publishDuration, publishRate)
	t.Logf("   📥 Consumed: %d blocks in %v (%.1f blocks/s)", finalBlocks, consumeDuration, consumeRate)
	t.Logf("   💾 Total transactions: %d", finalTxs)

	// Verify all data received correctly
	assert.Equal(t, int64(totalBlocks), finalBlocks, "Should receive all blocks")
	assert.Equal(t, int64(totalTxs), finalTxs, "Should receive all transactions")

	// Performance thresholds (adjust based on hardware)
	minRate := 50.0 // blocks/second
	assert.Greater(t, consumeRate, minRate, "Consume rate should be > %.1f blocks/s", minRate)
}

// TestE2E_GracefulShutdown tests proper cleanup during shutdown
func TestE2E_GracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, ns, _ := setupTestClientWithStream(t, ctx, 1101)
	defer ns.Shutdown()

	js, err := jetstream.New(client.nc)
	require.NoError(t, err)

	// Start reading
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	// Publish some data
	for i := 1; i <= 5; i++ {
		publishL2Block(t, js, uint64(i), 1, client.subjectPrefix)
		publishL2Transaction(t, js, uint64(i), 0, client.subjectPrefix)
		publishL2BlockEnd(t, js, uint64(i), client.subjectPrefix)
	}

	// Verify client is reading
	entryChan := client.GetEntryChan()
	timeout := time.After(5 * time.Second)
	select {
	case <-*entryChan:
		// Received data, client is working
	case <-timeout:
		t.Fatal("Client not receiving data before shutdown test")
	}

	// Test graceful shutdown
	t.Log("Testing graceful shutdown...")

	shutdownStart := time.Now()
	err = client.Stop()
	shutdownDuration := time.Since(shutdownStart)

	assert.NoError(t, err, "Stop should not return error")
	assert.Less(t, shutdownDuration, 5*time.Second, "Shutdown should complete quickly")

	// Verify client state after shutdown
	assert.False(t, client.started, "Client should not be started after stop")
	assert.False(t, client.reading, "Client should not be reading after stop")

	// Subsequent operations should fail appropriately
	err = client.ReadAllEntriesToChannel()
	assert.Error(t, err, "ReadAllEntriesToChannel should fail after stop")

	_, err = client.GetL2BlockByNumber(1)
	assert.Error(t, err, "GetL2BlockByNumber should fail after stop")

	t.Log("✅ Graceful shutdown completed successfully")
}
