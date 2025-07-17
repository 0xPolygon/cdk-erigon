package natsstream

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon/zk/datastream/server"
	"github.com/gateway-fm/zkevm-data-streamer/datastreamer"
	"github.com/nats-io/nats.go"
)

// Metadata keys for KV storage
const (
	MetadataTotalEntriesKey     = "METADATA_TOTAL_ENTRIES"
	MetadataLatestBlockBookmark = "METADATA_LATEST_BLOCK_BOOKMARK"
)

// BookmarkTx represents a bookmark to be stored during transaction commit
type BookmarkTx struct {
	bookmark []byte
	entryNum uint64
}

// NATSStreamServer implements the server.StreamServer interface
// It wraps an existing StreamServer implementation and forwards all calls to it
// While also publishing events to NATS for external subscribers
type NATSStreamServer struct {
	// The underlying StreamServer implementation
	delegate server.StreamServer
	// NATS manager for stream operations
	natsManager *Manager
	// Logger
	logger log.Logger
	// Metadata manager
	metadata *MetadataManager

	// Transaction related fields
	txActive          bool
	txMsgs            []*nats.Msg
	txMutex           sync.RWMutex
	nextEntry         uint64
	currentBlockStart uint64 // Entry number where current L2 block started
	lastBookmark      []byte // Last bookmark seen in current transaction
}

// Start starts the underlying stream server
func (n *NATSStreamServer) Start() error {

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Initialize next entry counter from NATS stream info - this is required
	if n.natsManager == nil || n.natsManager.mainStream == nil {
		return fmt.Errorf("NATS manager or main stream not initialized - cannot start server")
	}

	// Initialize metadata manager if not already initialized
	var err error
	if n.metadata == nil {
		n.metadata, err = NewMetadataManager(ctx, n.natsManager, n.logger)
		if err != nil {
			n.logger.Error("Failed to initialize NATS metadata manager", "error", err)
			return fmt.Errorf("metadata manager initialization failed: %w", err)
		}
	}

	info, err := n.natsManager.mainStream.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get NATS stream info for initialization: %w", err)
	}

	// Get the current count from stream - this is our starting point
	streamCount := info.State.LastSeq
	n.logger.Info("NATS stream current state",
		"streamMessages", streamCount,
		"lastMsgTime", info.State.LastTime)

	// Initialize total entries in KV store - this is required for consistency
	if n.metadata == nil {
		return fmt.Errorf("metadata manager not initialized - cannot start server")
	}

	// Check if we have an existing KV count - if so, verify consistency
	existingCount, err := n.metadata.GetTotalEntries(ctx)
	if err != nil && !errors.Is(err, ErrMetadataKeyNotFound) {
		return fmt.Errorf("failed to check existing total entries in metadata store: %w", err)
	}

	// Determine the correct initialisation approach
	if err != nil {
		// No existing KV count
		if streamCount == 0 {
			// Fresh start - no stream, no KV
			n.nextEntry = 0
			n.logger.Info("Fresh start: no stream data, no KV data", "count", n.nextEntry)

			// Set initial KV entry to 0
			err = n.metadata.SetTotalEntries(ctx, n.nextEntry)
			if err != nil {
				return fmt.Errorf("failed to initialize total entries in metadata store: %w", err)
			}
		} else {
			// Stream has data but no KV - this is an unknown/inconsistent state
			return fmt.Errorf("stream has %d messages but no KV entry exists - unknown state, cannot continue", streamCount)
		}
	}
	// We have an existing KV count - use it as the source of truth
	n.nextEntry = existingCount
	n.logger.Info("Resuming: using KV metadata as source of truth",
		"kvCount", existingCount,
		"streamCount", streamCount,
		"nextEntry", n.nextEntry)

	n.logger.Info("Initialization complete", "totalEntries", n.nextEntry)

	return n.delegate.Start()
}

// StartAtomicOp starts an atomic operation in the underlying stream server
func (n *NATSStreamServer) StartAtomicOp() error {
	// First start the atomic operation in the delegate
	err := n.delegate.StartAtomicOp()
	if err != nil {
		return err
	}

	// Begin a transaction in our local memory store
	n.txMutex.Lock()
	defer n.txMutex.Unlock()

	if n.txActive {
		// This is just a safeguard; the datastream API should prevent this
		n.logger.Warn("Starting a new transaction while one is already active; previous transaction data will be lost")
	}

	// Reset transaction state
	n.txActive = true
	n.txMsgs = make([]*nats.Msg, 0, 100) // Pre-allocate space for 100 messages

	n.logger.Debug("Started transaction")
	return nil
}

