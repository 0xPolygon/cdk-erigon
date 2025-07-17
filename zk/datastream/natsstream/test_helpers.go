package natsstream

import (
	"context"
	"fmt"
	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/gateway-fm/zkevm-data-streamer/datastreamer"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// NewTestLogger creates a logger that outputs to the test console
func NewTestLogger(t *testing.T) log.Logger {
	t.Helper()

	// Create a custom handler that outputs to t.Logf
	handler := log.FuncHandler(func(r *log.Record) error {
		// Format the context values
		ctx := ""
		if len(r.Ctx) > 0 {
			for i := 0; i < len(r.Ctx); i += 2 {
				if i > 0 {
					ctx += " "
				}
				ctx += fmt.Sprintf("%s=%v", r.Ctx[i], r.Ctx[i+1])
			}
		}

		// Output to test log
		if ctx != "" {
			t.Logf("[%s] [%s] %s %s", r.Time.Format("01-02|15:04:05.000"), r.Lvl, r.Msg, ctx)
		} else {
			t.Logf("[%s] [%s] %s", r.Time.Format("01-02|15:04:05.000"), r.Lvl, r.Msg)
		}
		return nil
	})

	logger := log.New()
	logger.SetHandler(handler)
	return logger
}

// publishBatchStartEntry publishes a batch start entry
func publishBatchStartEntry(t *testing.T, streamServer *NATSStreamServer, batchNum uint64) {
	t.Helper()

	batchStart := &datastream.BatchStart{
		Number:  batchNum,
		Type:    datastream.BatchType_BATCH_TYPE_REGULAR,
		ForkId:  7,    // Standard fork ID for tests
		ChainId: 1106, // Unique chain ID for bookmark test
	}
	msgData, err := proto.Marshal(batchStart)
	require.NoError(t, err)

	_, err = streamServer.AddStreamEntry(datastreamer.EntryType(types.EntryTypeBatchStart), msgData)
	require.NoError(t, err)
}

// publishCompleteL2BlockWithTx publishes a complete L2 block within its own transaction
func publishCompleteL2BlockWithTx(t *testing.T, streamServer *NATSStreamServer, blockNum, batchNum uint64, txCount int) {
	t.Helper()

	// Start atomic operation
	err := streamServer.StartAtomicOp()
	require.NoError(t, err)

	publishCompleteL2Block(t, streamServer, blockNum, batchNum, txCount)

	// Commit the atomic operation
	err = streamServer.CommitAtomicOp()
	require.NoError(t, err)
}

// publishCompleteL2Block publishes a complete L2 block with bookmark, block, transactions, and block end
// NOTE: This function expects to be called within an active transaction
func publishCompleteL2Block(t *testing.T, streamServer *NATSStreamServer, blockNum, batchNum uint64, txCount int) {
	t.Helper()

	// L2 Block bookmark - use AddStreamBookmark to store in KV
	blockBookmark := &datastream.BookMark{
		Type:  datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK,
		Value: blockNum,
	}
	bookmarkData, err := proto.Marshal(blockBookmark)
	require.NoError(t, err)
	_, err = streamServer.AddStreamBookmark(bookmarkData)
	require.NoError(t, err)

	// L2 Block
	l2Block := &datastream.L2Block{
		Number:      blockNum,
		BatchNumber: batchNum,
		Timestamp:   uint64(time.Now().Unix()) + blockNum, // Unique timestamps
		Hash:        common.Hash{byte(blockNum)}.Bytes(),
		StateRoot:   common.Hash{byte(blockNum + 100)}.Bytes(),
	}
	blockData, err := proto.Marshal(l2Block)
	require.NoError(t, err)
	_, err = streamServer.AddStreamEntry(datastreamer.EntryType(types.EntryTypeL2Block), blockData)
	require.NoError(t, err)

	// Randomized transactions (3-12 per block)
	for txIndex := 0; txIndex < txCount; txIndex++ {
		l2Tx := &datastream.Transaction{
			L2BlockNumber:               blockNum,
			Index:                       uint64(txIndex),
			IsValid:                     true,
			Encoded:                     []byte{byte(blockNum), byte(txIndex), 0x01, 0x02}, // Unique encoded tx
			EffectiveGasPricePercentage: 255,
		}
		txData, err := proto.Marshal(l2Tx)
		require.NoError(t, err)
		_, err = streamServer.AddStreamEntry(datastreamer.EntryType(types.EntryTypeL2Tx), txData)
		require.NoError(t, err)
	}

	// L2 Block end
	blockEnd := &datastream.L2BlockEnd{
		Number: blockNum,
	}
	blockEndData, err := proto.Marshal(blockEnd)
	require.NoError(t, err)
	_, err = streamServer.AddStreamEntry(datastreamer.EntryType(types.EntryTypeL2BlockEnd), blockEndData)
	require.NoError(t, err)
}

// publishProperBatchEndEntry publishes a proper BatchEnd
func publishProperBatchEndEntry(t *testing.T, streamServer *NATSStreamServer, batchNum uint64) {
	t.Helper()

	batchEnd := &datastream.BatchEnd{
		Number:        batchNum,
		LocalExitRoot: common.Hash{byte(batchNum + 10)}.Bytes(), // Test local exit root
		StateRoot:     common.Hash{byte(batchNum + 20)}.Bytes(), // Test state root from last block
	}
	msgData, err := proto.Marshal(batchEnd)
	require.NoError(t, err)

	_, err = streamServer.AddStreamEntry(datastreamer.EntryType(types.EntryTypeBatchEnd), msgData)
	require.NoError(t, err)
}

// publishBatchBookmarkEntry publishes a batch bookmark
func publishBatchBookmarkEntry(t *testing.T, streamServer *NATSStreamServer, batchNum uint64) {
	t.Helper()

	bookmark := &datastream.BookMark{
		Type:  datastream.BookmarkType_BOOKMARK_TYPE_BATCH,
		Value: batchNum,
	}
	msgData, err := proto.Marshal(bookmark)
	require.NoError(t, err)

	_, err = streamServer.AddStreamBookmark(msgData)
	require.NoError(t, err)
}

func publishL2BlockWithStreamServer(t *testing.T, streamServer *NATSStreamServer, blockNum, batchNum uint64) {
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

	// Add entry to transaction
	_, err = streamServer.AddStreamEntry(datastreamer.EntryType(types.EntryTypeL2Block), msgData)
	require.NoError(t, err)
}

func publishL2TransactionWithStreamServer(t *testing.T, streamServer *NATSStreamServer, blockNum, txIndex uint64) {
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

	// Add entry to transaction
	_, err = streamServer.AddStreamEntry(datastreamer.EntryType(types.EntryTypeL2Tx), msgData)
	require.NoError(t, err)
}

func publishL2BlockEndWithStreamServer(t *testing.T, streamServer *NATSStreamServer, blockNum uint64) {
	t.Helper()

	blockEnd := &datastream.L2BlockEnd{
		Number: blockNum,
	}

	msgData, err := proto.Marshal(blockEnd)
	require.NoError(t, err)

	// Add entry to transaction
	_, err = streamServer.AddStreamEntry(datastreamer.EntryType(types.EntryTypeL2BlockEnd), msgData)
	require.NoError(t, err)
}

// populateTestServerWithFixedDataReturnClient creates server, populates with fixed deterministic data, and returns client
func populateTestServerWithFixedDataReturnClient(t *testing.T, ctx context.Context) *NATSClient {
	t.Helper()

	return populateTestServerWithCustomDataReturnClient(t, ctx, func(t *testing.T, streamServer *NATSStreamServer) {
		// Fixed data population: publish a single batch with one block and exactly 8 transactions
		// This creates exactly 14 entries total:
		// 1. Batch bookmark (1 entry)
		// 2. Batch start (1 entry)
		// 3. L2 Block bookmark (1 entry)
		// 4. L2 Block (1 entry)
		// 5. L2 Transactions (8 entries - fixed)
		// 6. L2 Block end (1 entry)
		// 7. Batch end (1 entry)
		// Total: 14 entries
		publishFixedBatch(t, streamServer, 1, 1, 1, 8) // batchNum=1, startBlock=1, blockCount=1, txCount=8
	})
}

// publishFixedBatch publishes a complete batch with fixed transaction count (no randomization)
// This is used for tests that need predictable entry counts
func publishFixedBatch(t *testing.T, streamServer *NATSStreamServer, batchNum, startBlockNum uint64, blockCount int, txCount int) {
	t.Helper()

	// 1. Batch bookmark
	publishBatchBookmarkEntry(t, streamServer, batchNum)

	// 2. Batch start
	publishBatchStartEntry(t, streamServer, batchNum)

	// 3. L2 Blocks with fixed transaction count
	for i := 0; i < blockCount; i++ {
		blockNum := startBlockNum + uint64(i)
		publishCompleteL2Block(t, streamServer, blockNum, batchNum, txCount)
	}

	// 4. Batch end (proper BatchEnd, not reused BatchStart)
	publishProperBatchEndEntry(t, streamServer, batchNum)
}

// populateTestServerWithCustomDataReturnClient creates a server, populates it with minimal default data via a passed in function, and returns the client
func populateTestServerWithCustomDataReturnClient(t *testing.T, ctx context.Context, populateData func(t *testing.T, streamServer *NATSStreamServer)) *NATSClient {
	t.Helper()

	// Step 1: Create server infrastructure with manager
	streamServer, manager := setupTestServerWithManager(t, ctx)

	publishInAO(t, streamServer, populateData)

	// Step 2: Create and start the client
	client := createTestClient(t, ctx, manager.url, manager)

	t.Cleanup(func() {
		client.Stop()
		manager.Stop()
	})

	return client
}

// populateTestServerWithCustomDataReturnClient creates server, populates with minimal default data, and returns client
// This ensures the bookmark KV store exists before client connects, following the TestBookmarkBasedResumption pattern
func populateTestServerWithDefaultDataReturnClient(t *testing.T, ctx context.Context) *NATSClient {
	t.Helper()

	return populateTestServerWithCustomDataReturnClient(t, ctx, func(t *testing.T, streamServer *NATSStreamServer) {
		// Default data population: publish a single batch with one block
		publishRealisticBatch(t, streamServer, 1, 1, 1) // batchNum=1, startBlock=1, blockCount=1
	})
}

// Seed randomizer for transaction count generation
var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

// publishRealisticBatch publishes a complete batch with proper structure and randomized transactions
// Returns a map of blockNumber -> transactionCount for verification
func publishRealisticBatch(t *testing.T, streamServer *NATSStreamServer, batchNum, startBlockNum uint64, blockCount int) map[uint64]int {
	t.Helper()

	txCountPerBlock := make(map[uint64]int)

	// 1. Batch bookmark
	publishBatchBookmarkEntry(t, streamServer, batchNum)

	// 2. Batch start
	publishBatchStartEntry(t, streamServer, batchNum)

	// 3. L2 Blocks with randomized transactions
	for i := 0; i < blockCount; i++ {
		blockNum := startBlockNum + uint64(i)
		// Returns 3-12 transactions per block
		txCount := rng.Intn(10) + 3 // 3-12 transactions
		txCountPerBlock[blockNum] = txCount

		publishCompleteL2Block(t, streamServer, blockNum, batchNum, txCount)
	}

	// 4. Batch end (proper BatchEnd, not reused BatchStart)
	publishProperBatchEndEntry(t, streamServer, batchNum)

	return txCountPerBlock
}

// setupTestServerWithManager creates only the server infrastructure (manager and stream server)
func setupTestServerWithManager(t *testing.T, ctx context.Context) (*NATSStreamServer, *Manager) {
	t.Helper()

	// Create a temporary directory for NATS storage
	tempDir := t.TempDir()

	// Create Manager config with chain ID
	config := DefaultConfig()
	config.Port = -1 // Use random port
	config.ServerName = "test-server"
	config.HTTPPort = 0 // Disable HTTP monitoring for tests
	config.JetStreamEnabled = true
	config.StorageDir = tempDir

	logger := NewTestLogger(t)
	// Create and start manager (this sets up the full server infrastructure)
	manager := NewManager(config, logger)
	err := manager.Start()
	require.NoError(t, err)

	// Initialize streams and bookmarks
	err = manager.InitStreams(ctx)
	require.NoError(t, err)

	// Create NATSStreamServer for proper transaction support
	streamServer := &NATSStreamServer{
		delegate:    &MockStreamServer{}, // Use mock delegate for testing
		natsManager: manager,
		logger:      logger,
		txActive:    false,
		txMsgs:      nil,
		nextEntry:   1, // Start from entry 1
	}

	// Initialize the metadata manager
	metadata, err := NewMetadataManager(ctx, manager, logger)
	require.NoError(t, err)
	streamServer.metadata = metadata

	// Start the stream server to initialize bookmark KV store
	err = streamServer.Start()
	require.NoError(t, err)

	return streamServer, manager
}

// createTestClient creates and starts a NATS client connecting to an existing server
func createTestClient(t *testing.T, ctx context.Context, url string, manager *Manager) *NATSClient {
	t.Helper()

	// Create client with proper manager and test logger
	client := NewNATSClient(ctx, url, false, manager, NewTestLogger(t))
	err := client.Start()
	require.NoError(t, err)

	return client
}

func publishInAO(t *testing.T, streamServer *NATSStreamServer, populate func(t *testing.T, streamServer *NATSStreamServer)) {
	t.Helper()

	// Start atomic operation
	err := streamServer.StartAtomicOp()
	require.NoError(t, err)

	populate(t, streamServer)

	// Commit the atomic operation
	err = streamServer.CommitAtomicOp()
	require.NoError(t, err)

}

// MockStreamServer provides a minimal implementation of server.StreamServer for testing
type MockStreamServer struct {
	entryCounter uint64
	mutex        sync.Mutex
}

func (m *MockStreamServer) Start() error         { return nil }
func (m *MockStreamServer) StartAtomicOp() error { return nil }
func (m *MockStreamServer) AddStreamEntry(_ datastreamer.EntryType, _ []byte) (uint64, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.entryCounter++
	return m.entryCounter - 1, nil
}
func (m *MockStreamServer) AddStreamBookmark(_ []byte) (uint64, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.entryCounter++
	return m.entryCounter - 1, nil
}
func (m *MockStreamServer) CommitAtomicOp() error       { return nil }
func (m *MockStreamServer) RollbackAtomicOp() error     { return nil }
func (m *MockStreamServer) TruncateFile(_ uint64) error { return nil }
func (m *MockStreamServer) UpdateEntryData(_ uint64, _ datastreamer.EntryType, _ []byte) error {
	return nil
}
func (m *MockStreamServer) GetHeader() datastreamer.HeaderEntry {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return datastreamer.HeaderEntry{
		TotalEntries: m.entryCounter,
	}
}
func (m *MockStreamServer) GetEntry(_ uint64) (datastreamer.FileEntry, error) {
	return datastreamer.FileEntry{}, nil
}
func (m *MockStreamServer) GetBookmark(_ []byte) (uint64, error) { return 0, nil }
func (m *MockStreamServer) GetFirstEventAfterBookmark(_ []byte) (datastreamer.FileEntry, error) {
	return datastreamer.FileEntry{}, nil
}
func (m *MockStreamServer) GetDataBetweenBookmarks(_, _ []byte) ([]byte, error) { return nil, nil }

func (m *MockStreamServer) BookmarkPrintDump() {}
