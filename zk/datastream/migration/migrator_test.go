package migration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/natsstream"
	"github.com/erigontech/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/gateway-fm/zkevm-data-streamer/datastreamer"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestNewMigrator(t *testing.T) {
	logger := log.New()
	natsManager := createTestNATSManager(t)
	defer natsManager.Stop()

	migrator := NewMigrator("test.dat", natsManager, logger, 100, false)

	assert.NotNil(t, migrator)
	assert.Equal(t, "test.dat", migrator.tcpStreamFile)
	assert.Equal(t, 100, migrator.batchSize)
	assert.False(t, migrator.dryRun)
}

func TestOpenTCPDatastream(t *testing.T) {
	tempDir := t.TempDir()

	tcpFile := filepath.Join(tempDir, "test-datastream.dat")
	streamServer := createTestTCPDatastreamServer(t, tcpFile, 10)

	header := streamServer.GetHeader()
	assert.Equal(t, uint64(12), header.TotalEntries) // 10 L2Block + 2 Bookmarks
}

func TestOpenTCPDatastream_InvalidFile(t *testing.T) {
	logger := log.New()
	natsManager := createTestNATSManager(t)
	defer natsManager.Stop()

	migrator := NewMigrator("/nonexistent/path/file.dat", natsManager, logger, 100, false)

	streamServer, err := migrator.openTCPDatastream()
	if err != nil {
		assert.Contains(t, err.Error(), "failed to create TCP datastream server")
	} else {
		assert.Nil(t, streamServer)
	}
}

func TestInitializeNATSMetadata(t *testing.T) {
	ctx := context.Background()
	logger := log.New()

	natsManager := createTestNATSManager(t)
	defer natsManager.Stop()

	err := natsManager.Start()
	require.NoError(t, err)

	err = natsManager.InitStreams(ctx)
	require.NoError(t, err)

	migrator := NewMigrator("test.dat", natsManager, logger, 100, false)

	err = migrator.initializeNATSMetadata(ctx)
	require.NoError(t, err)
	assert.NotNil(t, migrator.metadata)

	totalEntries, err := migrator.metadata.GetTotalEntries(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), totalEntries)
}

func TestInitializeNATSMetadata_DryRun(t *testing.T) {
	ctx := context.Background()
	logger := log.New()

	natsManager := createTestNATSManager(t)
	defer natsManager.Stop()

	migrator := NewMigrator("test.dat", natsManager, logger, 100, true)

	err := migrator.initializeNATSMetadata(ctx)
	require.NoError(t, err)
	assert.Nil(t, migrator.metadata)
}

func TestCreateNATSMessage(t *testing.T) {
	logger := log.New()
	natsManager := createTestNATSManager(t)
	defer natsManager.Stop()

	migrator := NewMigrator("test.dat", natsManager, logger, 100, false)

	tests := []struct {
		name           string
		entryType      types.EntryType
		data           []byte
		expectBookmark bool
		expectBlockEnd bool
	}{
		{
			name:           "L2Block entry",
			entryType:      types.EntryTypeL2Block,
			data:           []byte("block data"),
			expectBookmark: false,
			expectBlockEnd: false,
		},
		{
			name:           "Bookmark entry",
			entryType:      types.BookmarkEntryType,
			data:           []byte("bookmark data"),
			expectBookmark: true,
			expectBlockEnd: false,
		},
		{
			name:           "L2BlockEnd entry",
			entryType:      types.EntryTypeL2BlockEnd,
			data:           []byte("block end data"),
			expectBookmark: false,
			expectBlockEnd: true,
		},
		{
			name:           "Batch start entry",
			entryType:      types.EntryTypeBatchStart,
			data:           []byte("batch start"),
			expectBookmark: false,
			expectBlockEnd: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &types.FileEntry{
				EntryType: tt.entryType,
				Data:      tt.data,
				EntryNum:  42,
			}

			msg, isBookmark, isBlockEnd, err := migrator.createNATSMessage(entry, 42)

			require.NoError(t, err)
			assert.NotNil(t, msg)
			assert.Equal(t, "datastream.entry", msg.Subject)
			assert.Equal(t, tt.data, msg.Data)
			assert.Equal(t, tt.expectBookmark, isBookmark)
			assert.Equal(t, tt.expectBlockEnd, isBlockEnd)

			entryTypeHeader := msg.Header.Get("EntryType")
			assert.NotEmpty(t, entryTypeHeader)

			entryNumHeader := msg.Header.Get("EntryNum")
			assert.Equal(t, "42", entryNumHeader)
		})
	}
}