// addStreamItem is a helper function that adds an item to the NATS cache
// for later publication. It handles the common logic between AddStreamEntry and AddStreamBookmark.
func (n *NATSStreamServer) addStreamItem(entryType string, data []byte, delegateEntryNum uint64, itemType string) (uint64, error) {
	if !n.txActive {
		n.logger.Warn(itemType + " called outside of a transaction; item will not be cached for NATS")
		return 0, fmt.Errorf("%s called outside of a transaction; item will not be cached for NATS", itemType)
	}

	// Store the item in our transaction buffer
	n.txMutex.Lock()
	defer n.txMutex.Unlock()

	// Create a NATS message for later publishing
	// Entry number will be assigned during commit when we actually publish
	msg := &nats.Msg{
		Subject: "datastream.entry",
		Data:    append([]byte(nil), data...), // Make a copy of data to avoid potential mutations
		Header: nats.Header{
			"EntryType": []string{entryType},
		},
	}
	n.txMsgs = append(n.txMsgs, msg)

	n.logger.Debug("Cached message for later publishing",
		"type", itemType,
		"cacheSize", len(n.txMsgs))

	// Return placeholder since callers discard this value anyway
	return 0, nil
}

// AddStreamEntry adds an entry to the stream and buffers it for NATS
func (n *NATSStreamServer) AddStreamEntry(entryType datastreamer.EntryType, data []byte) (uint64, error) {
	// First add the entry to the underlying stream
	entryNum, err := n.delegate.AddStreamEntry(entryType, data)
	if err != nil {
		return 0, err
	}

	return n.addStreamItem(
		strconv.Itoa(int(entryType)),
		data,
		entryNum,
		"AddStreamEntry",
	)
}

// AddStreamBookmark adds a bookmark to the stream and buffers it for NATS
func (n *NATSStreamServer) AddStreamBookmark(bookmark []byte) (uint64, error) {
	// First add the bookmark to the underlying stream
	entryNum, err := n.delegate.AddStreamBookmark(bookmark)
	if err != nil {
		return 0, err
	}

	return n.addStreamItem(
		"176", // 0xb0 - EtBookmark
		bookmark,
		entryNum,
		"AddStreamBookmark",
	)
}

// CommitAtomicOp commits an atomic operation, publishing all buffered entries to NATS
func (n *NATSStreamServer) CommitAtomicOp() error {
	// First commit the atomic operation in the delegate
	err := n.delegate.CommitAtomicOp()
	if err != nil {
		return err
	}

	// Publish all buffered entries to NATS
	n.txMutex.Lock()
	defer n.txMutex.Unlock()

	if !n.txActive {
		n.logger.Warn("Commit called but no transaction is active")
		return nil
	}

	// Publish all cached messages
	msgCount := len(n.txMsgs)
	if msgCount > 0 && n.natsManager != nil {
		n.logger.Info("Publishing cached messages to NATS",
			"count", msgCount,
			"currentNextEntry", n.nextEntry,
			"expectedFinalEntry", n.nextEntry+uint64(msgCount))

		// Get JetStream to publish messages
		js, err := n.natsManager.GetOrCreateDataStream(context.Background())
		if err != nil {
			n.logger.Error("Failed to get JetStream for publishing", "error", err)
			// Continue without publishing
		} else {
			// Create shared transaction context for all metadata operations
			txCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			// Track if we see any L2 blocks in this transaction
			var lastL2BlockBookmark []byte
			var lastL2BlockFound bool

			// Publish all messages and update KV atomically for each message
			for i, msg := range n.txMsgs {
				entryTypeHeader := msg.Header.Get("EntryType")

				// Assign entry number now that we're actually publishing
				currentEntryNum := n.nextEntry
				msg.Header.Set("EntryNum", fmt.Sprintf("%d", currentEntryNum))

				n.logger.Debug("Publishing message to NATS",
					"index", i,
					"entryType", entryTypeHeader,
					"entryNum", currentEntryNum)

				// Publish the message
				_, err := js.PublishMsg(context.Background(), msg)
				if err != nil {
					n.logger.Error("Failed to publish message to NATS during commit",
						"error", err,
						"index", i,
						"entryType", entryTypeHeader,
						"entryNum", currentEntryNum)
					// CRITICAL: Don't continue if publish fails - this would create inconsistency
					return fmt.Errorf("failed to publish message %d: %w", i, err)
				}

				// Increment counter immediately after successful publish
				n.nextEntry++

				// Update total entries count in KV store immediately after each successful publish
				// This ensures KV is always consistent with published messages
				err = n.metadata.SetTotalEntries(txCtx, n.nextEntry)
				if err != nil {
					n.logger.Error("Failed to update total entries in metadata store after publish",
						"error", err,
						"entryNum", currentEntryNum,
						"totalEntries", n.nextEntry,
						"entryType", entryTypeHeader)
					// This is critical - if we can't update KV, we have inconsistency
					return fmt.Errorf("failed to update KV after publishing message %d: %w", i, err)
				}

				n.logger.Debug("Successfully published and updated KV",
					"index", i,
					"entryType", entryTypeHeader,
					"entryNum", currentEntryNum,
					"newTotalEntries", n.nextEntry)

				// Store bookmark if this is a bookmark entry (already got entryTypeHeader above)
				if entryTypeHeader == "176" && n.metadata != nil { // EntryType for bookmark
					// Convert 0-based entry number to 1-based NATS sequence number
					natsSequence := n.nextEntry
					err = n.metadata.AddBookmark(txCtx, msg.Data, natsSequence)
					if err != nil {
						n.logger.Error("Failed to store bookmark in NATS KV",
							"error", err,
							"entryNum", n.nextEntry-1,
							"natsSequence", natsSequence)
						// Continue even if bookmark storage fails - this is less critical
					} else {
						n.logger.Debug("Stored bookmark in KV store",
							"entryNum", n.nextEntry-1,
							"natsSequence", natsSequence)
					}
					// Remember this bookmark in case it's before an L2 block
					n.lastBookmark = msg.Data
				} else if entryTypeHeader == "5" { // L2BlockStart
					// When we see a block start, remember the current position
					n.currentBlockStart = n.nextEntry - 1
				} else if entryTypeHeader == "6" { // L2BlockEnd
					// When we see a block end, we should update the latest block bookmark
					// The bookmark for this block is the last bookmark we saw before the block start
					if n.lastBookmark != nil && n.metadata != nil {
						lastL2BlockBookmark = n.lastBookmark
						lastL2BlockFound = true
					}
				}
			}

			// After publishing all messages, update the latest block bookmark if we found one
			if lastL2BlockFound && lastL2BlockBookmark != nil && n.metadata != nil {
				err = n.metadata.SetLatestBlockBookmark(txCtx, lastL2BlockBookmark)
				if err != nil {
					n.logger.Error("Failed to update latest block bookmark",
						"error", err)
					// Continue even if metadata update fails
				}
			}
		}
	}

	// Reset transaction state
	n.txActive = false
	n.txMsgs = nil

	return nil
}

