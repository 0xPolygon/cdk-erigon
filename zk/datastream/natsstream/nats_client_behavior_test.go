package natsstream

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNATSClientValidBlockSequence validates the state machine can process complete block sequences
func TestNATSClientValidBlockSequence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Hour)
	defer cancel()

	// server creation + population + client creation
	client := populateTestServerWithDefaultDataReturnClient(t, ctx)

	// Launch processing in background so we can read from channel concurrently
	errChan := make(chan error, 1)
	go func() {
		err := client.ReadAllEntriesToChannel()
		if err != nil {
			errChan <- err
		}
	}()

	entryChan := client.GetEntryChan()

	// Test validates that the state machine can process the complete block sequence from default data
	// The default data already contains a complete batch with block 1, so we verify that block
	// We may receive multiple entries (bookmarks, batch start, etc.) before getting the block
	var receivedBlock *types.FullL2Block
	for receivedBlock == nil {
		select {
		case entry := <-*entryChan:
			t.Logf("Received entry of type: %T", entry)
			if block, ok := entry.(*types.FullL2Block); ok {
				receivedBlock = block
				t.Logf("Successfully received complete block %d with %d transactions", block.L2BlockNumber, len(block.L2Txs))
			} else {
				// Log other entry types we're receiving
				switch e := entry.(type) {
				case *types.BatchStart:
					t.Logf("Received BatchStart with batchNum=%d", e.Number)
				case *types.BookmarkProto:
					t.Logf("Received Bookmark of type=%d, value=%d", e.BookmarkType(), e.BookMark.GetValue())
				default:
					t.Logf("Received other entry type: %T", entry)
				}
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

// TestNATSClientNewBlockWithoutEndingPrevious validates error handling when a new block starts without ending the previous one
func TestNATSClientNewBlockWithoutEndingPrevious(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := populateTestServerWithCustomDataReturnClient(
		t,
		ctx,
		func(t *testing.T, streamServer *NATSStreamServer) {
			// First publish a complete block to initialize bookmark store properly
			publishCompleteL2Block(t, streamServer, 1, 1, 1)

			// Then publish incomplete block sequence that should trigger error
			// Publish L2Block without ending it, then another L2Block
			// This should trigger the TCP client behavior: error on incomplete block
			publishL2BlockWithStreamServer(t, streamServer, 2, 1)
			publishL2TransactionWithStreamServer(t, streamServer, 2, 0)
			// Missing L2BlockEnd for block 2
			publishL2BlockWithStreamServer(t, streamServer, 3, 1) // This should cause error
		})

	// Launch processing in background
	errChan := make(chan error, 1)
	go func() {
		err := client.ReadAllEntriesToChannel()
		errChan <- err
	}()

	// We expect a fatal error for starting a new block without ending the previous one
	select {
	case err := <-errChan:
		require.Error(t, err, "Expected fatal error for incomplete block")
		assert.Contains(t, err.Error(), "received new L2 block", "Should get error about new block without proper end")
		t.Logf("Correctly caught fatal error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for fatal error")
	}
}

// TestNATSClientTransactionOutsideBlock validates that transactions outside blocks cause errors
func TestNATSClientTransactionOutsideBlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := populateTestServerWithCustomDataReturnClient(
		t,
		ctx,
		func(t *testing.T, streamServer *NATSStreamServer) {
			// Publish complete block first (this should be processed)
			publishCompleteL2Block(t, streamServer, 1, 1, 1)
			// Publish transaction outside of block (should cause error)
			publishL2TransactionWithStreamServer(t, streamServer, 1, 0)
		})

	// Start reading entries - should return the fatal error synchronously
	err := client.ReadAllEntriesToChannel()

	// We expect this to return the fatal error immediately (synchronous processing like TCP client)
	require.Error(t, err, "Expected fatal error from ReadAllEntriesToChannel")
	assert.Equal(t, "unexpected L2 tx entry, found outside of block", err.Error())
	t.Logf("Correctly caught fatal error: %v", err)
}

// TestBookmarkBasedResumption validates bookmark-based random access with realistic batch structure
func TestBookmarkBasedResumption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // Longer timeout for more data
	defer cancel()

	// Track expected transaction counts for verification
	allTxCounts := make(map[uint64]int)

	client := populateTestServerWithCustomDataReturnClient(
		t,
		ctx,
		func(t *testing.T, streamServer *NATSStreamServer) {
			// Publish Batch 1 (Blocks 1, 2, 3)
			txCounts1 := publishRealisticBatch(t, streamServer, 1, 1, 3) // batchNum=1, startBlock=1, blockCount=3
			for k, v := range txCounts1 {
				allTxCounts[k] = v
			}

			// Publish Batch 2 (Blocks 4, 5, 6)
			txCounts2 := publishRealisticBatch(t, streamServer, 2, 4, 3) // batchNum=2, startBlock=4, blockCount=3
			for k, v := range txCounts2 {
				allTxCounts[k] = v
			}
		})

	// Launch processing in background so we can read from channel concurrently
	// This test needs to read from the channel during processing, so we use the async pattern
	errChan := make(chan error, 1)
	go func() {
		err := client.ReadAllEntriesToChannel()
		errChan <- err
	}()

	entryChan := client.GetEntryChan()
	processedBlocks := 0
	receivedBlocks := make(map[uint64]*types.FullL2Block)

	// Process for up to 30 seconds or until we get all 6 blocks
	for processedBlocks < 6 {
		select {
		case entry := <-*entryChan:
			if block, isBlock := entry.(*types.FullL2Block); isBlock {
				processedBlocks++
				receivedBlocks[block.L2BlockNumber] = block

				// Verify block has expected number of transactions
				expectedTxCount := allTxCounts[block.L2BlockNumber]
				assert.Equal(t, expectedTxCount, len(block.L2Txs),
					"Block %d should have %d transactions, got %d",
					block.L2BlockNumber, expectedTxCount, len(block.L2Txs))

				t.Logf("Processed block %d with %d transactions", block.L2BlockNumber, len(block.L2Txs))
			}
			// Ignore other entry types (bookmarks, batch start/end)
		case err := <-errChan:
			require.NoError(t, err, "ReadAllEntriesToChannel should not error for valid data")
		case <-time.After(10 * time.Second):
			t.Fatalf("timeout waiting for blocks, processed: %d/6", processedBlocks)
		}
	}

	// Test bookmark-based random access to various blocks
	testCases := []struct {
		blockNum    uint64
		description string
	}{
		{2, "middle of first batch"},
		{4, "first block of second batch"},
		{6, "last block"},
		{1, "first block"},
		{5, "middle of second batch"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("GetL2BlockByNumber_%d_%s", tc.blockNum, tc.description), func(t *testing.T) {
			block, err := client.GetL2BlockByNumber(tc.blockNum)
			require.NoError(t, err, "Should retrieve block %d via bookmark", tc.blockNum)

			assert.Equal(t, tc.blockNum, block.L2BlockNumber)
			expectedTxCount := allTxCounts[tc.blockNum]
			assert.Len(t, block.L2Txs, expectedTxCount,
				"Block %d should have %d transactions", tc.blockNum, expectedTxCount)

			t.Logf("Successfully retrieved block %d with %d transactions via bookmark",
				tc.blockNum, len(block.L2Txs))
		})
	}

	// Test error case
	t.Run("non_existent_block", func(t *testing.T) {
		_, err := client.GetL2BlockByNumber(999)
		assert.Error(t, err, "Should error for non-existent block")
	})
}