func TestTCPIterator(t *testing.T) {
	tempDir := t.TempDir()

	numBlocks := 5
	tcpFile := createTestTCPDatastream(t, tempDir, numBlocks)

	streamServer, err := datastreamer.NewServer(
		0, 3, 4334, datastreamer.StreamType(1),
		tcpFile, 10*time.Second, 60*time.Second, 30*time.Second, nil,
	)
	require.NoError(t, err)

	header := streamServer.GetHeader()

	iterator := &tcpIterator{
		stream:       streamServer,
		curEntryNum:  0,
		totalEntries: header.TotalEntries,
	}

	assert.Equal(t, header.TotalEntries, iterator.GetEntryNumberLimit())

	entriesRead := 0
	for {
		entry, err := iterator.NextFileEntry()
		require.NoError(t, err)

		if entry == nil {
			break
		}

		entriesRead++
		assert.NotNil(t, entry.Data)
	}

	expectedTotal := calculateExpectedEntries(numBlocks)
	assert.Equal(t, expectedTotal, entriesRead)

	entryBeyondEnd, err := iterator.NextFileEntry()
	require.NoError(t, err)
	assert.Nil(t, entryBeyondEnd)
}

func TestMigrate_DryRun(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	logger := log.New()

	numBlocks := 10
	tcpFile := createTestTCPDatastream(t, tempDir, numBlocks)

	natsManager := createTestNATSManager(t)
	defer natsManager.Stop()

	migrator := NewMigrator(tcpFile, natsManager, logger, 5, true)

	stats, err := migrator.Migrate(ctx, 0)
	require.NoError(t, err)
	assert.NotNil(t, stats)

	expectedTotal := uint64(calculateExpectedEntries(numBlocks))
	assert.Equal(t, expectedTotal, stats.TotalEntries)
	assert.Equal(t, expectedTotal, stats.EntriesMigrated)
	assert.Greater(t, stats.BookmarksMigrated, uint64(0))
	assert.Empty(t, stats.Errors)
}

func TestMigrate_FullMigration(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	logger := log.New()

	numBlocks := 20
	tcpFile := createTestTCPDatastream(t, tempDir, numBlocks)

	natsManager := createTestNATSManager(t)
	defer natsManager.Stop()

	err := natsManager.Start()
	require.NoError(t, err)

	err = natsManager.InitStreams(ctx)
	require.NoError(t, err)

	migrator := NewMigrator(tcpFile, natsManager, logger, 5, false)

	stats, err := migrator.Migrate(ctx, 0)
	require.NoError(t, err)
	assert.NotNil(t, stats)

	expectedTotal := uint64(calculateExpectedEntries(numBlocks))
	assert.Equal(t, expectedTotal, stats.TotalEntries)
	assert.Equal(t, expectedTotal, stats.EntriesMigrated)
	assert.Greater(t, stats.BookmarksMigrated, uint64(0))
	assert.Empty(t, stats.Errors)

	totalEntries, err := migrator.metadata.GetTotalEntries(ctx)
	require.NoError(t, err)
	assert.Equal(t, expectedTotal, totalEntries)
}

func TestMigrate_Resumability(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	logger := log.New()

	numBlocks := 20
	tcpFile := createTestTCPDatastream(t, tempDir, numBlocks)

	natsManager := createTestNATSManager(t)
	defer natsManager.Stop()

	err := natsManager.Start()
	require.NoError(t, err)

	err = natsManager.InitStreams(ctx)
	require.NoError(t, err)

	startFrom := uint64(10)
	migrator := NewMigrator(tcpFile, natsManager, logger, 5, false)

	stats, err := migrator.Migrate(ctx, startFrom)
	require.NoError(t, err)

	expectedTotal := uint64(calculateExpectedEntries(numBlocks))
	expectedMigrated := expectedTotal - startFrom
	assert.Equal(t, expectedTotal, stats.TotalEntries)
	assert.Equal(t, expectedMigrated, stats.EntriesMigrated)
}

