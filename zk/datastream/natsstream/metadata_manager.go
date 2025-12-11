package natsstream

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// ErrMetadataKeyNotFound is returned when a key is not found in the metadata store
var ErrMetadataKeyNotFound = errors.New("metadata: key not found")

// MetadataManager manages metadata (bookmarks, total entries, etc.) using NATS KeyValue store
type MetadataManager struct {
	kv         jetstream.KeyValue
	manager    *Manager
	bucketName string
	logger     log.Logger
}

// NewMetadataManager creates a new metadata manager using NATS KV store
func NewMetadataManager(ctx context.Context, manager *Manager, logger log.Logger) (*MetadataManager, error) {
	if manager == nil {
		return nil, fmt.Errorf("manager cannot be nil")
	}

	m := &MetadataManager{
		manager:    manager,
		bucketName: "METADATA",
		logger:     logger,
	}

	// Get JetStream context
	js, err := m.manager.GetOrCreateDataStream(ctx)
	if err != nil {
		return nil, m.createInitError(fmt.Errorf("failed to get JetStream: %w", err))
	}

	// Try to get existing KV store
	kv, err := js.KeyValue(ctx, m.bucketName)
	if err == nil {
		m.kv = kv
		m.logger.Info("Using existing metadata KV store", "name", m.bucketName)
		return m, nil
	}

	// Create new KV store
	kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  m.bucketName,
		History: 1, // Only keep the most recent value
		TTL:     0, // No expiration
	})
	if err != nil {

		return nil, m.createInitError(fmt.Errorf("failed to create metadata KV store: %w", err))
	}

	m.kv = kv
	m.logger.Info("Created new metadata KV store", "name", m.bucketName)

	return m, nil
}

func (m *MetadataManager) createInitError(err error) error {
	return fmt.Errorf("failed to initialize metadata manager: %w", err)
}

// validateInitialized checks if the metadata manager is properly initialized
func (m *MetadataManager) validateInitialized() error {
	if m.manager == nil {
		return fmt.Errorf("metadata manager not properly initialized: manager is nil")
	}
	if m.kv == nil {
		return fmt.Errorf("metadata manager not properly initialized: KV store is nil")
	}
	return nil
}

// AddBookmark inserts or updates a bookmark
func (m *MetadataManager) AddBookmark(ctx context.Context, bookmark []byte, entryNum uint64) error {
	if err := m.validateInitialized(); err != nil {
		return err
	}

	// Convert entry number to bytes slice
	value := make([]byte, 8) // uint64 = 8 bytes
	binary.BigEndian.PutUint64(value, entryNum)

	// Use hex encoding for the key since bookmark bytes contain non-ASCII characters
	key := fmt.Sprintf("%x", bookmark)

	// Insert or update the bookmark into KV store
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := m.kv.Put(timeoutCtx, key, value)
	if err != nil {
		m.logger.Error("Error inserting or updating bookmark",
			"bookmark", bookmark,
			"entryNum", entryNum,
			"error", err)
		return err
	}

	m.logger.Debug("Bookmark added", "bookmark", bookmark, "entryNum", entryNum)
	return nil
}

// GetBookmark gets a bookmark value
func (m *MetadataManager) GetBookmark(ctx context.Context, bookmark []byte) (uint64, error) {
	// Use hex encoding for the key since bookmark bytes contain non-ASCII characters
	key := fmt.Sprintf("%x", bookmark)

	entry, err := m.GetValue(ctx, key)
	if err != nil {
		return 0, err
	}

	// Convert bytes slice to entry number
	entryNum := binary.BigEndian.Uint64(entry)

	m.logger.Debug("Bookmark retrieved", "bookmark", bookmark, "entryNum", entryNum)
	return entryNum, nil
}

// PrintDump prints all metadata stored in the KV store
func (m *MetadataManager) PrintDump(ctx context.Context) error {
	if err := m.validateInitialized(); err != nil {
		return err
	}

	var count uint64
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Create a watch context for the KV store (this gives us all keys)
	kw, err := m.kv.WatchAll(timeoutCtx)
	if err != nil {
		m.logger.Error("Error creating watch for metadata dump", "error", err)
		return err
	}
	defer kw.Stop()

	// Process each update (entry)
	for entry := range kw.Updates() {
		if entry == nil {
			break // End of stream
		}
		count++

		key := entry.Key()
		value := binary.BigEndian.Uint64(entry.Value())
		m.logger.Debug("Metadata entry", "key", key, "value", value)
	}

	m.logger.Info("Number of metadata entries", "count", count)
	return nil
}