// RollbackAtomicOp rolls back an atomic operation, discarding all buffered entries
func (n *NATSStreamServer) RollbackAtomicOp() error {
	// First roll back the atomic operation in the delegate
	err := n.delegate.RollbackAtomicOp()
	if err != nil {
		return err
	}

	// Clear all buffered entries
	n.txMutex.Lock()
	defer n.txMutex.Unlock()

	if !n.txActive {
		n.logger.Warn("Rollback called but no transaction is active")
		return nil
	}

	msgCount := len(n.txMsgs)
	n.logger.Info("Rolling back transaction, discarding cached messages",
		"count", msgCount)

	// Reset transaction state
	n.txActive = false
	n.txMsgs = nil

	return nil
}

// TruncateFile truncates the file in the underlying stream server and deletes messages from NATS stream
func (n *NATSStreamServer) TruncateFile(entryNum uint64) error {
	n.logger.Info("Truncating datastream file and NATS stream", "entryNum", entryNum)

	// First truncate the file in the delegate
	err := n.delegate.TruncateFile(entryNum)
	if err != nil {
		n.logger.Error("Failed to truncate delegate stream", "error", err, "entryNum", entryNum)
		return err
	}

	// Now delete messages from NATS JetStream
	if n.natsManager != nil && n.natsManager.mainStream != nil {
		// Convert our entry number to NATS sequence number (NATS is 1-based)
		// Any entry > entryNum should be deleted, so startSeq is entryNum+1
		startSeq := entryNum + 1

		// Get stream info to check current state
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		info, err := n.natsManager.mainStream.Info(ctx)
		if err != nil {
			n.logger.Error("Failed to get stream info for truncation",
				"error", err,
				"entryNum", entryNum)
			// Continue without failing as the delegate truncation was successful
			// We'll log the error but return nil to allow operation to continue
			return nil
		}

		// Check if there are messages to delete
		if info.State.LastSeq >= startSeq {
			totalToDelete := info.State.LastSeq - startSeq + 1
			n.logger.Info("Deleting messages from NATS stream",
				"fromSeq", startSeq,
				"toSeq", info.State.LastSeq,
				"totalToDelete", totalToDelete)

			// Track deleted and failed messages
			deletedCount := uint64(0)
			failedCount := uint64(0)

			// Delete messages sequentially
			for seq := startSeq; seq <= info.State.LastSeq; seq++ {
				// Create a context with timeout for each deletion
				deleteCtx, deleteCancel := context.WithTimeout(context.Background(), 1*time.Second)
				err := n.natsManager.mainStream.DeleteMsg(deleteCtx, seq)
				deleteCancel()

				if err != nil {
					failedCount++
					if failedCount <= 5 { // Only log the first few errors to avoid flooding
						n.logger.Warn("Failed to delete message", "seq", seq, "error", err)
					}
				} else {
					deletedCount++
				}
			}

			n.logger.Info("Truncated NATS stream",
				"fromSeq", startSeq,
				"toSeq", info.State.LastSeq,
				"messagesDeleted", deletedCount,
				"messagesFailed", failedCount,
				"totalAttempted", totalToDelete)
		} else {
			n.logger.Info("No messages to truncate from NATS stream",
				"requestedSeq", startSeq,
				"lastSeq", info.State.LastSeq)
		}

		// Reset next entry counter to match truncated state
		// Unlike file-based storage, NATS keeps sequence numbers as gaps after deletion
		// So we need to get updated stream info to properly calculate the effective entry count
		postTruncCtx, postTruncCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer postTruncCancel()

		postInfo, err := n.natsManager.mainStream.Info(postTruncCtx)
		if err == nil {
			// Calculate effective entries (counting only non-deleted messages)
			numDeleted := uint64(len(postInfo.State.Deleted))
			effectiveEntries := entryNum + 1 // Include the entry we kept

			// Update our nextEntry to reflect the truncated state
			n.nextEntry = effectiveEntries

			n.logger.Info("Updated nextEntry counter after truncation",
				"nextEntry", n.nextEntry,
				"deletedCount", numDeleted)
		} else {
			// Fall back to the simple approach if we can't get stream info
			n.nextEntry = entryNum + 1
			n.logger.Warn("Could not get stream info after truncation, using fallback counter value",
				"error", err,
				"nextEntry", n.nextEntry)
		}

		// If we have bookmarks that point beyond the truncation point, they should be cleaned up
		if n.metadata != nil {
			// Create shared context for all truncation metadata operations
			truncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Since we now have TruncateBookmarksAfter method, use it
			err = n.metadata.TruncateBookmarksAfter(truncCtx, entryNum)
			if err != nil {
				n.logger.Error("Failed to truncate bookmarks after entry",
					"error", err,
					"entryNum", entryNum)
				// Non-fatal error, continue
			}

			// Update total entries count in metadata store after truncation
			err = n.metadata.SetTotalEntries(truncCtx, n.nextEntry)
			if err != nil {
				n.logger.Error("Failed to update total entries in metadata store after truncation",
					"error", err,
					"totalEntries", n.nextEntry)
				// Continue even if metadata update fails
			}
		}
	}

	return nil
}

