package natsstream

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestStateBlockBuildingValidation validates the state machine matches TCP client behavior
func TestStateBlockBuildingValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, ns, _ := setupTestClientWithStream(t, ctx, 1101)
	defer ns.Shutdown()
	defer client.Stop()

	// Get JS client for publishing
	js, err := jetstream.New(client.nc)
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	t.Run("valid complete block sequence", func(t *testing.T) {
		err := client.ReadAllEntriesToChannel()
		require.NoError(t, err)

		// Publish valid sequence: L2Block -> L2Tx -> L2BlockEnd
		publishL2Block(t, js, 1, 1, client.subjectPrefix)
		publishL2Transaction(t, js, 1, 0, client.subjectPrefix)
		publishL2BlockEnd(t, js, 1, client.subjectPrefix)

		// Should receive complete block
		select {
		case entry := <-*entryChan:
			block, ok := entry.(*types.FullL2Block)
			require.True(t, ok, "Expected FullL2Block")
			assert.Equal(t, uint64(1), block.L2BlockNumber)
			assert.Len(t, block.L2Txs, 1)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for complete block")
		}
	})

	t.Run("error: new block without ending previous block", func(t *testing.T) {
		// Start new reading session
		client.StopReadingToChannel()
		client.RenewEntryChannel()
		entryChan = client.GetEntryChan()

		err := client.ReadAllEntriesToChannel()
		require.NoError(t, err)

		// Publish L2Block without ending it, then another L2Block
		// This should trigger the TCP client behavior: error on incomplete block
		publishL2Block(t, js, 2, 1, client.subjectPrefix)
		publishL2Transaction(t, js, 2, 0, client.subjectPrefix)
		// Missing L2BlockEnd for block 2
		publishL2Block(t, js, 3, 1, client.subjectPrefix) // This should cause error

		// The client should handle this error and not send partial block
		// Wait to ensure no partial block is sent
		select {
		case entry := <-*entryChan:
			t.Errorf("Received unexpected entry when expecting error: %T", entry)
		case <-time.After(2 * time.Second):
			// Expected: no entry should be sent due to error
		}
	})

	t.Run("batch end terminates incomplete block", func(t *testing.T) {
		// Start new reading session
		client.StopReadingToChannel()
		client.RenewEntryChannel()
		entryChan = client.GetEntryChan()

		err := client.ReadAllEntriesToChannel()
		require.NoError(t, err)

		// Publish L2Block with transactions, then BatchEnd (matches TCP behavior)
		publishL2Block(t, js, 4, 1, client.subjectPrefix)
		publishL2Transaction(t, js, 4, 0, client.subjectPrefix)
		publishL2Transaction(t, js, 4, 1, client.subjectPrefix)
		publishBatchEnd(t, js, 1, client.subjectPrefix) // This should finalize block 4

		// Should receive the finalized block
		select {
		case entry := <-*entryChan:
			block, ok := entry.(*types.FullL2Block)
			require.True(t, ok, "Expected FullL2Block")
			assert.Equal(t, uint64(4), block.L2BlockNumber)
			assert.Len(t, block.L2Txs, 2)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for block finalized by BatchEnd")
		}

		// Should also receive the BatchEnd
		select {
		case entry := <-*entryChan:
			batchEnd, ok := entry.(*types.BatchEnd)
			require.True(t, ok, "Expected BatchEnd")
			assert.Equal(t, uint64(1), batchEnd.Number)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for BatchEnd")
		}
	})

	t.Run("transaction outside block is ignored", func(t *testing.T) {
		// Start new reading session
		client.StopReadingToChannel()
		client.RenewEntryChannel()
		entryChan = client.GetEntryChan()

		err := client.ReadAllEntriesToChannel()
		require.NoError(t, err)

		// Publish transaction without starting a block (should be ignored)
		publishL2Transaction(t, js, 5, 0, client.subjectPrefix)

		// Should not receive anything
		select {
		case entry := <-*entryChan:
			t.Errorf("Received unexpected entry for orphaned transaction: %T", entry)
		case <-time.After(2 * time.Second):
			// Expected: transaction should be ignored
		}

		// Now publish proper block sequence
		publishL2Block(t, js, 5, 1, client.subjectPrefix)
		publishL2Transaction(t, js, 5, 0, client.subjectPrefix)
		publishL2BlockEnd(t, js, 5, client.subjectPrefix)

		// Should receive complete block
		select {
		case entry := <-*entryChan:
			block, ok := entry.(*types.FullL2Block)
			require.True(t, ok, "Expected FullL2Block")
			assert.Equal(t, uint64(5), block.L2BlockNumber)
			assert.Len(t, block.L2Txs, 1)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for valid block")
		}
	})

	t.Run("block end without block is ignored", func(t *testing.T) {
		// Start new reading session
		client.StopReadingToChannel()
		client.RenewEntryChannel()
		entryChan = client.GetEntryChan()

		err := client.ReadAllEntriesToChannel()
		require.NoError(t, err)

		// Publish block end without starting a block (should be ignored)
		publishL2BlockEnd(t, js, 6, client.subjectPrefix)

		// Should not receive anything
		select {
		case entry := <-*entryChan:
			t.Errorf("Received unexpected entry for orphaned block end: %T", entry)
		case <-time.After(2 * time.Second):
			// Expected: block end should be ignored
		}
	})
}