// TruncateBookmarksAfter deletes all bookmarks that point to entries after the given entry number
func (m *MetadataManager) TruncateBookmarksAfter(ctx context.Context, entryNum uint64) error {
	if err := m.validateInitialized(); err != nil {
		return err
	}

	var bookmarksToDelete []string
	var deletedCount int

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Create a watch context for the KV store (this gives us all keys)
	kw, err := m.kv.WatchAll(timeoutCtx)
	if err != nil {
		m.logger.Error("Error creating watch for bookmark truncation", "error", err)
		return err
	}
	defer kw.Stop()

	// Process each update (entry) to find bookmarks pointing beyond entryNum
	for entry := range kw.Updates() {
		if entry == nil {
			break // End of stream
		}

		// Skip metadata entries (like total entries count)
		key := entry.Key()
		if key == MetadataTotalEntriesKey || key == MetadataLatestBlockBookmark {
			continue
		}

		// Check if this bookmark points beyond the truncation point
		bookmarkEntryNum := binary.BigEndian.Uint64(entry.Value())
		if bookmarkEntryNum > entryNum {
			bookmarksToDelete = append(bookmarksToDelete, key)
		}
	}

	// Delete all identified bookmarks
	if len(bookmarksToDelete) > 0 {
		for _, bookmark := range bookmarksToDelete {
			err := m.kv.Delete(timeoutCtx, bookmark)
			if err != nil {
				m.logger.Error("Failed to delete bookmark during truncation",
					"bookmark", bookmark,
					"error", err)
				// Continue with next bookmark
			} else {
				deletedCount++
			}
		}
	}

	m.logger.Info("Bookmark truncation completed",
		"entryNum", entryNum,
		"bookmarksDeleted", deletedCount,
		"bookmarksAttempted", len(bookmarksToDelete))

	return nil
}

// SetTotalEntries stores the total number of entries in the metadata store
func (m *MetadataManager) SetTotalEntries(ctx context.Context, totalEntries uint64) error {
	if err := m.validateInitialized(); err != nil {
		return err
	}

	// Convert total entries to bytes
	value := make([]byte, 8)
	binary.BigEndian.PutUint64(value, totalEntries)

	// Store in KV with metadata key
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := m.kv.Put(timeoutCtx, MetadataTotalEntriesKey, value)
	if err != nil {
		return fmt.Errorf("failed to store total entries in metadata: %w", err)
	}

	m.logger.Debug("Updated total entries in metadata store",
		"key", MetadataTotalEntriesKey,
		"totalEntries", totalEntries)

	return nil
}

// GetTotalEntries retrieves the total number of entries from the metadata store
func (m *MetadataManager) GetTotalEntries(ctx context.Context) (uint64, error) {
	entry, err := m.GetValue(ctx, MetadataTotalEntriesKey)
	if err != nil {
		return 0, err
	}

	// Convert bytes to uint64
	totalEntries := binary.BigEndian.Uint64(entry)
	m.logger.Debug("Retrieved total entries from metadata store", "totalEntries", totalEntries)
	return totalEntries, nil
}

// SetLatestBlockBookmark stores the bookmark for the latest L2 block
func (m *MetadataManager) SetLatestBlockBookmark(ctx context.Context, bookmark []byte) error {
	if err := m.validateInitialized(); err != nil {
		return err
	}

	// Store bookmark in KV with metadata key
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := m.kv.Put(timeoutCtx, MetadataLatestBlockBookmark, bookmark)
	if err != nil {
		return fmt.Errorf("failed to store latest block bookmark in metadata: %w", err)
	}

	m.logger.Debug("Updated latest block bookmark in metadata store",
		"key", MetadataLatestBlockBookmark,
		"bookmarkLen", len(bookmark))

	return nil
}

// GetLatestBlockBookmark retrieves the bookmark for the latest L2 block
func (m *MetadataManager) GetLatestBlockBookmark(ctx context.Context) ([]byte, error) {
	bookmark, err := m.GetValue(ctx, MetadataLatestBlockBookmark)
	if err != nil {
		return nil, err
	}

	m.logger.Debug("Retrieved latest block bookmark from metadata store", "bookmarkLen", len(bookmark))
	return bookmark, nil
}

func (m *MetadataManager) GetValue(ctx context.Context, key string) ([]byte, error) {
	if err := m.validateInitialized(); err != nil {
		return nil, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	entry, err := m.kv.Get(timeoutCtx, key)
	if err != nil {
		if errors.Is(err, nats.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, ErrMetadataKeyNotFound
		}
		m.logger.Error("Error getting value from KV store", "key", key, "error", err)
		return nil, err
	}

	return entry.Value(), nil
}
