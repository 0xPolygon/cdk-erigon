package natsstream

import (
	"context"
	"testing"
	"time"

	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_ServerPublishClientConsume performs end-to-end validation using server publishing and client consumption
func TestE2E_ServerPublishClientConsume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Step 1: Create server infrastructure with manager
	streamServer, manager := setupTestServerWithManager(t, ctx)
	defer manager.Stop()

	// Step 2: Publish realistic data using the server
	publishInAO(t, streamServer, func(t *testing.T, streamServer *NATSStreamServer) {
		// Publish a complete batch with realistic structure
		publishRealisticBatch(t, streamServer, 1, 1, 1) // batchNum=1, startBlock=1, blockCount=1
	})

	// Step 3: Create and start the client
	client := createTestClient(t, ctx, manager.url, manager)
	defer client.Stop()

	t.Log("📤 Server has published realistic batch with complete block")

	// Start client reading
	err := client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	// Collect entries from client
	t.Log("📥 Collecting entries from client...")
	var receivedEntries []interface{}
	timeout := time.After(10 * time.Second)

	// We expect to receive: BatchBookmark + BatchStart + L2BlockBookmark + FullL2Block + BatchEnd = 5 entries
	expectedClientEntries := 5

collectionLoop:
	for len(receivedEntries) < expectedClientEntries {
		select {
		case entry := <-*entryChan:
			if entry != nil { // Skip nil end-of-stream signals
				receivedEntries = append(receivedEntries, entry)
				t.Logf("📦 Received entry %d/%d: %T", len(receivedEntries), expectedClientEntries, entry)
			}
		case <-timeout:
			t.Logf("⏰ Timeout. Expected %d entries, got %d", expectedClientEntries, len(receivedEntries))
			// Break out of the for loop, not just the select
			break collectionLoop
		}
	}

	// Validate the received entries
	t.Log("🔍 Validating received entries...")

	require.GreaterOrEqual(t, len(receivedEntries), 1, "Should receive at least one entry")

	// Find the L2 block
	var receivedBlock *types.FullL2Block
	for _, entry := range receivedEntries {
		if block, ok := entry.(*types.FullL2Block); ok {
			receivedBlock = block
			break
		}
	}

	require.NotNil(t, receivedBlock, "Should receive a complete L2 block")
	assert.Equal(t, uint64(1), receivedBlock.L2BlockNumber, "Block number should be 1")
	assert.GreaterOrEqual(t, len(receivedBlock.L2Txs), 1, "Block should contain transactions")

	t.Logf("✅ Received complete block %d with %d transactions", receivedBlock.L2BlockNumber, len(receivedBlock.L2Txs))

	// Test GetLatestL2Block functionality
	t.Log("📊 Testing GetLatestL2Block...")
	latestBlock, err := client.GetLatestL2Block()
	require.NoError(t, err)

	if latestBlock != nil {
		assert.Equal(t, uint64(1), latestBlock.L2BlockNumber, "Latest block should be block 1")
		t.Logf("✅ Latest block is %d as expected", latestBlock.L2BlockNumber)
	} else {
		t.Log("⚠️  No latest block available (this may be expected)")
	}

	t.Log("✅ End-to-end test completed successfully!")
}

// TestE2E_MultipleBlocks tests processing multiple blocks in sequence
func TestE2E_MultipleBlocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Step 1: Create server infrastructure with manager
	streamServer, manager := setupTestServerWithManager(t, ctx)
	defer manager.Stop()

	// Step 2: Publish realistic data with multiple blocks using the server
	publishInAO(t, streamServer, func(t *testing.T, streamServer *NATSStreamServer) {
		// Publish a batch with 3 blocks
		publishRealisticBatch(t, streamServer, 1, 1, 3) // batchNum=1, startBlock=1, blockCount=3
	})

	// Step 3: Create and start the client
	client := createTestClient(t, ctx, manager.url, manager)
	defer client.Stop()

	t.Log("📤 Server has published batch with 3 blocks")

	// Start client reading
	err := client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	// Collect entries from client
	t.Log("📥 Collecting entries from client...")
	var receivedEntries []interface{}
	timeout := time.After(15 * time.Second)

	// We expect: BatchBookmark + BatchStart + (3 × (L2BlockBookmark + FullL2Block)) + BatchEnd = 8 entries
	expectedEntries := 8

multiBlockLoop:
	for len(receivedEntries) < expectedEntries {
		select {
		case entry := <-*entryChan:
			if entry != nil {
				receivedEntries = append(receivedEntries, entry)
				t.Logf("📦 Received entry %d/%d: %T", len(receivedEntries), expectedEntries, entry)
			}
		case <-timeout:
			t.Logf("⏰ Timeout. Expected %d, got %d", expectedEntries, len(receivedEntries))
			// Break out of the for loop, not just the select
			break multiBlockLoop
		}
	}

	// Validate we received all blocks
	var receivedBlocks []*types.FullL2Block
	for _, entry := range receivedEntries {
		if block, ok := entry.(*types.FullL2Block); ok {
			receivedBlocks = append(receivedBlocks, block)
		}
	}

	blockCount := 3
	assert.Equal(t, blockCount, len(receivedBlocks), "Should receive %d blocks", blockCount)

	// Validate block sequence and transaction counts
	for i, block := range receivedBlocks {
		expectedBlockNum := uint64(i + 1)
		assert.Equal(t, expectedBlockNum, block.L2BlockNumber, "Block %d number mismatch", i)
		assert.GreaterOrEqual(t, len(block.L2Txs), 3, "Block %d should have at least 3 transactions", expectedBlockNum)
	}

	t.Logf("✅ Successfully processed %d blocks in sequence", blockCount)
}