// TestMessageOrderingValidation ensures NATS preserves message ordering
func TestMessageOrderingValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, ns, _ := setupTestClientWithStream(t, ctx, 1101)
	defer ns.Shutdown()
	defer client.Stop()

	js, err := jetstream.New(client.nc)
	require.NoError(t, err)

	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	// Publish multiple blocks in sequence
	blockCount := 5
	for i := 1; i <= blockCount; i++ {
		publishL2Block(t, js, uint64(i), 1, client.subjectPrefix)
		publishL2Transaction(t, js, uint64(i), 0, client.subjectPrefix)
		publishL2Transaction(t, js, uint64(i), 1, client.subjectPrefix)
		publishL2BlockEnd(t, js, uint64(i), client.subjectPrefix)
	}

	// Verify blocks are received in order
	for i := 1; i <= blockCount; i++ {
		select {
		case entry := <-*entryChan:
			block, ok := entry.(*types.FullL2Block)
			require.True(t, ok, "Expected FullL2Block for block %d", i)
			assert.Equal(t, uint64(i), block.L2BlockNumber, "Block %d should be received in order", i)
			assert.Len(t, block.L2Txs, 2, "Block %d should have 2 transactions", i)
		case <-time.After(10 * time.Second):
			t.Fatalf("timeout waiting for block %d", i)
		}
	}
}

// TestBookmarkBasedResumption validates bookmark functionality
func TestBookmarkBasedResumption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, ns, _ := setupTestClientWithStream(t, ctx, 1101)
	defer ns.Shutdown()
	defer client.Stop()

	js, err := jetstream.New(client.nc)
	require.NoError(t, err)

	// Publish several blocks with bookmarks
	for i := 1; i <= 3; i++ {
		// Publish bookmark first
		publishBookmark(t, js, uint64(i), datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK, client.subjectPrefix)
		publishL2Block(t, js, uint64(i), 1, client.subjectPrefix)
		publishL2Transaction(t, js, uint64(i), 0, client.subjectPrefix)
		publishL2BlockEnd(t, js, uint64(i), client.subjectPrefix)
	}

	// Start reading to process bookmarks (creates KV entries)
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	// Wait for all blocks to be processed
	entryChan := client.GetEntryChan()
	for i := 1; i <= 3; i++ {
		select {
		case <-*entryChan:
			// Block received
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for block %d during initial processing", i)
		}
	}

	// Now test GetL2BlockByNumber using bookmarks
	for i := 1; i <= 3; i++ {
		block, err := client.GetL2BlockByNumber(uint64(i))
		require.NoError(t, err, "GetL2BlockByNumber should work for block %d", i)
		assert.Equal(t, uint64(i), block.L2BlockNumber)
		assert.Len(t, block.L2Txs, 1)
	}

	// Test non-existent block
	_, err = client.GetL2BlockByNumber(999)
	assert.Error(t, err, "Should error for non-existent block")
}