// UpdateEntryData updates an entry's data in the underlying stream
func (n *NATSStreamServer) UpdateEntryData(entryNum uint64, etype datastreamer.EntryType, data []byte) error {
	//obsolete method throw error
	return fmt.Errorf("UpdateEntryData is not supported in NATSStreamServer, use AddStreamEntry instead")
}

// GetHeader gets the stream header - NATS is the authoritative source
func (n *NATSStreamServer) GetHeader() datastreamer.HeaderEntry {
	// Create a basic HeaderEntry with minimal required fields
	header := datastreamer.HeaderEntry{
		Version:  1,    // Use a default version
		SystemID: 4334, // Example system ID
	}

	// Get authoritative count from KV store - this must work
	if n.metadata == nil || n.natsManager == nil {
		n.logger.Info("Cannot get header: metadata manager or NATS manager not initialized")
		// This should not happen after Start() completes successfully
		header.TotalEntries = 0
		return header
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	totalEntries, err := n.metadata.GetTotalEntries(ctx)
	if err != nil {
		n.logger.Info("Failed to get total entries from metadata store", "error", err)
		// Return header with zero entries rather than crashing
		header.TotalEntries = 0
		return header
	}

	header.TotalEntries = totalEntries
	return header
}

// GetEntry gets an entry from NATS JetStream - NATS is the authoritative source
func (n *NATSStreamServer) GetEntry(entryNum uint64) (datastreamer.FileEntry, error) {
	// NATS must be available for this operation
	if n.natsManager == nil || n.natsManager.mainStream == nil {
		return datastreamer.FileEntry{}, fmt.Errorf("NATS manager or main stream not initialized")
	}

	// NATS sequence numbers are 1-based, but our entry numbers are 0-based
	// We need to add 1 to convert our entry number to a NATS sequence number
	seqNum := entryNum + 1

	// Get the message by sequence number
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg, err := n.natsManager.mainStream.GetMsg(ctx, seqNum)
	if err != nil {
		return datastreamer.FileEntry{}, fmt.Errorf("failed to get entry %d (seq %d) from NATS: %w", entryNum, seqNum, err)
	}

	// Convert NATS message to FileEntry
	fileEntry := datastreamer.FileEntry{}

	// Copy data
	fileEntry.Data = msg.Data

	// Set entry number
	fileEntry.Number = entryNum

	// Parse entry type from header
	entryTypeStr := msg.Header.Get("EntryType")
	if entryTypeStr != "" {
		entryType, err := strconv.ParseUint(entryTypeStr, 10, 32)
		if err == nil {
			fileEntry.Type = datastreamer.EntryType(entryType)
		} else {
			n.logger.Warn("Failed to parse EntryType header, using default",
				"entryTypeStr", entryTypeStr,
				"entryNum", entryNum,
				"error", err)
		}
	}

	// Set length - calculate based on data size plus fixed header size
	fileEntry.Length = uint32(len(fileEntry.Data)) + 1 + 4 + 4 + 8 // packetType(1) + Length(4) + Type(4) + Number(8)

	return fileEntry, nil
}

// GetBookmark gets a bookmark from the NATS KV store - NATS is the authoritative source
func (n *NATSStreamServer) GetBookmark(bookmark []byte) (uint64, error) {
	// NATS metadata must be available for this operation
	if n.metadata == nil {
		return 0, fmt.Errorf("metadata manager not initialized")
	}

	bookmarkGetCtx, bookmarkGetCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer bookmarkGetCancel()
	entryNum, err := n.metadata.GetBookmark(bookmarkGetCtx, bookmark)
	if err != nil {
		return 0, fmt.Errorf("failed to get bookmark from NATS KV: %w", err)
	}

	return entryNum, nil
}

// GetFirstEventAfterBookmark gets the first event after a bookmark from the underlying stream server
func (n *NATSStreamServer) GetFirstEventAfterBookmark(bookmark []byte) (datastreamer.FileEntry, error) {
	// Get the bookmark entry number
	bookmarkEntryNum, err := n.GetBookmark(bookmark)
	if err != nil {
		return datastreamer.FileEntry{}, err
	}

	// Get current header info to know how many entries we have
	header := n.GetHeader()
	if bookmarkEntryNum >= header.TotalEntries-1 {
		return datastreamer.FileEntry{}, fmt.Errorf("no entries after bookmark at position %d", bookmarkEntryNum)
	}

	// Start searching from the entry after the bookmark
	currentEntryNum := bookmarkEntryNum + 1

	// NATS may have deleted messages or more bookmarks, so we need to
	// iterate until we find a valid non-bookmark entry
	for currentEntryNum < header.TotalEntries {
		entry, err := n.GetEntry(currentEntryNum)
		if err == nil {
			// Skip bookmarks, we want the first actual data entry
			if entry.Type != datastreamer.EtBookmark {
				return entry, nil
			}
		} else {
			// Log warning but continue searching - entry might not exist due to deletions
			n.logger.Warn("Failed to get entry while searching for first event after bookmark",
				"entryNum", currentEntryNum,
				"error", err)
		}
		// If entry doesn't exist or is a bookmark, try the next one
		currentEntryNum++
	}

	return datastreamer.FileEntry{}, fmt.Errorf("no valid entries found after bookmark at position %d", bookmarkEntryNum)
}

// GetDataBetweenBookmarks gets data between bookmarks from the underlying stream server
func (n *NATSStreamServer) GetDataBetweenBookmarks(bookmarkFrom, bookmarkTo []byte) ([]byte, error) {
	// TODO: This method appears to be unused and should be removed from the interface
	// after the original datastreamer is retired. We're not implementing it for NATS.
	panic("GetDataBetweenBookmarks is not implemented for NATS and appears to be unused. " +
		"This method should be removed from the interface after datastreamer is fully retired.")
}

// BookmarkPrintDump prints the metadata dump from NATS KV - NATS is the authoritative source
func (n *NATSStreamServer) BookmarkPrintDump() {
	// NATS metadata must be available for this operation
	if n.metadata == nil {
		n.logger.Error("Cannot print bookmark dump: metadata manager not initialized")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := n.metadata.PrintDump(ctx)
	if err != nil {
		n.logger.Error("Failed to print metadata from NATS KV", "error", err)
	}
}
