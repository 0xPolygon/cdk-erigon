package migration

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/client"
	"github.com/erigontech/erigon/zk/datastream/natsstream"
	"github.com/erigontech/erigon/zk/datastream/proto/github.com/0xPolygonHermez/zkevm-node/state/datastream"
	"github.com/erigontech/erigon/zk/datastream/server"
	"github.com/erigontech/erigon/zk/datastream/types"
	"github.com/gateway-fm/zkevm-data-streamer/datastreamer"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
)

type Migrator struct {
	tcpStreamFile string
	natsManager   *natsstream.Manager
	metadata      *natsstream.MetadataManager
	logger        log.Logger
	batchSize     int
	dryRun        bool
}

type MigrationStats struct {
	TotalEntries      uint64
	EntriesMigrated   uint64
	BookmarksMigrated uint64
	StartTime         time.Time
	EndTime           time.Time
	Errors            []error
}

func NewMigrator(tcpStreamFile string, natsManager *natsstream.Manager, logger log.Logger, batchSize int, dryRun bool) *Migrator {
	return &Migrator{
		tcpStreamFile: tcpStreamFile,
		natsManager:   natsManager,
		logger:        logger,
		batchSize:     batchSize,
		dryRun:        dryRun,
	}
}

func (m *Migrator) SetNATSManager(natsManager *natsstream.Manager) {
	m.natsManager = natsManager
}

func (m *Migrator) Migrate(ctx context.Context, startFrom uint64) (*MigrationStats, error) {
	stats := &MigrationStats{
		StartTime: time.Now(),
		Errors:    make([]error, 0),
	}

	m.logger.Info("Starting migration from TCP to NATS",
		"tcpFile", m.tcpStreamFile,
		"startFrom", startFrom,
		"batchSize", m.batchSize,
		"dryRun", m.dryRun)

	tcpServer, err := m.openTCPDatastream()
	if err != nil {
		return stats, fmt.Errorf("failed to open TCP datastream: %w", err)
	}

	header := tcpServer.GetHeader()
	stats.TotalEntries = header.TotalEntries

	m.logger.Info("TCP datastream opened",
		"totalEntries", stats.TotalEntries,
		"systemID", header.SystemID)

	if stats.TotalEntries == 0 {
		m.logger.Warn("TCP datastream is empty, nothing to migrate")
		stats.EndTime = time.Now()
		return stats, nil
	}

	if err := m.initializeNATSMetadata(ctx); err != nil {
		return stats, fmt.Errorf("failed to initialize NATS metadata: %w", err)
	}

	iterator := &tcpIterator{
		stream:       tcpServer,
		curEntryNum:  startFrom,
		totalEntries: stats.TotalEntries,
	}

	if err := m.migrateEntries(ctx, iterator, stats); err != nil {
		return stats, fmt.Errorf("failed to migrate entries: %w", err)
	}

	stats.EndTime = time.Now()
	m.logger.Info("Migration completed",
		"totalEntries", stats.TotalEntries,
		"migratedEntries", stats.EntriesMigrated,
		"bookmarksMigrated", stats.BookmarksMigrated,
		"duration", stats.EndTime.Sub(stats.StartTime),
		"errors", len(stats.Errors))

	return stats, nil
}

func (m *Migrator) openTCPDatastream() (server.StreamServer, error) {
	const datastreamVersion = 3
	tcpServer, err := datastreamer.NewServer(
		0,
		datastreamVersion,
		4334,
		datastreamer.StreamType(1),
		m.tcpStreamFile,
		10*time.Second,
		60*time.Second,
		30*time.Second,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create TCP datastream server: %w", err)
	}

	return tcpServer, nil
}