func TestMigrate_EmptyDatastream(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	logger := log.New()

	tcpFile := createTestTCPDatastream(t, tempDir, 0)

	natsManager := createTestNATSManager(t)
	defer natsManager.Stop()

	migrator := NewMigrator(tcpFile, natsManager, logger, 100, false)

	stats, err := migrator.Migrate(ctx, 0)
	require.NoError(t, err)

	assert.Equal(t, uint64(0), stats.TotalEntries)
	assert.Equal(t, uint64(0), stats.EntriesMigrated)
	assert.Equal(t, uint64(0), stats.BookmarksMigrated)
}

func TestMigrate_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tempDir := t.TempDir()
	logger := log.New()

	tcpFile := createTestTCPDatastream(t, tempDir, 1000)

	natsManager := createTestNATSManager(t)
	defer natsManager.Stop()

	err := natsManager.Start()
	require.NoError(t, err)

	err = natsManager.InitStreams(ctx)
	require.NoError(t, err)

	migrator := NewMigrator(tcpFile, natsManager, logger, 10, false)

	cancel()

	_, err = migrator.Migrate(ctx, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}

func TestPublishBatch_DryRun(t *testing.T) {
	ctx := context.Background()
	logger := log.New()

	natsManager := createTestNATSManager(t)
	defer natsManager.Stop()

	migrator := NewMigrator("test.dat", natsManager, logger, 100, true)

	stats := &MigrationStats{
		EntriesMigrated: 0,
	}

	batch := createTestBatch(5)
	currentBlock := uint64(0)

	js, _ := natsManager.GetOrCreateDataStream(ctx)
	err := migrator.publishBatch(ctx, js, batch, &currentBlock, stats)

	require.NoError(t, err)
	assert.Equal(t, uint64(5), stats.EntriesMigrated)
}

func TestPublishBatch_WithBookmarks(t *testing.T) {
	ctx := context.Background()
	logger := log.New()

	natsManager := createTestNATSManager(t)
	defer natsManager.Stop()

	err := natsManager.Start()
	require.NoError(t, err)

	err = natsManager.InitStreams(ctx)
	require.NoError(t, err)

	migrator := NewMigrator("test.dat", natsManager, logger, 100, false)

	err = migrator.initializeNATSMetadata(ctx)
	require.NoError(t, err)

	stats := &MigrationStats{
		EntriesMigrated: 0,
	}

	batch := createTestBatchWithBookmarks(3)
	currentBlock := uint64(0)

	js, err := natsManager.GetOrCreateDataStream(ctx)
	require.NoError(t, err)

	err = migrator.publishBatch(ctx, js, batch, &currentBlock, stats)
	require.NoError(t, err)

	assert.Equal(t, uint64(3), stats.EntriesMigrated)

	totalEntries, err := migrator.metadata.GetTotalEntries(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), totalEntries)
}

type testHelper interface {
	Helper()
	TempDir() string
	Fatal(...interface{})
	Fatalf(string, ...interface{})
}

func createTestNATSManager(t testHelper) *natsstream.Manager {
	t.Helper()

	tempDir := t.TempDir()
	logger := log.New()

	config := natsstream.Config{
		Host:             "127.0.0.1",
		Port:             -1,
		ServerName:       "test-migration",
		ClusterName:      "test-cluster",
		HTTPHost:         "127.0.0.1",
		HTTPPort:         0,
		JetStreamEnabled: true,
		StorageDir:       tempDir,
		MaxMemory:        1024 * 1024 * 1024,
		MaxStorage:       10 * 1024 * 1024 * 1024,
		Debug:            false,
		Trace:            false,
	}

	return natsstream.NewManager(config, logger)
}

