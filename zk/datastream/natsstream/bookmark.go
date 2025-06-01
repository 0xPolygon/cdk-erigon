package natsstream

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NATSBookmark manages bookmarks using NATS KeyValue store
type NATSBookmark struct {
	kv          jetstream.KeyValue
	bucketName  string
	chainId     uint64
	logger      log.Logger
	initialized bool
}

// NewNATSBookmark creates a new bookmark manager using NATS KV store
func NewNATSBookmark(manager *Manager, chainId uint64, logger log.Logger) (*NATSBookmark, error) {
	b := &NATSBookmark{
		bucketName:  fmt.Sprintf("BOOKMARKS_%d", chainId),
		chainId:     chainId,
		logger:      logger,
		initialized: false,
	}

	// Initialize will be called lazily on first use
	return b, nil
}

// initialize creates or gets the KV bucket for bookmarks
func (b *NATSBookmark) initialize(manager *Manager) error {
	if b.initialized {
		return nil
	}

	// Get JetStream context
	js, err := manager.getJetStream()
	if err != nil {
		return fmt.Errorf("failed to get JetStream: %w", err)
	}

	// Try to get existing KV store
	kv, err := js.KeyValue(context.Background(), b.bucketName)
	if err == nil {
		b.kv = kv
		b.initialized = true
		b.logger.Info("Using existing bookmark KV store", "name", b.bucketName)
		return nil
	}

	// Create new KV store
	kv, err = js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:  b.bucketName,
		History: 1, // Only keep the most recent value
		TTL:     0, // No expiration
	})
	if err != nil {
		return fmt.Errorf("failed to create bookmark KV store: %w", err)
	}

	b.kv = kv
	b.initialized = true
	b.logger.Info("Created new bookmark KV store", "name", b.bucketName)
	return nil
}

// AddBookmark inserts or updates a bookmark
func (b *NATSBookmark) AddBookmark(manager *Manager, bookmark []byte, entryNum uint64) error {
	// Ensure we're initialized
	if err := b.initialize(manager); err != nil {
		return err
	}

	// Convert entry number to bytes slice
	value := make([]byte, 8) // uint64 = 8 bytes
	binary.BigEndian.PutUint64(value, entryNum)

	// Use bookmark bytes as key
	key := string(bookmark)

	// Insert or update the bookmark into KV store
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := b.kv.Put(ctx, key, value)
	if err != nil {
		b.logger.Error("Error inserting or updating bookmark",
			"bookmark", bookmark,
			"entryNum", entryNum,
			"error", err)
		return err
	}

	b.logger.Debug("Bookmark added", "bookmark", bookmark, "entryNum", entryNum)
	return nil
}

// GetBookmark gets a bookmark value
func (b *NATSBookmark) GetBookmark(manager *Manager, bookmark []byte) (uint64, error) {
	// Ensure we're initialized
	if err := b.initialize(manager); err != nil {
		return 0, err
	}

	// Use bookmark bytes as key
	key := string(bookmark)

	// Get the bookmark from KV store
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entry, err := b.kv.Get(ctx, key)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return 0, fmt.Errorf("bookmark not found: %w", err)
		}
		b.logger.Error("Error getting bookmark", "bookmark", bookmark, "error", err)
		return 0, err
	}

	// Convert bytes slice to entry number
	entryNum := binary.BigEndian.Uint64(entry.Value())

	b.logger.Debug("Bookmark retrieved", "bookmark", bookmark, "entryNum", entryNum)
	return entryNum, nil
}

// PrintDump prints all bookmarks stored in the KV store
func (b *NATSBookmark) PrintDump(manager *Manager) error {
	// Ensure we're initialized
	if err := b.initialize(manager); err != nil {
		return err
	}

	var count uint64
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a watch context for the KV store (this gives us all keys)
	kw, err := b.kv.WatchAll(ctx)
	if err != nil {
		b.logger.Error("Error creating watch for bookmark dump", "error", err)
		return err
	}
	defer kw.Stop()

	// Process each update (entry)
	for entry := range kw.Updates() {
		if entry == nil {
			break // End of stream
		}
		count++

		bookmark := entry.Key()
		entryNum := binary.BigEndian.Uint64(entry.Value())
		b.logger.Debug("Bookmark", "key", bookmark, "entryNum", entryNum)
	}

	b.logger.Info("Number of bookmarks", "count", count)
	return nil
}

// TruncateBookmarksAfter deletes all bookmarks that point to entries after the given entry number
func (b *NATSBookmark) TruncateBookmarksAfter(manager *Manager, entryNum uint64) error {
	// Ensure we're initialized
	if err := b.initialize(manager); err != nil {
		return err
	}

	var bookmarksToDelete []string
	var deletedCount int

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a watch context for the KV store (this gives us all keys)
	kw, err := b.kv.WatchAll(ctx)
	if err != nil {
		b.logger.Error("Error creating watch for bookmark truncation", "error", err)
		return err
	}
	defer kw.Stop()

	// Process each update (entry) to find bookmarks pointing beyond entryNum
	for entry := range kw.Updates() {
		if entry == nil {
			break // End of stream
		}

		// Check if this bookmark points beyond the truncation point
		bookmarkEntryNum := binary.BigEndian.Uint64(entry.Value())
		if bookmarkEntryNum > entryNum {
			bookmarksToDelete = append(bookmarksToDelete, entry.Key())
		}
	}

	// Delete all identified bookmarks
	if len(bookmarksToDelete) > 0 {
		for _, bookmark := range bookmarksToDelete {
			err := b.kv.Delete(ctx, bookmark)
			if err != nil {
				b.logger.Error("Failed to delete bookmark during truncation",
					"bookmark", bookmark,
					"error", err)
				// Continue with next bookmark
			} else {
				deletedCount++
			}
		}
	}

	b.logger.Info("Bookmark truncation completed",
		"entryNum", entryNum,
		"bookmarksDeleted", deletedCount,
		"bookmarksAttempted", len(bookmarksToDelete))

	return nil
}