func (m *Migrator) initializeNATSMetadata(ctx context.Context) error {
	if m.dryRun {
		m.logger.Info("Dry run mode: skipping NATS initialization")
		return nil
	}

	var err error
	m.metadata, err = natsstream.NewMetadataManager(ctx, m.natsManager, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create metadata manager: %w", err)
	}

	err = m.metadata.SetTotalEntries(ctx, 0)
	if err != nil {
		return fmt.Errorf("failed to initialize total entries: %w", err)
	}

	m.logger.Info("NATS metadata initialized")
	return nil
}

func (m *Migrator) migrateEntries(ctx context.Context, iterator *tcpIterator, stats *MigrationStats) error {
	var js jetstream.JetStream
	var err error

	if !m.dryRun {
		js, err = m.natsManager.GetOrCreateDataStream(ctx)
		if err != nil {
			return fmt.Errorf("failed to get JetStream: %w", err)
		}
	}

	batch := make([]*nats.Msg, 0, m.batchSize)
	currentBlock := uint64(0)
	lastBookmark := []byte(nil)
	var latestL2BlockBookmark []byte

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		entry, err := iterator.NextFileEntry()
		if err != nil {
			stats.Errors = append(stats.Errors, err)
			return fmt.Errorf("failed to read entry %d: %w", iterator.curEntryNum, err)
		}

		if entry == nil {
			break
		}

		msg, isBookmark, isL2BlockEnd, err := m.createNATSMessage(entry, iterator.curEntryNum)
		if err != nil {
			stats.Errors = append(stats.Errors, err)
			m.logger.Warn("Failed to create NATS message", "entryNum", iterator.curEntryNum, "error", err)
			continue
		}

		if isBookmark {
			lastBookmark = entry.Data
			stats.BookmarksMigrated++
		}

		if isL2BlockEnd {
			if lastBookmark != nil {
				latestL2BlockBookmark = lastBookmark
			}
		}

		batch = append(batch, msg)

		if len(batch) >= m.batchSize || iterator.curEntryNum >= stats.TotalEntries-1 {
			if err := m.publishBatch(ctx, js, batch, &currentBlock, stats); err != nil {
				return err
			}
			batch = batch[:0]
		}

		if iterator.curEntryNum%10000 == 0 {
			m.logger.Info("Migration progress",
				"entryNum", iterator.curEntryNum,
				"totalEntries", stats.TotalEntries,
				"progress", fmt.Sprintf("%.2f%%", float64(iterator.curEntryNum)/float64(stats.TotalEntries)*100))
		}
	}

	if len(batch) > 0 {
		if err := m.publishBatch(ctx, js, batch, &currentBlock, stats); err != nil {
			return err
		}
	}

	if latestL2BlockBookmark != nil && !m.dryRun {
		if err := m.metadata.SetLatestBlockBookmark(ctx, latestL2BlockBookmark); err != nil {
			m.logger.Warn("Failed to set latest block bookmark", "error", err)
		}
	}

	return nil
}

func (m *Migrator) createNATSMessage(entry *types.FileEntry, entryNum uint64) (*nats.Msg, bool, bool, error) {
	entryTypeStr := strconv.Itoa(int(entry.EntryType))

	msg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    entry.Data,
		Header: nats.Header{
			"EntryType": []string{entryTypeStr},
			"EntryNum":  []string{fmt.Sprintf("%d", entryNum)},
		},
	}

	isBookmark := entry.EntryType == types.BookmarkEntryType
	isL2BlockEnd := entry.EntryType == types.EntryTypeL2BlockEnd

	return msg, isBookmark, isL2BlockEnd, nil
}