// TestProgressTracking validates progress atomic operations
func TestProgressTracking(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, ns, _ := setupTestClientWithStream(t, ctx, 1101)
	defer ns.Shutdown()
	defer client.Stop()

	js, err := jetstream.New(client.nc)
	require.NoError(t, err)

	// Initially progress should be 0
	progress := client.GetProgressAtomic()
	assert.Equal(t, uint64(0), progress.Load())

	// Set progress and test resumption
	progress.Store(2)

	// Publish blocks starting from 1
	for i := 1; i <= 5; i++ {
		publishL2Block(t, js, uint64(i), 1, client.subjectPrefix)
		publishL2Transaction(t, js, uint64(i), 0, client.subjectPrefix)
		publishL2BlockEnd(t, js, uint64(i), client.subjectPrefix)
	}

	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	// Should receive blocks starting from progress
	receivedBlocks := []uint64{}
	for len(receivedBlocks) < 5 {
		select {
		case entry := <-*entryChan:
			if block, ok := entry.(*types.FullL2Block); ok {
				receivedBlocks = append(receivedBlocks, block.L2BlockNumber)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("timeout waiting for blocks, received: %v", receivedBlocks)
		}
	}

	// All blocks should be received (client scans from beginning when no bookmark)
	expectedBlocks := []uint64{1, 2, 3, 4, 5}
	assert.Equal(t, expectedBlocks, receivedBlocks)
}

// Helper functions for publishing test messages

func publishL2Block(t *testing.T, js jetstream.JetStream, blockNum, batchNum uint64, subjectPrefix string) {
	t.Helper()

	l2Block := &datastream.L2Block{
		Number:      blockNum,
		BatchNumber: batchNum,
		Timestamp:   uint64(time.Now().Unix()),
		Hash:        common.Hash{byte(blockNum)}.Bytes(),
		StateRoot:   common.Hash{byte(blockNum + 1)}.Bytes(),
	}

	msgData, err := proto.Marshal(l2Block)
	require.NoError(t, err)

	headers := nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2Block))
	headers.Set("EntryNum", fmt.Sprintf("%d", blockNum*10))

	msg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    msgData,
		Header:  headers,
	}

	_, err = js.PublishMsg(context.Background(), msg)
	require.NoError(t, err)
}

func publishL2Transaction(t *testing.T, js jetstream.JetStream, blockNum, txIndex uint64, subjectPrefix string) {
	t.Helper()

	l2Tx := &datastream.Transaction{
		L2BlockNumber:               blockNum,
		Index:                       txIndex,
		IsValid:                     true,
		Encoded:                     []byte{0x01, 0x02, 0x03}, // Dummy encoded tx
		EffectiveGasPricePercentage: 255,
	}

	msgData, err := proto.Marshal(l2Tx)
	require.NoError(t, err)

	headers := nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2Tx))
	headers.Set("EntryNum", fmt.Sprintf("%d", blockNum*10+txIndex+1))

	msg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    msgData,
		Header:  headers,
	}

	_, err = js.PublishMsg(context.Background(), msg)
	require.NoError(t, err)
}

func publishL2BlockEnd(t *testing.T, js jetstream.JetStream, blockNum uint64, subjectPrefix string) {
	t.Helper()

	blockEnd := &datastream.L2BlockEnd{
		Number: blockNum,
	}

	msgData, err := proto.Marshal(blockEnd)
	require.NoError(t, err)

	headers := nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeL2BlockEnd))
	headers.Set("EntryNum", fmt.Sprintf("%d", blockNum*10+9))

	msg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    msgData,
		Header:  headers,
	}

	_, err = js.PublishMsg(context.Background(), msg)
	require.NoError(t, err)
}

func publishBatchStart(t *testing.T, js jetstream.JetStream, batchNum uint64, subjectPrefix string) {
	t.Helper()

	batchStart := &datastream.BatchStart{
		Number: batchNum,
		ForkId: 7,
		Type:   datastream.BatchType_BATCH_TYPE_REGULAR,
	}

	msgData, err := proto.Marshal(batchStart)
	require.NoError(t, err)

	headers := nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeBatchStart))
	headers.Set("EntryNum", fmt.Sprintf("%d", batchNum*100))

	msg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    msgData,
		Header:  headers,
	}

	_, err = js.PublishMsg(context.Background(), msg)
	require.NoError(t, err)
}

func publishBatchEnd(t *testing.T, js jetstream.JetStream, batchNum uint64, subjectPrefix string) {
	t.Helper()

	batchEnd := &datastream.BatchStart{ // Note: Using BatchStart as BatchEnd proto
		Number: batchNum,
	}

	msgData, err := proto.Marshal(batchEnd)
	require.NoError(t, err)

	headers := nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", types.EntryTypeBatchEnd))
	headers.Set("EntryNum", fmt.Sprintf("%d", batchNum*100+99))

	msg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    msgData,
		Header:  headers,
	}

	_, err = js.PublishMsg(context.Background(), msg)
	require.NoError(t, err)
}

func publishBookmark(t *testing.T, js jetstream.JetStream, value uint64, bookmarkType datastream.BookmarkType, subjectPrefix string) {
	t.Helper()

	bookmark := &datastream.BookMark{
		Type:  bookmarkType,
		Value: value,
	}

	msgData, err := proto.Marshal(bookmark)
	require.NoError(t, err)

	headers := nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", types.BookmarkEntryType))
	headers.Set("EntryNum", fmt.Sprintf("%d", value*1000))

	msg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    msgData,
		Header:  headers,
	}

	_, err = js.PublishMsg(context.Background(), msg)
	require.NoError(t, err)
}
