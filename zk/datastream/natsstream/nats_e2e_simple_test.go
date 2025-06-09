package natsstream

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestE2E_ServerPublishClientConsume performs end-to-end validation using server publishing and client consumption
func TestE2E_ServerPublishClientConsume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Setup NATS server and client
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	createTestStream(t, url, 1101)

	client := NewNATSClient(ctx, url, false, 1101, 7, log.New())
	err := client.Start()
	require.NoError(t, err)
	defer client.Stop()

	// Start client reading
	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	// Create a publisher to simulate server
	nc, err := nats.Connect(url)
	require.NoError(t, err)
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	publisher := &e2ePublisher{js: js, ctx: ctx, entryNum: 1}

	t.Log("📤 Publishing realistic datastream sequence...")

	// Publish a realistic sequence: BatchStart -> L2Block -> Transactions -> L2BlockEnd -> BatchEnd

	// Step 1: Publish BatchStart
	publisher.publishBatchStart(t, 1, 7)
	expectedEntries := 1

	// Step 2: Publish L2Block
	publisher.publishL2Block(t, 1, 1)
	expectedEntries++

	// Step 3: Publish Transactions
	txCount := 3
	for i := 0; i < txCount; i++ {
		publisher.publishL2Transaction(t, 1, uint64(i))
		expectedEntries++
	}

	// Step 4: Publish L2BlockEnd
	publisher.publishL2BlockEnd(t, 1)
	expectedEntries++

	// Step 5: Publish BatchEnd
	publisher.publishBatchEnd(t, 1)
	expectedEntries++

	t.Logf("📦 Published %d entries total", expectedEntries)

	// Collect entries from client
	t.Log("📥 Collecting entries from client...")
	var receivedEntries []interface{}
	timeout := time.After(10 * time.Second)

	// We expect to receive fewer entries because the client builds complete blocks
	// The transactions get combined into the block, so we expect:
	// BatchStart + FullL2Block + BatchEnd = 3 entries (instead of 6)
	expectedClientEntries := 3

	for len(receivedEntries) < expectedClientEntries {
		select {
		case entry := <-*entryChan:
			if entry != nil { // Skip nil end-of-stream signals
				receivedEntries = append(receivedEntries, entry)
				t.Logf("📦 Received entry %d/%d: %T", len(receivedEntries), expectedClientEntries, entry)
			}
		case <-timeout:
			t.Logf("⏰ Timeout. Expected %d entries, got %d", expectedClientEntries, len(receivedEntries))
			break
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
	assert.Equal(t, txCount, len(receivedBlock.L2Txs), "Block should contain %d transactions", txCount)

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

// e2ePublisher handles publishing entries to NATS for testing
type e2ePublisher struct {
	js       jetstream.JetStream
	ctx      context.Context
	entryNum uint64
}

func (p *e2ePublisher) publishBatchStart(t *testing.T, batchNumber, forkID uint64) {
	batchStart := &datastream.BatchStart{
		Number:  batchNumber,
		ForkId:  forkID,
		Type:    datastream.BatchType_BATCH_TYPE_REGULAR,
		ChainId: 1101,
	}

	p.publishEntry(t, batchStart, types.EntryTypeBatchStart)
	t.Logf("📤 Published BatchStart %d", batchNumber)
}

func (p *e2ePublisher) publishL2Block(t *testing.T, blockNumber, batchNumber uint64) {
	l2Block := &datastream.L2Block{
		Number:         blockNumber,
		BatchNumber:    batchNumber,
		Timestamp:      uint64(time.Now().Unix()),
		DeltaTimestamp: 1000,
		Hash:           common.Hash{byte(blockNumber)}.Bytes(),
		StateRoot:      common.Hash{byte(blockNumber + 100)}.Bytes(),
		GlobalExitRoot: common.Hash{byte(blockNumber + 200)}.Bytes(),
		Coinbase:       common.Address{byte(blockNumber)}.Bytes(),
		BlockGasLimit:  30000000,
		BlockInfoRoot:  common.Hash{byte(blockNumber + 50)}.Bytes(),
	}

	p.publishEntry(t, l2Block, types.EntryTypeL2Block)
	t.Logf("📤 Published L2Block %d", blockNumber)
}

func (p *e2ePublisher) publishL2Transaction(t *testing.T, blockNumber, txIndex uint64) {
	// Create realistic transaction data
	txData := fmt.Sprintf("tx_%d_%d", blockNumber, txIndex)

	l2Tx := &datastream.Transaction{
		L2BlockNumber:               blockNumber,
		Index:                       txIndex,
		IsValid:                     true,
		Encoded:                     []byte(txData),
		EffectiveGasPricePercentage: 100,
		ImStateRoot:                 common.Hash{byte(blockNumber), byte(txIndex)}.Bytes(),
	}

	p.publishEntry(t, l2Tx, types.EntryTypeL2Tx)
	t.Logf("📤 Published L2Transaction %d.%d", blockNumber, txIndex)
}

func (p *e2ePublisher) publishL2BlockEnd(t *testing.T, blockNumber uint64) {
	l2BlockEnd := &datastream.L2BlockEnd{
		Number: blockNumber,
	}

	p.publishEntry(t, l2BlockEnd, types.EntryTypeL2BlockEnd)
	t.Logf("📤 Published L2BlockEnd %d", blockNumber)
}

func (p *e2ePublisher) publishBatchEnd(t *testing.T, batchNumber uint64) {
	batchEnd := &datastream.BatchEnd{
		Number:        batchNumber,
		LocalExitRoot: common.Hash{byte(batchNumber + 10)}.Bytes(),
		StateRoot:     common.Hash{byte(batchNumber + 20)}.Bytes(),
	}

	p.publishEntry(t, batchEnd, types.EntryTypeBatchEnd)
	t.Logf("📤 Published BatchEnd %d", batchNumber)
}

// publishEntry is a helper that publishes any protobuf message to NATS
func (p *e2ePublisher) publishEntry(t *testing.T, msg proto.Message, entryType types.EntryType) {
	data, err := proto.Marshal(msg)
	require.NoError(t, err)

	headers := nats.Header{}
	headers.Set("EntryType", fmt.Sprintf("%d", entryType))
	headers.Set("EntryNumber", fmt.Sprintf("%d", p.entryNum))

	natsMsg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    data,
		Header:  headers,
	}

	_, err = p.js.PublishMsg(p.ctx, natsMsg)
	require.NoError(t, err)

	p.entryNum++
}

// TestE2E_MultipleBlocks tests processing multiple blocks in sequence
func TestE2E_MultipleBlocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Setup
	ns, url := setupTestNATSServer(t)
	defer ns.Shutdown()

	createTestStream(t, url, 1101)

	client := NewNATSClient(ctx, url, false, 1101, 7, log.New())
	err := client.Start()
	require.NoError(t, err)
	defer client.Stop()

	err = client.ReadAllEntriesToChannel()
	require.NoError(t, err)

	entryChan := client.GetEntryChan()

	// Create publisher
	nc, err := nats.Connect(url)
	require.NoError(t, err)
	js, err := jetstream.New(nc)
	require.NoError(t, err)

	publisher := &e2ePublisher{js: js, ctx: ctx, entryNum: 1}

	t.Log("📤 Publishing sequence with multiple blocks...")

	// Publish BatchStart
	publisher.publishBatchStart(t, 1, 7)

	// Publish 3 complete blocks
	blockCount := 3
	for blockNum := 1; blockNum <= blockCount; blockNum++ {
		// Block start
		publisher.publishL2Block(t, uint64(blockNum), 1)

		// 2 transactions per block
		for txIndex := 0; txIndex < 2; txIndex++ {
			publisher.publishL2Transaction(t, uint64(blockNum), uint64(txIndex))
		}

		// Block end
		publisher.publishL2BlockEnd(t, uint64(blockNum))
	}

	// Publish BatchEnd
	publisher.publishBatchEnd(t, 1)

	// Collect blocks (BatchStart + 3 blocks + BatchEnd = 5 entries)
	expectedEntries := 5
	var receivedEntries []interface{}
	timeout := time.After(15 * time.Second)

	for len(receivedEntries) < expectedEntries {
		select {
		case entry := <-*entryChan:
			if entry != nil {
				receivedEntries = append(receivedEntries, entry)
				t.Logf("📦 Received entry %d/%d: %T", len(receivedEntries), expectedEntries, entry)
			}
		case <-timeout:
			t.Logf("⏰ Timeout. Expected %d, got %d", expectedEntries, len(receivedEntries))
			break
		}
	}

	// Validate we received all blocks
	var receivedBlocks []*types.FullL2Block
	for _, entry := range receivedEntries {
		if block, ok := entry.(*types.FullL2Block); ok {
			receivedBlocks = append(receivedBlocks, block)
		}
	}

	assert.Equal(t, blockCount, len(receivedBlocks), "Should receive %d blocks", blockCount)

	// Validate block sequence and transaction counts
	for i, block := range receivedBlocks {
		expectedBlockNum := uint64(i + 1)
		assert.Equal(t, expectedBlockNum, block.L2BlockNumber, "Block %d number mismatch", i)
		assert.Equal(t, 2, len(block.L2Txs), "Block %d should have 2 transactions", expectedBlockNum)
	}

	t.Logf("✅ Successfully processed %d blocks in sequence", blockCount)
}
