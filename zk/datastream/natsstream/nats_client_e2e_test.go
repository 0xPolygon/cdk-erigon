package natsstream

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_ConcurrentConsumers tests multiple consumers reading from same stream
func TestE2E_ConcurrentConsumers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get server + manager
	streamServer, manager := setupTestServerWithManager(t, ctx)
	defer manager.Stop()

	// Get URL for client connections
	url, err := manager.URL()
	require.NoError(t, err)

	const consumerCount = 3
	const totalBlocks = 3
	// Populate data once
	publishInAO(t, streamServer, func(t *testing.T, server *NATSStreamServer) {
		publishFixedBatch(t, server, 1, 1, totalBlocks, 8) // Fixed data
	})

	// Create multiple consumers using the same pattern as base client
	clients := make([]*NATSClient, consumerCount)
	entryChans := make([]*chan interface{}, consumerCount)

	for i := 0; i < consumerCount; i++ {
		// Create clients with minimal managers that won't initialize metadata
		// Since these are test consumers, they don't need full Manager functionality
		client := NewNATSClient(ctx, url, false, manager, NewTestLogger(t))
		err := client.Start()
		require.NoError(t, err)
		defer client.Stop()

		clients[i] = client
		entryChans[i] = client.GetEntryChan()
	}

	errChans := make([]chan error, consumerCount)
	for i := 0; i < consumerCount; i++ {
		errChans[i] = make(chan error, 1)
		go func(idx int) {
			err := clients[idx].ReadAllEntriesToChannel()
			errChans[idx] <- err
		}(i)
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
					if entry == nil {
						t.Logf("Consumer %d received nil entry", idx)
						continue
					}
					if block, ok := entry.(*types.FullL2Block); ok {
						results[idx][block.L2BlockNumber] = block
						receivedBlocks++
						if receivedBlocks%5 == 0 || receivedBlocks == 1 {
							t.Logf("Consumer %d received block %d (%d/%d total)",
								idx, block.L2BlockNumber, receivedBlocks, totalBlocks)
						}
					} else {
						t.Logf("Consumer %d received non-block entry: %T", idx, entry)
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
			if exists && block != nil {
				assert.Equal(t, uint64(blockNum), block.L2BlockNumber)
			} else if exists && block == nil {
				t.Errorf("Consumer %d has nil block for block number %d", consumerIdx, blockNum)
			}
		}
	}

	t.Logf("✅ All %d consumers successfully processed %d blocks", consumerCount, totalBlocks)
}