func (m *Migrator) publishBatch(ctx context.Context, js jetstream.JetStream, batch []*nats.Msg, currentBlock *uint64, stats *MigrationStats) error {
	if m.dryRun {
		m.logger.Debug("Dry run: would publish batch", "size", len(batch))
		stats.EntriesMigrated += uint64(len(batch))
		return nil
	}

	for _, msg := range batch {
		_, err := js.PublishMsg(ctx, msg)
		if err != nil {
			return fmt.Errorf("failed to publish message: %w", err)
		}

		stats.EntriesMigrated++

		entryTypeStr := msg.Header.Get("EntryType")
		entryNum := stats.EntriesMigrated - 1

		if entryTypeStr == "176" {
			natsSequence := stats.EntriesMigrated
			err = m.metadata.AddBookmark(ctx, msg.Data, natsSequence)
			if err != nil {
				m.logger.Warn("Failed to store bookmark", "entryNum", entryNum, "error", err)
			}
		}

		if entryTypeStr == "5" {
			bookmark := &datastream.BookMark{}
			if err := proto.Unmarshal(msg.Data, bookmark); err == nil {
				*currentBlock = bookmark.Value
			}
		}
	}

	if err := m.metadata.SetTotalEntries(ctx, stats.EntriesMigrated); err != nil {
		return fmt.Errorf("failed to update total entries: %w", err)
	}

	return nil
}

type tcpIterator struct {
	stream       server.StreamServer
	curEntryNum  uint64
	totalEntries uint64
}

func (it *tcpIterator) NextFileEntry() (*types.FileEntry, error) {
	if it.curEntryNum >= it.totalEntries {
		return nil, nil
	}

	fileEntry, err := it.stream.GetEntry(it.curEntryNum)
	if err != nil {
		return nil, err
	}

	it.curEntryNum++

	return &types.FileEntry{
		PacketType: uint8(fileEntry.Type),
		Length:     fileEntry.Length,
		EntryType:  types.EntryType(fileEntry.Type),
		EntryNum:   fileEntry.Number,
		Data:       fileEntry.Data,
	}, nil
}

func (it *tcpIterator) GetEntryNumberLimit() uint64 {
	return it.totalEntries
}

var _ client.FileEntryIterator = (*tcpIterator)(nil)

type ExportedEntry struct {
	Number    uint64 `json:"number"`
	EntryType uint32 `json:"entry_type"`
	Data      string `json:"data"`
}

type ExportedBookmark struct {
	Key   string `json:"key"`
	Entry uint64 `json:"entry"`
}

type DatastreamExport struct {
	TotalEntries uint64             `json:"total_entries"`
	Entries      []ExportedEntry    `json:"entries"`
	Bookmarks    []ExportedBookmark `json:"bookmarks"`
}

func (m *Migrator) Export(ctx context.Context, outputFile string) error {
	m.logger.Info("Starting datastream export", "outputFile", outputFile)

	tcpServer, err := m.openTCPDatastream()
	if err != nil {
		return fmt.Errorf("failed to open TCP datastream: %w", err)
	}

	header := tcpServer.GetHeader()
	totalEntries := header.TotalEntries

	m.logger.Info("TCP datastream opened",
		"totalEntries", totalEntries,
		"systemID", header.SystemID)

	if totalEntries == 0 {
		m.logger.Warn("TCP datastream is empty, nothing to export")
		return fmt.Errorf("datastream is empty")
	}

	iterator := &tcpIterator{
		stream:       tcpServer,
		curEntryNum:  0,
		totalEntries: totalEntries,
	}

	exportData := DatastreamExport{
		TotalEntries: totalEntries,
		Entries:      make([]ExportedEntry, 0, totalEntries),
		Bookmarks:    make([]ExportedBookmark, 0),
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		entry, err := iterator.NextFileEntry()
		if err != nil {
			return fmt.Errorf("failed to read entry %d: %w", iterator.curEntryNum, err)
		}

		if entry == nil {
			break
		}

		exportedEntry := ExportedEntry{
			Number:    entry.EntryNum,
			EntryType: uint32(entry.EntryType),
			Data:      hex.EncodeToString(entry.Data),
		}
		exportData.Entries = append(exportData.Entries, exportedEntry)

		if entry.EntryType == types.BookmarkEntryType {
			bookmark := ExportedBookmark{
				Key:   hex.EncodeToString(entry.Data),
				Entry: entry.EntryNum,
			}
			exportData.Bookmarks = append(exportData.Bookmarks, bookmark)
		}

		if iterator.curEntryNum%10000 == 0 {
			m.logger.Info("Export progress",
				"entryNum", iterator.curEntryNum,
				"totalEntries", totalEntries,
				"progress", fmt.Sprintf("%.2f%%", float64(iterator.curEntryNum)/float64(totalEntries)*100))
		}
	}

	m.logger.Info("Writing export file", "entries", len(exportData.Entries), "bookmarks", len(exportData.Bookmarks))

	jsonData, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal export data: %w", err)
	}

	if err := os.WriteFile(outputFile, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}

	m.logger.Info("Export complete",
		"outputFile", outputFile,
		"entries", len(exportData.Entries),
		"bookmarks", len(exportData.Bookmarks))

	return nil
}