func createTestTCPDatastreamServer(t testHelper, tcpFile string, numEntries int) *datastreamer.StreamServer {
	t.Helper()

	streamServer, err := datastreamer.NewServer(
		0, 3, 4334, datastreamer.StreamType(1),
		tcpFile, 10*time.Second, 60*time.Second, 30*time.Second, nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = streamServer.Start()
	if err != nil {
		t.Fatal(err)
	}

	blockNum := uint64(1)
	batchNum := uint64(1)

	for i := 0; i < numEntries; i++ {
		err = streamServer.StartAtomicOp()
		if err != nil {
			t.Fatal(err)
		}

		if i%5 == 0 {
			bookmark := createTestBookmark(blockNum, datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK)
			bookmarkData, err := bookmark.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			_, err = streamServer.AddStreamBookmark(bookmarkData)
			if err != nil {
				t.Fatal(err)
			}
		}

		blockData := createTestL2Block(blockNum, batchNum)
		blockBytes, err := blockData.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		_, err = streamServer.AddStreamEntry(datastreamer.EntryType(types.EntryTypeL2Block), blockBytes)
		if err != nil {
			t.Fatal(err)
		}

		err = streamServer.CommitAtomicOp()
		if err != nil {
			t.Fatal(err)
		}

		blockNum++
	}

	return streamServer
}

func createTestTCPDatastream(t testHelper, tempDir string, numEntries int) string {
	t.Helper()

	tcpFile := filepath.Join(tempDir, "test-datastream.dat")

	streamServer, err := datastreamer.NewServer(
		0, 3, 4334, datastreamer.StreamType(1),
		tcpFile, 10*time.Second, 60*time.Second, 30*time.Second, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer streamServer.Close()

	err = streamServer.Start()
	if err != nil {
		t.Fatal(err)
	}

	blockNum := uint64(1)
	batchNum := uint64(1)

	for i := 0; i < numEntries; i++ {
		err = streamServer.StartAtomicOp()
		if err != nil {
			t.Fatal(err)
		}

		if i%5 == 0 {
			bookmark := createTestBookmark(blockNum, datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK)
			bookmarkData, err := bookmark.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			_, err = streamServer.AddStreamBookmark(bookmarkData)
			if err != nil {
				t.Fatal(err)
			}
		}

		blockData := createTestL2Block(blockNum, batchNum)
		blockBytes, err := blockData.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		_, err = streamServer.AddStreamEntry(datastreamer.EntryType(types.EntryTypeL2Block), blockBytes)
		if err != nil {
			t.Fatal(err)
		}

		err = streamServer.CommitAtomicOp()
		if err != nil {
			t.Fatal(err)
		}

		blockNum++
	}

	return tcpFile
}

func calculateExpectedEntries(numBlocks int) int {
	bookmarks := 0
	for i := 0; i < numBlocks; i++ {
		if i%5 == 0 {
			bookmarks++
		}
	}
	return numBlocks + bookmarks
}

func createTestBookmark(value uint64, bookmarkType datastream.BookmarkType) *types.BookmarkProto {
	return &types.BookmarkProto{
		BookMark: &datastream.BookMark{
			Type:  bookmarkType,
			Value: value,
		},
	}
}

func createTestL2Block(blockNum, batchNum uint64) *types.L2BlockProto {
	return &types.L2BlockProto{
		L2Block: &datastream.L2Block{
			Number:      blockNum,
			BatchNumber: batchNum,
			Timestamp:   uint64(time.Now().Unix()),
		},
	}
}

func createTestBatch(size int) []*nats.Msg {
	batch := make([]*nats.Msg, size)
	for i := 0; i < size; i++ {
		batch[i] = &nats.Msg{
			Subject: "datastream.entry",
			Data:    []byte("test data"),
			Header: nats.Header{
				"EntryType": []string{"5"},
				"EntryNum":  []string{"0"},
			},
		}
	}
	return batch
}

func createTestBatchWithBookmarks(size int) []*nats.Msg {
	batch := make([]*nats.Msg, size)
	for i := 0; i < size; i++ {
		entryType := "5"
		if i%2 == 0 {
			entryType = "176"
		}

		bookmark := createTestBookmark(uint64(i), datastream.BookmarkType_BOOKMARK_TYPE_L2_BLOCK)
		data, _ := proto.Marshal(bookmark.BookMark)

		batch[i] = &nats.Msg{
			Subject: "datastream.entry",
			Data:    data,
			Header: nats.Header{
				"EntryType": []string{entryType},
				"EntryNum":  []string{"0"},
			},
		}
	}
	return batch
}