// TestE2E_StreamTruncationHandling tests behavior when stream gets truncated
func TestE2E_StreamTruncationHandling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1. Setup server with proper metadata
	streamServer, manager := setupTestServerWithManager(t, ctx)
	defer manager.Stop()

	url, err := manager.URL()
	require.NoError(t, err)

	// 2. Populate initial data with known boundaries
	publishInAO(t, streamServer, func(t *testing.T, server *NATSStreamServer) {
		publishFixedBatch(t, server, 1, 1, 5, 2) // 5 blocks, 2 tx each
	})

	// 3. Create client and start reading
	client := NewNATSClient(ctx, url, false, manager, NewTestLogger(t))
	err = client.Start()
	require.NoError(t, err)
	defer client.Stop()

	// 4. First read - get all initial data
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	// Verify we can read the initial blocks
	blocksReceived := 0
	timeout := time.After(5 * time.Second)

	for blocksReceived < 5 {
		select {
		case entry := <-*entryChan:
			if entry == nil {
				t.Logf("Consumer received nil entry")
				continue
			}
			if block, ok := entry.(*types.FullL2Block); ok {
				blocksReceived++
				t.Logf("Received block %d before truncation", block.L2BlockNumber)
			}
		case <-timeout:
			t.Fatalf("Timeout waiting for initial blocks (got %d/5)", blocksReceived)
		}
	}

	t.Log("✅ Received all initial blocks, now simulating truncation...")

	// 5. Perform truncation - truncate after block 3
	truncateAtEntry := uint64(17) // After block 3 complete
	err = streamServer.TruncateFile(truncateAtEntry)
	require.NoError(t, err)
	t.Logf("Truncated stream at entry %d (removing blocks 4-5)", truncateAtEntry)
	client.GetProgressAtomic().Store(3) // Update progress to reflect truncation

	// 6. Publish new batch after truncation
	publishInAO(t, streamServer, func(t *testing.T, server *NATSStreamServer) {
		publishFixedBatch(t, server, 2, 6, 3, 2) // Batch 2, blocks 6-8, 2 tx each
	})

	// 7. Second read - attempt to read new data after truncation
	errChan := make(chan error, 1)
	go func() {
		err := client.ReadAllEntriesToChannel()
		if err != nil {
			errChan <- err
		}
	}()

	// 8. Verify we receive the new blocks
	newBlocksReceived := 0
	timeout = time.After(50 * time.Second)

	for newBlocksReceived < 3 {
		select {
		case entry := <-*entryChan:
			if entry == nil {
				t.Logf("Consumer received nil entry")
				continue
			}
			if block, ok := entry.(*types.FullL2Block); ok {
				newBlocksReceived++
				t.Logf("Received new block %d after truncation", block.L2BlockNumber)

				// Verify we're getting the new blocks (6-8)
				assert.True(t, block.L2BlockNumber >= 6,
					"After truncation, should receive new blocks starting from 6")
			}
		case err := <-errChan:
			if err != nil {
				require.NoError(t, err, "ReadAllEntriesToChannel should not error after truncation")
			}
		case <-timeout:
			t.Fatalf("Timeout waiting for new blocks (got %d/3)", newBlocksReceived)
		}
	}

	t.Log("✅ Received new blocks after truncation")

	// 9. Third read - create completely fresh client to verify final stream state
	client.Stop()

	finalClient := NewNATSClient(ctx, url, false, manager, NewTestLogger(t))
	err = finalClient.Start()
	require.NoError(t, err)
	defer finalClient.Stop()

	go func() {
		err := finalClient.ReadAllEntriesToChannel()
		if err != nil {
			errChan <- err
		}
	}()

	finalEntryChan := finalClient.GetEntryChan()

	// Collect all blocks from the fresh client
	allBlocks := make(map[uint64]*types.FullL2Block)
	timeout = time.After(50 * time.Second)
	done := false

	for !done {
		select {
		case entry := <-*finalEntryChan:
			if entry == nil {
				t.Logf("Consumer received nil entry")
				continue
			}
			if block, ok := entry.(*types.FullL2Block); ok {
				allBlocks[block.L2BlockNumber] = block
				t.Logf("Fresh client received block %d", block.L2BlockNumber)
			} else if _, ok := entry.(*types.BatchEnd); ok {
				// Batch end signals we've read everything
				done = true
			}
		case err := <-errChan:
			if err != nil {
				require.NoError(t, err, "Fresh client ReadAllEntriesToChannel should not error")
			}
		case <-timeout:
			done = true
		}
	}

	// 10. Verify final state: blocks 1-3 from batch 1, blocks 6-8 from batch 2
	// Blocks 4-5 should be missing due to truncation
	assert.Equal(t, 6, len(allBlocks), "Should have exactly 6 blocks after truncation")

	// Verify blocks 1-3 exist
	for i := uint64(1); i <= 3; i++ {
		assert.Contains(t, allBlocks, i, "Block %d should exist (before truncation point)", i)
	}

	// Verify blocks 4-5 are missing (truncated)
	assert.NotContains(t, allBlocks, uint64(4), "Block 4 should be missing (truncated)")
	assert.NotContains(t, allBlocks, uint64(5), "Block 5 should be missing (truncated)")

	// Verify blocks 6-8 exist
	for i := uint64(6); i <= 8; i++ {
		assert.Contains(t, allBlocks, i, "Block %d should exist (added after truncation)", i)
	}

	t.Log("✅ Fresh client successfully read all non-truncated data: blocks 1-3 and 6-8")
}

// TestE2E_GracefulShutdown tests proper cleanup during shutdown
func TestE2E_GracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := populateTestServerWithDefaultDataReturnClient(t, ctx)

	// Start reading in background so we can read from channel concurrently
	errChan := make(chan error, 1)
	go func() {
		err := client.ReadAllEntriesToChannel()
		errChan <- err
	}()

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
	err := client.Stop()
	shutdownDuration := time.Since(shutdownStart)

	assert.NoError(t, err, "Stop should not return error")
	assert.Less(t, shutdownDuration, 5*time.Second, "Shutdown should complete quickly")

	// Verify client state after shutdown
	assert.False(t, client.started, "Client should not be started after stop")
	assert.False(t, client.reading.Load(), "Client should not be reading after stop")

	// Subsequent operations should fail appropriately
	err = client.ReadAllEntriesToChannel()
	assert.Error(t, err, "ReadAllEntriesToChannel should fail after stop")

	_, err = client.GetL2BlockByNumber(1)
	assert.Error(t, err, "GetL2BlockByNumber should fail after stop")

	t.Log("✅ Graceful shutdown completed successfully")
}