func (m *Migrator) ExportNATS(ctx context.Context, outputFile string) error {
	m.logger.Info("Starting NATS datastream export", "outputFile", outputFile)

	if m.natsManager == nil {
		return fmt.Errorf("NATS manager not initialized")
	}

	js, err := m.natsManager.GetOrCreateDataStream(ctx)
	if err != nil {
		return fmt.Errorf("failed to get JetStream: %w", err)
	}

	metadata, err := natsstream.NewMetadataManager(ctx, m.natsManager, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create metadata manager: %w", err)
	}

	totalEntries, err := metadata.GetTotalEntries(ctx)
	if err != nil {
		return fmt.Errorf("failed to get total entries: %w", err)
	}

	m.logger.Info("NATS datastream opened", "totalEntries", totalEntries)

	if totalEntries == 0 {
		m.logger.Warn("NATS datastream is empty, nothing to export")
		return fmt.Errorf("datastream is empty")
	}

	exportData := DatastreamExport{
		TotalEntries: totalEntries,
		Entries:      make([]ExportedEntry, 0, totalEntries),
		Bookmarks:    make([]ExportedBookmark, 0),
	}

	consumer, err := js.CreateOrUpdateConsumer(ctx, "DATASTREAM", jetstream.ConsumerConfig{
		Name:          "export-consumer",
		Durable:       "export-consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	msgs, err := consumer.Fetch(int(totalEntries), jetstream.FetchMaxWait(30*time.Second))
	if err != nil {
		return fmt.Errorf("failed to fetch messages: %w", err)
	}

	entryNum := uint64(0)
	for msg := range msgs.Messages() {
		entryTypeStr := msg.Headers().Get("EntryType")
		var entryType uint32
		if n, err := strconv.Atoi(entryTypeStr); err == nil {
			entryType = uint32(n)
		}

		exportedEntry := ExportedEntry{
			Number:    entryNum,
			EntryType: entryType,
			Data:      hex.EncodeToString(msg.Data()),
		}
		exportData.Entries = append(exportData.Entries, exportedEntry)

		if entryType == uint32(types.BookmarkEntryType) {
			bookmark := ExportedBookmark{
				Key:   hex.EncodeToString(msg.Data()),
				Entry: entryNum,
			}
			exportData.Bookmarks = append(exportData.Bookmarks, bookmark)
		}

		msg.Ack()
		entryNum++

		if entryNum%10000 == 0 {
			m.logger.Info("Export progress",
				"entryNum", entryNum,
				"totalEntries", totalEntries,
				"progress", fmt.Sprintf("%.2f%%", float64(entryNum)/float64(totalEntries)*100))
		}
	}

	if msgs.Error() != nil {
		return fmt.Errorf("error fetching messages: %w", msgs.Error())
	}

	m.logger.Info("Writing export file", "entries", len(exportData.Entries), "bookmarks", len(exportData.Bookmarks))

	jsonData, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal export data: %w", err)
	}

	if err := os.WriteFile(outputFile, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}

	m.logger.Info("NATS export complete",
		"outputFile", outputFile,
		"entries", len(exportData.Entries),
		"bookmarks", len(exportData.Bookmarks))

	return nil
}
