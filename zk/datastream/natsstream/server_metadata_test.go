package natsstream

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestServerBookmarkStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Step 1: Create server infrastructure with manager
	streamServer, manager := setupTestServerWithManager(t, ctx)
	defer manager.Stop()

	// Step 2: Publish realistic data with multiple blocks using the server
	publishInAO(t, streamServer, func(t *testing.T, streamServer *NATSStreamServer) {
		// Publish 5 complete blocks for bookmark testing
		for blockNum := uint64(1); blockNum <= 5; blockNum++ {
			publishCompleteL2Block(t, streamServer, blockNum, 1, 3) // 3 transactions per block
		}
	})

	// Step 3: Create and start the client
	client := createTestClient(t, ctx, manager.url, manager)
	defer client.Stop()

	t.Log("Server has published 5 blocks with bookmarks")

	// Test retrieving existing blocks - should be fast due to bookmarks
	for i := uint64(1); i <= 5; i++ {
		start := time.Now()
		block, err := client.GetL2BlockByNumber(i)
		elapsed := time.Since(start)

		assert.NoError(t, err)
		assert.NotNil(t, block)
		assert.Equal(t, i, block.L2BlockNumber)
		assert.GreaterOrEqual(t, len(block.L2Txs), 3, "Block should have at least 3 transactions")
		assert.Less(t, elapsed, 100*time.Millisecond, "Block lookup should be fast with bookmarks")

		t.Logf("Retrieved block %d in %v with %d transactions", i, elapsed, len(block.L2Txs))
	}

	// Test non-existent block - should fail quickly
	start := time.Now()
	_, err := client.GetL2BlockByNumber(10)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Less(t, elapsed, 50*time.Millisecond, "Non-existent block should fail quickly")

	t.Logf("Non-existent block lookup failed correctly in %v", elapsed)
}
